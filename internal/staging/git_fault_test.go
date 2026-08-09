package staging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
)

// faultRepo prepares an isolated repository with a captured, un-materialized
// patch and returns the manager, workspace, and patch. The manager has no
// injector armed.
func faultRepo(t *testing.T) (Manager, domain.Workspace, domain.Patch) {
	t.Helper()
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
	ws, err := m.Create("tx_fault", ins, Reject)
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
	return m, ws, patch
}

func refExists(t *testing.T, repo, ref string) bool {
	t.Helper()
	return execGit(t, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}").ExitCode == 0
}

func execGit(t *testing.T, dir string, args ...string) *execResult {
	t.Helper()
	cmd := gitCommand(dir, gitEnv(), args...)
	out, err := cmd.CombinedOutput()
	return &execResult{Out: string(out), ExitCode: exitCode(err)}
}

type execResult struct {
	Out      string
	ExitCode int
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee interface{ ExitCode() int }
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func TestMaterializeBoundaryFaultsFailClosed(t *testing.T) {
	boundaries := []struct {
		op   string
		fail error
	}{
		{OpWorktreeAdd, durablewrite.ErrIO},
		{OpApplyIndex, durablewrite.ErrIO},
		{OpWriteTree, durablewrite.ErrIO},
		{OpCommitTree, durablewrite.ErrIO},
		{OpUpdateRef, durablewrite.ErrIO},
	}
	for _, b := range boundaries {
		t.Run(b.op, func(t *testing.T) {
			m, ws, patch := faultRepo(t)
			mainBefore := git(t, ws.RepositoryRoot, "rev-parse", "HEAD")
			m.Injector = durablewrite.NewOneShot(map[string]error{b.op: b.fail})
			ref, err := m.Materialize(ws, patch, "digest")
			if err == nil {
				t.Fatalf("expected %s fault", b.op)
			}
			if ref.CommitOID != "" {
				t.Fatalf("partial materialized ref returned: %+v", ref)
			}
			if mainAfter := git(t, ws.RepositoryRoot, "rev-parse", "HEAD"); mainAfter != mainBefore {
				t.Fatalf("source branch mutated: %s -> %s", mainBefore, mainAfter)
			}
			if refExists(t, ws.RepositoryRoot, "refs/heads/futurediff/"+ws.TransactionID) {
				t.Fatalf("visible future branch after %s fault", b.op)
			}
			if out := git(t, ws.RepositoryRoot, "worktree", "list", "--porcelain"); strings.Count(out, "worktree ") != 2 {
				t.Fatalf("leftover worktrees after %s fault:\n%s", b.op, out)
			}
		})
	}
}

func TestCreateWorktreeFaultFailsClosed(t *testing.T) {
	m, ws, _ := faultRepo(t)
	mainBefore := git(t, ws.RepositoryRoot, "rev-parse", "HEAD")
	m.Injector = durablewrite.NewOneShot(map[string]error{OpWorktreeAdd: durablewrite.ErrIO})
	ins, err := m.Inspect(ws.RepositoryRoot, Reject)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create("tx_create_fault", ins, Reject); err == nil {
		t.Fatal("expected worktree_add fault")
	}
	if mainAfter := git(t, ws.RepositoryRoot, "rev-parse", "HEAD"); mainAfter != mainBefore {
		t.Fatalf("source branch mutated: %s -> %s", mainBefore, mainAfter)
	}
	if refExists(t, ws.RepositoryRoot, "refs/heads/futurediff/tx_create_fault") {
		t.Fatal("visible future branch after create fault")
	}
}

func TestCaptureBoundaryFaultsFailClosed(t *testing.T) {
	for _, op := range []string{OpGitAdd, OpWriteTree} {
		t.Run(op, func(t *testing.T) {
			m, ws, _ := faultRepo(t)
			patchPath := filepath.Join(ws.ArtifactsPath, "staged.patch")
			if err := os.Remove(patchPath); err != nil {
				t.Fatal(err)
			}
			m.Injector = durablewrite.NewOneShot(map[string]error{op: durablewrite.ErrIO})
			if _, err := m.Capture(ws); err == nil {
				t.Fatalf("expected %s fault", op)
			}
			if _, err := os.Stat(patchPath); !os.IsNotExist(err) {
				t.Fatalf("patch artifact written despite %s fault", op)
			}
		})
	}
}

func TestUpdateRefFaultLeavesDanglingObjectNotVisible(t *testing.T) {
	m, ws, patch := faultRepo(t)
	m.Injector = durablewrite.NewOneShot(map[string]error{OpUpdateRef: durablewrite.ErrIO})
	predicted, err := m.PredictMaterializedRef(ws, patch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Materialize(ws, patch, "digest"); err == nil {
		t.Fatal("expected update_ref fault")
	}
	// The commit object was created but no reference points at it: it is
	// unreachable garbage for future collection, never an applied change.
	commit, err := gitText(ws.RepositoryRoot, "cat-file", "-e", predicted.CommitOID+"^{commit}")
	if err != nil {
		t.Fatalf("predicted commit object missing: %v", err)
	}
	if commit != "" {
		t.Fatal("unexpected cat-file output")
	}
	if refExists(t, ws.RepositoryRoot, "refs/heads/futurediff/"+ws.TransactionID) {
		t.Fatal("visible future branch after update_ref fault")
	}
	refs := git(t, ws.RepositoryRoot, "for-each-ref", "--format=%(refname)", "refs/heads/futurediff/")
	if refs != "" {
		t.Fatalf("dangling object surfaced as a ref: %s", refs)
	}
}

func TestMaterializeFaultClassification(t *testing.T) {
	cases := []struct {
		name string
		fail error
		want string
	}{
		{"enospc", durablewrite.ErrDiskFull, "disk_full"},
		{"edquot", durablewrite.ErrQuotaExceeded, "quota_exceeded"},
		{"erofs", durablewrite.ErrReadOnlyFilesystem, "filesystem_read_only"},
		{"eio", durablewrite.ErrIO, "durable_write_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, ws, patch := faultRepo(t)
			m.Injector = durablewrite.NewOneShot(map[string]error{OpCommitTree: c.fail})
			_, err := m.Materialize(ws, patch, "digest")
			if err == nil {
				t.Fatal("expected fault")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
		})
	}
}

func TestMaterializeFaultErrorContainsNoCredentials(t *testing.T) {
	t.Setenv("FUTUREDIFF_GITHUB_TOKEN", "must-not-leak-into-error")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-secret-agent")
	m, ws, patch := faultRepo(t)
	m.Injector = durablewrite.NewOneShot(map[string]error{OpUpdateRef: durablewrite.ErrIO})
	_, err := m.Materialize(ws, patch, "digest")
	if err == nil {
		t.Fatal("expected fault")
	}
	text := err.Error()
	for _, secret := range []string{"must-not-leak-into-error", "/tmp/ssh-secret-agent"} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret leaked into error: %q in %q", secret, text)
		}
	}
}

