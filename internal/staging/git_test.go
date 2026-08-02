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
	env := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_NO_REPLACE_OBJECTS=") || strings.HasPrefix(entry, "GIT_REPLACE_REF_BASE=") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env, "GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@x", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@x")
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

func TestGitEnvironmentDisablesReplacementObjectsAndPrompts(t *testing.T) {
	env := gitEnv()
	for _, want := range []string{"GIT_NO_REPLACE_OBJECTS=1", "GIT_TERMINAL_PROMPT=0"} {
		found := false
		for _, entry := range env {
			if entry == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing hardened Git environment entry %q: %v", want, env)
		}
	}
}

func TestRunGitIgnoresReplacementRefs(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "value.txt"), []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "value.txt")
	git(t, repo, "commit", "-m", "original")
	originalCommit := git(t, repo, "rev-parse", "HEAD")
	originalTree := git(t, repo, "show", "-s", "--format=%T", originalCommit)

	if err := os.WriteFile(filepath.Join(repo, "value.txt"), []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "value.txt")
	git(t, repo, "commit", "-m", "replacement")
	replacementCommit := git(t, repo, "rev-parse", "HEAD")
	replacementTree := git(t, repo, "show", "-s", "--format=%T", replacementCommit)
	if replacementTree == originalTree {
		t.Fatal("test commits unexpectedly have the same tree")
	}

	git(t, repo, "--no-replace-objects", "reset", "--hard", originalCommit)
	git(t, repo, "replace", originalCommit, replacementCommit)
	if got := git(t, repo, "show", "-s", "--format=%T", originalCommit); got != replacementTree {
		t.Fatalf("test replacement ref not active: got %s want %s", got, replacementTree)
	}

	got, err := gitText(repo, "show", "-s", "--format=%T", originalCommit)
	if err != nil {
		t.Fatal(err)
	}
	if got != originalTree {
		t.Fatalf("FutureDiff Git boundary honored replacement ref: got %s want %s", got, originalTree)
	}
}
