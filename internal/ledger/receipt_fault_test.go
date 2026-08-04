package ledger

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
)

// receiptFixture returns a repository with a verified effect in a committing
// transaction and a valid fencing lease, ready for BeginEffectAttempt and
// RecordEffectCommitted.
func receiptFixture(t *testing.T, inject durablewrite.Injector) (*Repository, domain.ExternalEffect, int64) {
	t.Helper()
	repo, effects, token := receiptFixtureEffects(t, inject, 1)
	return repo, effects[0], token
}

// receiptFixtureEffects creates n verified effects before the transaction is
// placed in the committing state.
func receiptFixtureEffects(t *testing.T, inject durablewrite.Injector, n int) (*Repository, []domain.ExternalEffect, int64) {
	t.Helper()
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	repo.Injector = inject
	txID := domain.NewID("tx")
	root := t.TempDir()
	if _, err := repo.Create(CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: txID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "work"), ArtifactsPath: filepath.Join(root, "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}}); err != nil {
		t.Fatal(err)
	}
	effects := make([]domain.ExternalEffect, 0, n)
	for i := 0; i < n; i++ {
		effect, err := repo.CreateExternalEffect(PrepareExternalEffectInput{Effect: domain.ExternalEffect{
			EffectID: domain.NewID("eff"), TransactionID: txID, ToolIdentity: "githubdraft-tool", AdapterIdentity: "github-draft-pr",
			CredentialID: "github-main", Operation: "create_pull_request", Destination: "https://api.github.com/repos/acme/app/pulls",
			InputDigest: "input-digest", PreparedDigest: "prepared-digest", PreviewDigest: "preview-digest",
			IdempotencyKey: "github-draft-pr:" + domain.NewID("eff"), Status: domain.EffectVerified, Reversibility: "compensatable", CommitRank: 100, SupportLevel: "test",
		}})
		if err != nil {
			t.Fatal(err)
		}
		effects = append(effects, effect)
	}
	// BeginCommit requires the approval-material machinery; place the
	// transaction in the committing state directly for this focused test.
	if _, err := repo.db.Exec(`UPDATE transactions SET status='committing',revision=revision+1,updated_at=updated_at WHERE transaction_id=?`, txID); err != nil {
		t.Fatal(err)
	}
	token, err := repo.AcquireLease("transaction:"+txID, "test-coordinator", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return repo, effects, token
}

func sampleReceipt(attempt domain.EffectAttempt) domain.EffectReceipt {
	return domain.EffectReceipt{ReceiptID: domain.NewID("receipt"), ProviderOperationID: "github.pull_request.1", ProviderResourceID: "github://acme/app/pulls/1", RequestDigest: attempt.RequestDigest, ResponseDigest: "response-digest", StatusQueryRef: "https://api.github.com/repos/acme/app/pulls", CommittedAt: time.Now().UTC()}
}

func attemptCount(t *testing.T, repo *Repository) int64 {
	t.Helper()
	rows, err := repo.db.Query("SELECT COUNT(*) AS c FROM effect_attempts")
	if err != nil {
		t.Fatal(err)
	}
	return Int64(rows[0], "c")
}

func receiptCount(t *testing.T, repo *Repository) int64 {
	t.Helper()
	rows, err := repo.db.Query("SELECT COUNT(*) AS c FROM receipts")
	if err != nil {
		t.Fatal(err)
	}
	return Int64(rows[0], "c")
}

func TestBeginEffectAttemptFaultPreventsIntent(t *testing.T) {
	for _, op := range []string{durablewrite.OpCreate, durablewrite.OpWrite} {
		t.Run(op, func(t *testing.T) {
			repo, effect, token := receiptFixture(t, durablewrite.NewOneShot(map[string]error{op: durablewrite.ErrIO}))
			_, err := repo.BeginEffectAttempt(effect.EffectID, "commit", "request-digest", token)
			if err == nil {
				t.Fatal("expected injected fault")
			}
			if !errors.Is(err, durablewrite.ErrIO) {
				t.Fatalf("errors.Is(ErrIO) failed: %v", err)
			}
			if got := attemptCount(t, repo); got != 0 {
				t.Fatalf("attempt row written despite fault: %d", got)
			}
			stored, err := repo.ExternalEffect(effect.EffectID)
			if err != nil || stored.Status != domain.EffectVerified {
				t.Fatalf("effect moved off verified: %s %v", stored.Status, err)
			}
		})
	}
}

func TestRecordEffectCommittedFaultFailsClosed(t *testing.T) {
	ops := []string{durablewrite.OpCreate, durablewrite.OpWrite, durablewrite.OpShortWrite, durablewrite.OpFileSync, durablewrite.OpRename, durablewrite.OpDirectorySync}
	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			repo, effect, token := receiptFixture(t, nil)
			attempt, err := repo.BeginEffectAttempt(effect.EffectID, "commit", "request-digest", token)
			if err != nil {
				t.Fatal(err)
			}
			repo.Injector = durablewrite.NewOneShot(map[string]error{op: durablewrite.ErrIO})
			if _, err := repo.RecordEffectCommitted(attempt, sampleReceipt(attempt)); err == nil {
				t.Fatal("expected injected fault")
			}
			// No false success and no partial state: the effect is not
			// committed, the attempt stays intent, and no receipt row exists.
			stored, _ := repo.ExternalEffect(effect.EffectID)
			if stored.Status != domain.EffectCommitting {
				t.Fatalf("effect status=%s", stored.Status)
			}
			if got := receiptCount(t, repo); got != 0 {
				t.Fatalf("receipt row present despite fault: %d", got)
			}
			rows, err := repo.db.Query("SELECT outcome FROM effect_attempts WHERE attempt_id=?", attempt.AttemptID)
			if err != nil || len(rows) != 1 || String(rows[0], "outcome") != "intent" {
				t.Fatalf("attempt outcome not intent: %v %v", String(rows[0], "outcome"), err)
			}
		})
	}
}