func TestInspectIntegrationRefRecognizesExisting(t *testing.T) {
	m, ws, patch := faultRepo(t)
	ref, err := m.Materialize(ws, patch, "digest")
	if err != nil {
		t.Fatal(err)
	}
	seen, exists, err := m.InspectIntegrationRef(ws, patch)
	if err != nil || !exists {
		t.Fatalf("existing ref not recognized: exists=%v err=%v", exists, err)
	}
	if seen.CommitOID != ref.CommitOID || seen.RefName != ref.RefName {
		t.Fatalf("recognized ref mismatch: %+v != %+v", seen, ref)
	}
}

func TestRetryAfterFaultCreatesExactlyOneCommit(t *testing.T) {
	m, ws, patch := faultRepo(t)
	m.Injector = durablewrite.NewOneShot(map[string]error{OpUpdateRef: durablewrite.ErrIO})
	if _, err := m.Materialize(ws, patch, "digest"); err == nil {
		t.Fatal("expected update_ref fault")
	}
	// Capacity restored (one-shot consumed): retry materializes exactly once.
	ref, err := m.Materialize(ws, patch, "digest")
	if err != nil {
		t.Fatal(err)
	}
	if count := git(t, ws.RepositoryRoot, "rev-list", "--count", ref.RefName, "--not", ws.SourceHeadRef); count != "1" {
		t.Fatalf("expected exactly one future commit, got %s", count)
	}
	if out := git(t, ws.RepositoryRoot, "for-each-ref", "--format=%(refname)", "refs/heads/futurediff/"); out != ref.RefName {
		t.Fatalf("expected exactly one future branch %s, got %q", ref.RefName, out)
	}
}
