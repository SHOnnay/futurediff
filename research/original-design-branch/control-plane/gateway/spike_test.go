package gateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futurediff/futurediff/adapters/runtime/dockerrun"
)

func TestRunInspectCommitWithoutRerun(t *testing.T) {
	repo := initGitRepo(t)
	service := SpikeService{}

	record, err := service.Run(context.Background(), repo, []string{"/bin/sh", "-c", "printf 'hello from staged future\n' > note.txt"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if record.State != transactionStateAwaitingApproval {
		t.Fatalf("expected awaiting approval, got %s", record.State)
	}
	if len(record.Effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(record.Effects))
	}

	inspected, patch, err := service.Inspect(repo, record.ID)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspected.ID != record.ID {
		t.Fatalf("inspect returned wrong transaction")
	}
	if !strings.Contains(patch, "hello from staged future") {
		t.Fatalf("expected patch to contain staged content, got %q", patch)
	}

	if _, err := gitOutput(repo, "worktree", "remove", "--force", record.WorktreePath); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	committed, err := service.Commit(context.Background(), repo, record.ID)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if committed.State != "COMMITTED" {
		t.Fatalf("expected committed state, got %s", committed.State)
	}

	bytes, err := os.ReadFile(filepath.Join(repo, "note.txt"))
	if err != nil {
		t.Fatalf("read promoted file: %v", err)
	}
	if string(bytes) != "hello from staged future\n" {
		t.Fatalf("unexpected promoted content: %q", string(bytes))
	}

	ledgerBytes, err := os.ReadFile(record.LedgerPath)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	ledger := string(ledgerBytes)
	if !strings.Contains(ledger, record.ID) {
		t.Fatalf("ledger missing transaction id")
	}
	if !strings.Contains(ledger, record.Effects[0].ID) || !strings.Contains(ledger, record.Effects[1].ID) {
		t.Fatalf("ledger missing effect ids")
	}
	if !strings.Contains(ledger, "without rerunning staged command") {
		t.Fatalf("ledger missing no-rerun commit event")
	}
}

func TestRecoverAfterCrashDuringPatchPromotion(t *testing.T) {
	repo := initGitRepo(t)
	service := SpikeService{}

	record, err := service.Run(context.Background(), repo, []string{"/bin/sh", "-c", "printf 'recoverable future\n' > recover.txt"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	crashing := SpikeService{
		AfterPatchApply: func(record *TransactionRecord) error {
			return errors.New("simulated crash after patch apply")
		},
	}
	if _, err := crashing.Commit(context.Background(), repo, record.ID); err == nil || !strings.Contains(err.Error(), "simulated crash") {
		t.Fatalf("expected simulated crash, got %v", err)
	}

	persisted, patch, err := service.Inspect(repo, record.ID)
	if err != nil {
		t.Fatalf("inspect after crash: %v", err)
	}
	if persisted.State != "COMMITTING" {
		t.Fatalf("expected persisted committing state, got %s", persisted.State)
	}
	if !strings.Contains(patch, "recoverable future") {
		t.Fatalf("expected stored patch to remain available")
	}

	recovered, err := service.Recover(repo, record.ID)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered.State != "COMMITTED" {
		t.Fatalf("expected recovered committed state, got %s", recovered.State)
	}

	bytes, err := os.ReadFile(filepath.Join(repo, "recover.txt"))
	if err != nil {
		t.Fatalf("read recovered file: %v", err)
	}
	if string(bytes) != "recoverable future\n" {
		t.Fatalf("unexpected recovered content: %q", string(bytes))
	}

	ledgerBytes, err := os.ReadFile(record.LedgerPath)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if !strings.Contains(string(ledgerBytes), "recovery finalized commit from stored patch evidence") {
		t.Fatalf("ledger missing recovery completion event")
	}
}

func TestRunWithVerificationFailureAbortsWithoutPromotion(t *testing.T) {
	repo := initGitRepo(t)
	service := SpikeService{}

	record, err := service.RunWithOptions(context.Background(), repo, RunOptions{
		Command:       []string{"/bin/sh", "-c", "printf 'needs verification\n' > verify.txt"},
		VerifyCommand: []string{"/bin/sh", "-c", "printf 'failing verification\n' >&2; exit 1"},
	})
	if err != nil {
		t.Fatalf("run with verification: %v", err)
	}
	if record.State != "ABORTED" {
		t.Fatalf("expected aborted state, got %s", record.State)
	}
	if record.VerificationOutputPath == "" {
		t.Fatal("expected verification output path to be recorded")
	}

	inspected, patch, err := service.Inspect(repo, record.ID)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if inspected.State != "ABORTED" {
		t.Fatalf("expected persisted aborted state, got %s", inspected.State)
	}
	if !strings.Contains(patch, "needs verification") {
		t.Fatalf("expected stored patch to remain inspectable, got %q", patch)
	}

	verificationOutput, err := os.ReadFile(record.VerificationOutputPath)
	if err != nil {
		t.Fatalf("read verification output: %v", err)
	}
	if !strings.Contains(string(verificationOutput), "failing verification") {
		t.Fatalf("expected verification output evidence, got %q", string(verificationOutput))
	}

	if _, err := service.Commit(context.Background(), repo, record.ID); err == nil || !strings.Contains(err.Error(), "not ready to commit") {
		t.Fatalf("expected commit rejection for aborted transaction, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "verify.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected source repo to remain unchanged, got err=%v", err)
	}

	ledgerBytes, err := os.ReadFile(record.LedgerPath)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	ledger := string(ledgerBytes)
	for _, needle := range []string{
		"running staged verification command",
		"verification failed",
		"verification failed; aborting transaction",
		"transaction aborted after failed verification",
	} {
		if !strings.Contains(ledger, needle) {
			t.Fatalf("ledger missing %q", needle)
		}
	}
}

func TestRunWithVerificationPassesBeforeApproval(t *testing.T) {
	repo := initGitRepo(t)
	service := SpikeService{}

	record, err := service.RunWithOptions(context.Background(), repo, RunOptions{
		Command:       []string{"/bin/sh", "-c", "printf 'verified future\n' > verified.txt"},
		VerifyCommand: []string{"/bin/sh", "-c", "grep -q 'verified future' verified.txt"},
	})
	if err != nil {
		t.Fatalf("run with passing verification: %v", err)
	}
	if record.State != transactionStateAwaitingApproval {
		t.Fatalf("expected awaiting approval, got %s", record.State)
	}
	if record.Effects[1].State != "VERIFIED" {
		t.Fatalf("expected filesystem effect to be verified, got %s", record.Effects[1].State)
	}
}

func TestRunWithDockerGatewayExecutorStagesChanges(t *testing.T) {
	repo := initGitRepo(t)
	executor := dockerrun.GatewayExecutor{
		Image: "alpine:3.22",
		Runtime: dockerrun.Runtime{
			LookPath: func(name string) (string, error) { return "/usr/bin/docker", nil },
			Runner: func(ctx context.Context, name string, args []string) ([]byte, error) {
				mountSource := ""
				for i := 0; i < len(args)-1; i++ {
					if args[i] == "--mount" && strings.Contains(args[i+1], "src=") {
						for _, part := range strings.Split(args[i+1], ",") {
							if strings.HasPrefix(part, "src=") {
								mountSource = strings.TrimPrefix(part, "src=")
							}
						}
					}
				}
				if mountSource == "" {
					return nil, errors.New("missing docker mount source")
				}
				if err := os.WriteFile(filepath.Join(mountSource, "docker.txt"), []byte("docker staged future\n"), 0o644); err != nil {
					return nil, err
				}
				return []byte("docker executor ok"), nil
			},
		},
	}

	service := SpikeService{}
	record, err := service.RunWithOptions(context.Background(), repo, RunOptions{
		Command:         []string{"/bin/sh", "-c", "printf 'ignored by injected docker runner'"},
		CommandExecutor: executor,
	})
	if err != nil {
		t.Fatalf("run with docker executor: %v", err)
	}
	if record.State != transactionStateAwaitingApproval {
		t.Fatalf("expected awaiting approval, got %s", record.State)
	}
	_, patch, err := service.Inspect(repo, record.ID)
	if err != nil {
		t.Fatalf("inspect docker patch: %v", err)
	}
	if !strings.Contains(patch, "docker staged future") {
		t.Fatalf("expected docker-driven staged patch, got %q", patch)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	mustGit(t, repo, "init")
	mustGit(t, repo, "config", "user.email", "spike@example.com")
	mustGit(t, repo, "config", "user.name", "Spike User")
	mustGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	return repo
}

func mustGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	if _, err := gitOutput(repo, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}