func TestRecordEffectCommittedCommitFaultRollsBackAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault.db")
	faults := failAt{}
	db, err := OpenWithFaultInjector(path, faults)
	if err != nil {
		t.Fatal(err)
	}
	repo := &Repository{db: db}
	t.Cleanup(func() { _ = repo.Close() })
	if err := repo.migrate(); err != nil {
		t.Fatal(err)
	}
	effect, token := receiptFixtureComponents(t, repo)
	attempt, err := repo.BeginEffectAttempt(effect.EffectID, "commit", "request-digest", token)
	if err != nil {
		t.Fatal(err)
	}
	// The genuine commit/fsync boundary: the receipt statement runs inside the
	// transaction but the COMMIT fails, so the whole receipt write must roll
	// back atomically.
	faults["commit"] = 1
	if _, err := repo.RecordEffectCommitted(attempt, sampleReceipt(attempt)); err == nil {
		t.Fatal("expected injected commit failure")
	}
	stored, _ := repo.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectCommitting {
		t.Fatalf("effect status=%s", stored.Status)
	}
	if got := receiptCount(t, repo); got != 0 {
		t.Fatalf("receipt leaked despite commit rollback: %d", got)
	}
	rows, _ := repo.db.Query("SELECT outcome FROM effect_attempts WHERE attempt_id=?", attempt.AttemptID)
	if len(rows) != 1 || String(rows[0], "outcome") != "intent" {
		t.Fatalf("attempt outcome=%v", String(rows[0], "outcome"))
	}
}

