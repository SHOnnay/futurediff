package staging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@x")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func TestLifecycle(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "base")
	m := Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}
	ins, err := m.Inspect(repo, Reject)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := m.Create("tx_test", ins, Reject)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws.WorkspacePath, "a.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch, err := m.Capture(ws)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := m.Materialize(ws, patch, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(mustRead(t, filepath.Join(repo, "a.txt"))); got != "old\n" {
		t.Fatalf("live checkout changed: %q", got)
	}
	if got := git(t, repo, "show", ref.RefName+":a.txt"); got != "new" {
		t.Fatalf("future ref mismatch: %q", got)
	}
}
func mustRead(t *testing.T, p string) []byte {
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestGitEnvironmentDoesNotInheritAmbientSecrets(t *testing.T) {
	t.Setenv("FUTUREDIFF_GITHUB_TOKEN", "must-not-reach-git")
	env := gitEnv()
	for _, entry := range env {
		if strings.HasPrefix(entry, "FUTUREDIFF_GITHUB_TOKEN=") || strings.Contains(entry, "must-not-reach-git") {
			t.Fatalf("ambient secret inherited by Git subprocess: %q", entry)
		}
	}
}
