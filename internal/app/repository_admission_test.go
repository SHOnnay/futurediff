package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
)

func TestCreateUsesStableRepositoryAdmissionWhenPolicyOmitted(t *testing.T) {
	tmp := t.TempDir()
	repoPath := createAdmissionTestRepository(t, tmp)
	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}}
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Transaction.ID == "" {
		t.Fatal("transaction ID is empty")
	}
}

func TestCreateStableDefaultRejectsReplacementRefs(t *testing.T) {
	tmp := t.TempDir()
	repoPath := createAdmissionTestRepository(t, tmp)
	original := admissionGit(t, repoPath, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	admissionGit(t, repoPath, "add", "README.md")
	admissionGit(t, repoPath, "commit", "-m", "replacement")
	replacement := admissionGit(t, repoPath, "rev-parse", "HEAD")
	admissionGit(t, repoPath, "--no-replace-objects", "reset", "--hard", original)
	admissionGit(t, repoPath, "replace", original, replacement)

	store, err := ledger.OpenRepository(filepath.Join(tmp, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := &Service{Ledger: store, Staging: staging.Manager{RuntimeRoot: filepath.Join(tmp, "runtime")}}

	_, err = svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err == nil {
		t.Fatal("repository with replacement refs was admitted")
	}
	for _, want := range []string{"stable-default-v0.2", "replace_refs_not_allowed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("admission error %q does not contain %q", err, want)
		}
	}
}

func createAdmissionTestRepository(t *testing.T, root string) string {
	t.Helper()
	repoPath := filepath.Join(root, "repo")
	if err := os.Mkdir(repoPath, 0o700); err != nil {
		t.Fatal(err)
	}
	admissionGit(t, repoPath, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	admissionGit(t, repoPath, "add", "README.md")
	admissionGit(t, repoPath, "commit", "-m", "base")
	return repoPath
}

func admissionGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=FutureDiff Test",
		"GIT_AUTHOR_EMAIL=futurediff-test@localhost",
		"GIT_COMMITTER_NAME=FutureDiff Test",
		"GIT_COMMITTER_EMAIL=futurediff-test@localhost",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}
