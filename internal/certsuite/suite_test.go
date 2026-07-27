package certsuite

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/egress"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fakeOCI struct{}

func (fakeOCI) Ready(context.Context) (runtimeoci.Backend, error) {
	return runtimeoci.Backend{Kind: runtimeoci.Docker, Binary: "/usr/bin/docker", Version: "test", Rootless: true}, nil
}
func (fakeOCI) Execute(_ context.Context, req runtimeoci.Request) (runtimeoci.Result, error) {
	if _, err := os.Lstat(filepath.Join(req.Workspace, "escape")); err == nil {
		return runtimeoci.Result{}, errors.New("unsafe symlink")
	}
	for key := range req.Environment {
		if strings.Contains(strings.ToUpper(key), "TOKEN") {
			return runtimeoci.Result{}, errors.New("sensitive environment")
		}
	}
	if req.Purpose == runtimeoci.Mutation {
		if err := os.WriteFile(filepath.Join(req.Workspace, "certified.txt"), []byte("ok\n"), 0o600); err != nil {
			return runtimeoci.Result{}, err
		}
	}
	return runtimeoci.Result{ExitCode: 0, Evidence: runtimeoci.Evidence{TerminationReason: runtimeoci.Exited, WorkspaceSynchronized: req.SyncWorkspace}}, nil
}

func executable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalCertification(t *testing.T) {
	dir := t.TempDir()
	mcp := executable(t, dir, "futurediff-mcp")
	report, err := Run(context.Background(), Options{Targets: []string{"local"}, MCPBinary: mcp, DaemonSocket: filepath.Join(dir, "fd.sock")}, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Certified {
		t.Fatalf("expected certified report: %+v", report)
	}
	if report.ReportDigest == "" {
		t.Fatal("missing digest")
	}
}

func TestAllTargetsCanPassWithInjectedDependencies(t *testing.T) {
	dir := t.TempDir()
	mcp := executable(t, dir, "futurediff-mcp")
	agent := executable(t, dir, "agent")
	artifact := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FD_GITHUB_TOKEN", "github-secret")
	t.Setenv("FD_SLACK_TOKEN", "slack-secret")

	factory := func(_ egress.Policy) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var body string
			switch {
			case strings.Contains(req.URL.Path, "/repos/"):
				body = `{"full_name":"acme/futurediff-test","permissions":{"push":true}}`
			case strings.HasSuffix(req.URL.Path, "/auth.test"):
				body = `{"ok":true}`
			case strings.HasSuffix(req.URL.Path, "/conversations.info"):
				body = `{"ok":true}`
			default:
				return nil, errors.New("unexpected request")
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}, nil
	}
	runner := func(_ context.Context, binary string, args []string, _ []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "attestation verify") {
			return []byte("verified"), nil
		}
		if binary == agent {
			return []byte("agent 1.0"), nil
		}
		return nil, errors.New("unexpected command")
	}
	report, err := Run(context.Background(), Options{
		Targets: []string{"all"}, ScratchRoot: dir, MCPBinary: mcp, DaemonSocket: filepath.Join(dir, "fd.sock"),
		OCIImage:    "example.invalid/futurediff@sha256:" + strings.Repeat("a", 64),
		GitHubOwner: "acme", GitHubRepo: "futurediff-test", GitHubTokenEnv: "FD_GITHUB_TOKEN",
		SlackChannel: "C123", SlackTokenEnv: "FD_SLACK_TOKEN",
		OpenCodeBinary: agent, HermesBinary: agent,
		GHBinary: agent, AttestationArtifact: artifact, AttestationRepo: "acme/futurediff",
	}, Dependencies{OCI: fakeOCI{}, HTTPClientFactory: factory, CommandRunner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Certified {
		t.Fatalf("expected all targets certified: %+v", report)
	}
}

func TestMissingExternalPrerequisitesAreBlocked(t *testing.T) {
	report, err := Run(context.Background(), Options{Targets: []string{"github", "slack", "oci", "opencode", "hermes", "attestation"}}, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Certified {
		t.Fatal("expected report to be uncertified")
	}
	blocked := 0
	for _, target := range report.Targets {
		for _, check := range target.Checks {
			if check.Status == Blocked {
				blocked++
			}
		}
	}
	if blocked < 5 {
		t.Fatalf("expected blocked prerequisites, got %d", blocked)
	}
}

func TestUnknownTargetRejected(t *testing.T) {
	_, err := Run(context.Background(), Options{Targets: []string{"unknown"}}, Dependencies{})
	if err == nil {
		t.Fatal("expected error")
	}
}
