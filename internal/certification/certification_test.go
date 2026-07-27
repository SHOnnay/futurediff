package certification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

type fakeExecutor struct {
	backend runtimeoci.Backend
	calls   int
}

func (f *fakeExecutor) Ready(context.Context) (runtimeoci.Backend, error) { return f.backend, nil }
func (f *fakeExecutor) Execute(_ context.Context, req runtimeoci.Request) (runtimeoci.Result, error) {
	f.calls++
	for key := range req.Environment {
		if key == "API_TOKEN" {
			return runtimeoci.Result{}, errors.New("sensitive environment rejected")
		}
	}
	if _, err := os.Lstat(filepath.Join(req.Workspace, "escape")); err == nil {
		return runtimeoci.Result{}, errors.New("symlink rejected")
	}
	result := runtimeoci.Result{ExitCode: 0, Evidence: runtimeoci.Evidence{WorkspaceSynchronized: req.SyncWorkspace, TerminationReason: runtimeoci.Exited}}
	if len(req.Command) >= 3 && req.Command[2] != "" {
		if req.SyncWorkspace {
			_ = os.WriteFile(filepath.Join(req.Workspace, "certified.txt"), []byte("created by container\n"), 0o600)
		}
		if filepath.Base(req.Workspace) == "network-workspace" {
			result.ExitCode = 77
		}
	}
	return result, nil
}

func TestRunProducesCertification(t *testing.T) {
	f := &fakeExecutor{backend: runtimeoci.Backend{Kind: runtimeoci.Docker, Binary: "fake", Version: "1", Rootless: true}}
	report, err := Run(context.Background(), f, "example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Certified || report.ReportDigest == "" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestFinalizeFailsRequiredCheck(t *testing.T) {
	report := finalize(Report{Checks: []Check{{ID: "x", Status: Fail, Required: true}}})
	if report.Certified {
		t.Fatal("failed required check must prevent certification")
	}
}