// receiptFixtureComponents is the shared setup for tests that construct the
// Repository from a raw fault-injectable DB.
func receiptFixtureComponents(t *testing.T, repo *Repository) (domain.ExternalEffect, int64) {
	t.Helper()
	txID := domain.NewID("tx")
	root := t.TempDir()
	if _, err := repo.Create(CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: txID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "work"), ArtifactsPath: filepath.Join(root, "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}}); err != nil {
		t.Fatal(err)
	}
	effect, err := repo.CreateExternalEffect(PrepareExternalEffectInput{Effect: domain.ExternalEffect{
		EffectID: domain.NewID("eff"), TransactionID: txID, ToolIdentity: "githubdraft-tool", AdapterIdentity: "github-draft-pr",
		CredentialID: "github-main", Operation: "create_pull_request", Destination: "https://api.github.com/repos/acme/app/pulls",
		InputDigest: "input-digest", PreparedDigest: "prepared-digest", PreviewDigest: "preview-digest",
		IdempotencyKey: "github-draft-pr:" + domain.NewID("eff"), Status: domain.EffectVerified, Reversibility: "compensatable", CommitRank: 100, SupportLevel: "test",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE transactions SET status='committing',revision=revision+1,updated_at=updated_at WHERE transaction_id=?`, txID); err != nil {
		t.Fatal(err)
	}
	token, err := repo.AcquireLease("transaction:"+txID, "test-coordinator", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return effect, token
}

func TestRecordEffectCommittedIdempotentNoDuplicateReceipt(t *testing.T) {
	repo, effect, token := receiptFixture(t, nil)
	attempt, err := repo.BeginEffectAttempt(effect.EffectID, "commit", "request-digest", token)
	if err != nil {
		t.Fatal(err)
	}
	receipt := sampleReceipt(attempt)
	if _, err := repo.RecordEffectCommitted(attempt, receipt); err != nil {
		t.Fatal(err)
	}
	// Reconciliation records the same completed outcome again: recognized
	// without duplication (INSERT OR IGNORE + conflict check).
	if _, err := repo.RecordEffectCommitted(attempt, receipt); err != nil {
		t.Fatalf("repeated receipt recording failed: %v", err)
	}
	if got := receiptCount(t, repo); got != 1 {
		t.Fatalf("receipt duplicated: %d", got)
	}
	stored, err := repo.EffectReceipt(effect.EffectID)
	if err != nil || stored.ReceiptID != receipt.ReceiptID || stored.RequestDigest != receipt.RequestDigest {
		t.Fatalf("receipt mismatch: %+v %v", stored, err)
	}
}

func TestRecordEffectCommittedConflictingReceiptRejected(t *testing.T) {
	repo, effect, token := receiptFixture(t, nil)
	attempt, err := repo.BeginEffectAttempt(effect.EffectID, "commit", "request-digest", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordEffectCommitted(attempt, sampleReceipt(attempt)); err != nil {
		t.Fatal(err)
	}
	conflicting := sampleReceipt(attempt)
	conflicting.ReceiptID = domain.NewID("receipt")
	conflicting.RequestDigest = "different-request-digest"
	conflicting.ProviderResourceID = "github://acme/app/pulls/2"
	if _, err := repo.RecordEffectCommitted(attempt, conflicting); err == nil {
		t.Fatal("expected conflict rejection for changed provider result")
	}
	if got := receiptCount(t, repo); got != 1 {
		t.Fatalf("conflicting receipt recorded: %d", got)
	}
}

func TestPriorReceiptRemainsAvailableAfterFault(t *testing.T) {
	repo, effects, token := receiptFixtureEffects(t, nil, 2)
	effectA, effectB := effects[0], effects[1]
	attemptA, err := repo.BeginEffectAttempt(effectA.EffectID, "commit", "request-digest-a", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordEffectCommitted(attemptA, sampleReceipt(attemptA)); err != nil {
		t.Fatal(err)
	}
	// Second effect: the receipt write fails, but the first receipt stays
	// queryable.
	attemptB, err := repo.BeginEffectAttempt(effectB.EffectID, "commit", "request-digest-b", token)
	if err != nil {
		t.Fatal(err)
	}
	repo.Injector = durablewrite.NewOneShot(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO})
	if _, err := repo.RecordEffectCommitted(attemptB, sampleReceipt(attemptB)); err == nil {
		t.Fatal("expected injected fault")
	}
	stored, err := repo.EffectReceipt(effectA.EffectID)
	if err != nil || stored.ReceiptID == "" {
		t.Fatalf("prior receipt lost: %+v %v", stored, err)
	}
	if got := receiptCount(t, repo); got != 1 {
		t.Fatalf("receipt rows=%d", got)
	}
}

func TestReceiptFaultClassification(t *testing.T) {
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
			repo, effect, token := receiptFixture(t, durablewrite.NewOneShot(map[string]error{durablewrite.OpCreate: c.fail}))
			_, err := repo.BeginEffectAttempt(effect.EffectID, "commit", "request-digest", token)
			if err == nil {
				t.Fatal("expected fault")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
		})
	}
}

func TestRecordEffectCommittedRetryAfterFaultSucceeds(t *testing.T) {
	repo, effect, token := receiptFixture(t, nil)
	attempt, err := repo.BeginEffectAttempt(effect.EffectID, "commit", "request-digest", token)
	if err != nil {
		t.Fatal(err)
	}
	repo.Injector = durablewrite.NewOneShot(map[string]error{durablewrite.OpRename: durablewrite.ErrIO})
	receipt := sampleReceipt(attempt)
	if _, err := repo.RecordEffectCommitted(attempt, receipt); err == nil {
		t.Fatal("expected injected fault")
	}
	// The fault fired before any state change: retry records exactly once.
	if _, err := repo.RecordEffectCommitted(attempt, receipt); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if got := receiptCount(t, repo); got != 1 {
		t.Fatalf("receipt rows=%d", got)
	}
	stored, _ := repo.ExternalEffect(effect.EffectID)
	if stored.Status != domain.EffectCommitted {
		t.Fatalf("effect status=%s", stored.Status)
	}
}
