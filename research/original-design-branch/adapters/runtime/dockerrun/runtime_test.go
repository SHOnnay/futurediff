package dockerrun

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildPlanUsesHardenedDockerDefaults(t *testing.T) {
	runtime := Runtime{LookPath: func(name string) (string, error) { return "/usr/bin/docker", nil }}
	plan, err := runtime.BuildPlan(Config{
		Image:       "alpine:3.22",
		Workdir:     "/tmp/worktree",
		MountSource: "/tmp/worktree",
		Command:     []string{"/bin/sh", "-c", "printf ok"},
		Env:         map[string]string{"FUTUREDIFF_TX": "tx_123"},
	})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	joined := strings.Join(plan.Args, " ")
	for _, needle := range []string{"run", "--network none", "--cap-drop ALL", "--security-opt no-new-privileges", "--read-only", "--tmpfs /tmp:rw,noexec,nosuid,size=64m", "type=bind,src=/tmp/worktree,dst=/workspace", "--workdir /workspace", "alpine:3.22"} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("expected hardened docker plan to contain %q, got %s", needle, joined)
		}
	}
}

func TestProbeReportsUnavailableDocker(t *testing.T) {
	runtime := Runtime{LookPath: func(name string) (string, error) { return "", errors.New("missing") }}
	result, err := runtime.Probe()
	if err == nil {
		t.Fatal("expected unavailable docker probe to return an error")
	}
	if result.Available {
		t.Fatal("expected docker probe to report unavailable")
	}
}

func TestRunUsesInjectedRunner(t *testing.T) {
	var gotName string
	var gotArgs []string
	runtime := Runtime{
		LookPath: func(name string) (string, error) { return "/usr/bin/docker", nil },
		Runner: func(ctx context.Context, name string, args []string) ([]byte, error) {
			gotName = name
			gotArgs = append([]string(nil), args...)
			return []byte("ok"), nil
		},
	}
	output, err := runtime.Run(context.Background(), Config{
		Image:       "alpine:3.22",
		Workdir:     "/tmp/worktree",
		MountSource: "/tmp/worktree",
		Command:     []string{"/bin/sh", "-c", "printf ok"},
	})
	if err != nil {
		t.Fatalf("run plan: %v", err)
	}
	if string(output) != "ok" {
		t.Fatalf("unexpected runner output: %q", string(output))
	}
	if gotName != "/usr/bin/docker" {
		t.Fatalf("unexpected runtime binary: %s", gotName)
	}
	if len(gotArgs) == 0 || gotArgs[len(gotArgs)-3] != "/bin/sh" {
		t.Fatalf("expected injected runner to receive command tail, got %v", gotArgs)
	}
}
