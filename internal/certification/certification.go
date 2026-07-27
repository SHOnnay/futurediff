package certification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Skip Status = "skip"
)

type Check struct {
	ID       string `json:"id"`
	Status   Status `json:"status"`
	Detail   string `json:"detail"`
	Required bool   `json:"required"`
}

type Report struct {
	FormatVersion string             `json:"format_version"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Runtime       runtimeoci.Backend `json:"runtime"`
	Image         string             `json:"image"`
	Checks        []Check            `json:"checks"`
	Certified     bool               `json:"certified"`
	ReportDigest  string             `json:"report_digest"`
}

type Executor interface {
	Ready(context.Context) (runtimeoci.Backend, error)
	Execute(context.Context, runtimeoci.Request) (runtimeoci.Result, error)
}

type Options struct {
	Root string
}

func Run(ctx context.Context, executor Executor, image string, options Options) (Report, error) {
	if executor == nil {
		return Report{}, errors.New("executor is required")
	}
	root := options.Root
	if root == "" {
		root = os.TempDir()
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Report{}, err
	}
	runRoot, err := os.MkdirTemp(root, "futurediff-certify-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(runRoot)

	report := Report{FormatVersion: "0.1", GeneratedAt: time.Now().UTC(), Image: image}
	backend, err := executor.Ready(ctx)
	if err != nil {
		report.Checks = append(report.Checks, Check{ID: "runtime_ready", Status: Fail, Detail: err.Error(), Required: true})
		return finalize(report), nil
	}
	report.Runtime = backend
	report.Checks = append(report.Checks,
		Check{ID: "runtime_ready", Status: Pass, Detail: fmt.Sprintf("%s %s", backend.Kind, backend.Version), Required: true},
		booleanCheck("runtime_rootless", backend.Rootless, "runtime reports rootless mode", "runtime is not rootless", true),
		booleanCheck("image_digest_pinned", strings.Contains(image, "@sha256:") && len(strings.SplitN(image, "@sha256:", 2)[1]) == 64, "image is digest pinned", "image is not digest pinned", true),
	)

	workspace := filepath.Join(runRoot, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return Report{}, err
	}
	sentinel := filepath.Join(runRoot, "host-sentinel")
	if err := os.WriteFile(sentinel, []byte("host-only\n"), 0o600); err != nil {
		return Report{}, err
	}
	secret := "FD_CERT_SECRET_" + domain.NewID("value")
	_ = os.Setenv("FUTUREDIFF_CERTIFICATION_SECRET", secret)
	defer os.Unsetenv("FUTUREDIFF_CERTIFICATION_SECRET")

	isolationCmd := []string{"/bin/sh", "-c", `set -eu
[ ! -e /workspace/.git ]
[ ! -e /host-sentinel ]
[ -z "${FUTUREDIFF_CERTIFICATION_SECRET:-}" ]
printf 'created by container\n' > /workspace/certified.txt
printf 'isolation-ok\n'`}
	isolation, isolationErr := executor.Execute(ctx, runtimeoci.Request{TransactionID: "certification", ExecutionID: domain.NewID("cert"), Workspace: workspace, Command: isolationCmd, Purpose: runtimeoci.Mutation, SyncWorkspace: true})
	isolationPass := isolationErr == nil && isolation.ExitCode == 0 && isolation.Evidence.WorkspaceSynchronized
	report.Checks = append(report.Checks, booleanCheck("workspace_and_secret_isolation", isolationPass, "container could not see Git metadata, host sentinel, or ambient secret", detailErr(isolationErr, isolation), true))
	_, syncErr := os.Stat(filepath.Join(workspace, "certified.txt"))
	report.Checks = append(report.Checks, booleanCheck("controlled_workspace_sync", syncErr == nil, "successful mutation synchronized only the transaction workspace", fmt.Sprintf("expected synchronized file: %v", syncErr), true))
	hostBytes, hostErr := os.ReadFile(sentinel)
	report.Checks = append(report.Checks, booleanCheck("host_sentinel_unchanged", hostErr == nil && string(hostBytes) == "host-only\n", "host sentinel remained unchanged", fmt.Sprintf("host sentinel changed or unreadable: %v", hostErr), true))

	symlinkWorkspace := filepath.Join(runRoot, "symlink-workspace")
	if err := os.MkdirAll(symlinkWorkspace, 0o700); err != nil {
		return Report{}, err
	}
	if err := os.Symlink(sentinel, filepath.Join(symlinkWorkspace, "escape")); err != nil {
		return Report{}, err
	}
	_, symlinkErr := executor.Execute(ctx, runtimeoci.Request{TransactionID: "certification", ExecutionID: domain.NewID("cert"), Workspace: symlinkWorkspace, Command: []string{"/bin/true"}, Purpose: runtimeoci.Verification})
	report.Checks = append(report.Checks, booleanCheck("symlink_rejected", symlinkErr != nil, "symlinked workspace content was rejected", "symlink was unexpectedly accepted", true))

	secretWorkspace := filepath.Join(runRoot, "secret-env-workspace")
	if err := os.MkdirAll(secretWorkspace, 0o700); err != nil {
		return Report{}, err
	}
	_, envErr := executor.Execute(ctx, runtimeoci.Request{TransactionID: "certification", ExecutionID: domain.NewID("cert"), Workspace: secretWorkspace, Command: []string{"/bin/true"}, Environment: map[string]string{"API_TOKEN": "must-not-pass"}, Purpose: runtimeoci.Verification})
	report.Checks = append(report.Checks, booleanCheck("sensitive_environment_rejected", envErr != nil, "sensitive environment key was rejected before execution", "sensitive environment key was accepted", true))

	networkWorkspace := filepath.Join(runRoot, "network-workspace")
	if err := os.MkdirAll(networkWorkspace, 0o700); err != nil {
		return Report{}, err
	}
	networkCmd := []string{"/bin/sh", "-c", `if command -v wget >/dev/null 2>&1; then
  if wget -q -T 2 -O /dev/null https://example.com; then exit 1; else exit 0; fi
elif command -v curl >/dev/null 2>&1; then
  if curl -fsS --max-time 2 https://example.com >/dev/null; then exit 1; else exit 0; fi
else
  exit 77
fi`}
	network, networkErr := executor.Execute(ctx, runtimeoci.Request{TransactionID: "certification", ExecutionID: domain.NewID("cert"), Workspace: networkWorkspace, Command: networkCmd, Purpose: runtimeoci.Verification})
	switch {
	case network.ExitCode == 77:
		report.Checks = append(report.Checks, Check{ID: "network_denied", Status: Skip, Detail: "image has neither wget nor curl; plan-level network=none remains configured", Required: false})
	case networkErr == nil && network.ExitCode == 0:
		report.Checks = append(report.Checks, Check{ID: "network_denied", Status: Pass, Detail: "outbound HTTPS attempt failed inside the container", Required: true})
	default:
		report.Checks = append(report.Checks, Check{ID: "network_denied", Status: Fail, Detail: detailErr(networkErr, network), Required: true})
	}

	return finalize(report), nil
}

func finalize(report Report) Report {
	report.Certified = true
	for _, check := range report.Checks {
		if check.Required && check.Status != Pass {
			report.Certified = false
		}
	}
	clone := report
	clone.ReportDigest = ""
	digest, _ := domain.Digest(clone)
	report.ReportDigest = digest
	return report
}

func booleanCheck(id string, ok bool, pass, fail string, required bool) Check {
	if ok {
		return Check{ID: id, Status: Pass, Detail: pass, Required: required}
	}
	return Check{ID: id, Status: Fail, Detail: fail, Required: required}
}

func detailErr(err error, result runtimeoci.Result) string {
	if err != nil {
		return fmt.Sprintf("%v (exit=%d reason=%s stderr=%q)", err, result.ExitCode, result.Evidence.TerminationReason, truncate(string(result.Stderr), 512))
	}
	return fmt.Sprintf("unexpected result exit=%d reason=%s", result.ExitCode, result.Evidence.TerminationReason)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

func WriteJSON(path string, report Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
