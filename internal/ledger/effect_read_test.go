package ledger

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

// effectReadFixture builds a repository with one transaction holding two
// verified effects, moves it into the committing state, commits the first
// effect (attempt outcome "success" + receipt) and marks the second effect
// unknown (attempt outcome "unknown") — the durable evidence the restore
// comparison reads.
func effectReadFixture(t *testing.T) (*Repository, []domain.ExternalEffect, int64) {
	t.Helper()
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	txID := domain.NewID("tx")
	root := t.TempDir()
	if _, err := repo.Create(CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: txID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "work"), ArtifactsPath: filepath.Join(root, "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}}); err != nil {
		t.Fatal(err)
	}
	effects := make([]domain.ExternalEffect, 0, 2)
	for i := 0; i < 2; i++ {
		effect, err := repo.CreateExternalEffect(PrepareExternalEffectInput{Effect: domain.ExternalEffect{
			EffectID: domain.NewID("eff"), TransactionID: txID, ToolIdentity: "tool", AdapterIdentity: "adapter",
			CredentialID: "cred", Operation: "op", Destination: "https://example.invalid/op",
			InputDigest: "input-digest", PreparedDigest: "prepared-digest", PreviewDigest: "preview-digest",
			IdempotencyKey: "adapter:" + domain.NewID("eff"), Status: domain.EffectVerified, Reversibility: "compensatable", CommitRank: 100, SupportLevel: "test",
		}})
		if err != nil {
			t.Fatal(err)
		}
		effects = append(effects, effect)
	}
	if _, err := repo.db.Exec(`UPDATE transactions SET status='committing',revision=revision+1,updated_at=updated_at WHERE transaction_id=?`, txID); err != nil {
		t.Fatal(err)
	}
	token, err := repo.AcquireLease("transaction:"+txID, "test-coordinator", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Commit the first effect: intent attempt is mutated to success and a
	// receipt is recorded.
	attempt, err := repo.BeginEffectAttempt(effects[0].EffectID, "commit", "request-digest", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordEffectCommitted(attempt, domain.EffectReceipt{
		ReceiptID: domain.NewID("receipt"), EffectID: effects[0].EffectID, ProviderOperationID: "github.pull_request.1",
		ProviderResourceID: "github://acme/app/pulls/1", RequestDigest: attempt.RequestDigest, ResponseDigest: "response-digest",
		StatusQueryRef: "https://api.github.com/repos/acme/app/pulls", FencingToken: token, CommittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	// Leave the second effect ambiguous: commit intent with an unknown
	// outcome (transport error).
	attempt, err = repo.BeginEffectAttempt(effects[1].EffectID, "commit", "request-digest-2", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MarkEffectUnknown(attempt, "transport", "connection reset"); err != nil {
		t.Fatal(err)
	}
	return repo, effects, token
}

func TestTransactionsWithEffectsListsOnlyTransactionsWithEffects(t *testing.T) {
	repo, effects, _ := effectReadFixture(t)
	txID := effects[0].TransactionID
	txs, err := repo.TransactionsWithEffects()
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 1 {
		t.Fatalf("expected 1 transaction with effects, got %d", len(txs))
	}
	if txs[0].ID != txID {
		t.Fatalf("unexpected transaction %s", txs[0].ID)
	}
	if txs[0].Status != domain.StateCommitting {
		t.Fatalf("expected committing status, got %s", txs[0].Status)
	}
	// A transaction without effects is not listed.
	bareID := domain.NewID("bare")
	if _, err := repo.Create(CreateInput{Transaction: domain.Transaction{ID: bareID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: bareID, RepositoryRoot: t.TempDir(), GitCommonDir: t.TempDir(), BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(t.TempDir(), "w"), ArtifactsPath: filepath.Join(t.TempDir(), "a"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}}); err != nil {
		t.Fatal(err)
	}
	txs, err = repo.TransactionsWithEffects()
	if err != nil {
		t.Fatal(err)
	}
	if len(txs) != 1 {
		t.Fatalf("bare transaction must not be listed, got %d", len(txs))
	}
}

func TestEffectAttemptsReturnsDurableAttemptsOldestFirst(t *testing.T) {
	repo, effects, _ := effectReadFixture(t)
	txID := effects[0].TransactionID
	attempts, err := repo.EffectAttempts(txID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts (one per effect), got %d", len(attempts))
	}
	// The first effect's commit attempt mutated to success; the second
	// mutated to unknown. Order is by started_at then attempt_id.
	if attempts[0].EffectID != effects[0].EffectID || attempts[0].Outcome != "success" {
		t.Fatalf("unexpected first attempt: %+v", attempts[0])
	}
	if attempts[0].Phase != "commit" || attempts[0].RequestDigest != "request-digest" || attempts[0].FencingToken == 0 {
		t.Fatalf("attempt fields not parsed: %+v", attempts[0])
	}
	if attempts[0].StartedAt.IsZero() || attempts[0].FinishedAt.IsZero() {
		t.Fatalf("attempt timestamps not parsed: %+v", attempts[0])
	}
	if attempts[1].EffectID != effects[1].EffectID || attempts[1].Outcome != "unknown" {
		t.Fatalf("unexpected second attempt: %+v", attempts[1])
	}
	if attempts[1].ErrorClass != "transport" || attempts[1].ErrorMessage != "connection reset" {
		t.Fatalf("unknown attempt fields not parsed: %+v", attempts[1])
	}
	// A transaction without attempts yields an empty slice, not an error.
	empty, err := repo.EffectAttempts(domain.NewID("other"))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no attempts, got %d", len(empty))
	}
}

func TestEffectAttemptsParseNulls(t *testing.T) {
	repo, effects, token := effectReadFixture(t)
	// The unknown effect may get a status query; MarkEffectUnknown leaves the
	// http_status null, exercising the nullable-column parsing.
	attempt, err := repo.BeginEffectAttempt(effects[1].EffectID, "status", "status-digest", token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MarkEffectUnknown(attempt, "transport", "still unreachable"); err != nil {
		t.Fatal(err)
	}
	attempts, err := repo.EffectAttempts(effects[0].TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(attempts))
	}
	last := attempts[len(attempts)-1]
	if last.Outcome != "unknown" || last.Phase != "status" || last.ErrorClass != "transport" || last.ErrorMessage != "still unreachable" {
		t.Fatalf("status attempt not parsed: %+v", last)
	}
	if last.HTTPStatus != 0 || last.ResponseDigest != "" {
		t.Fatalf("null http_status/response_digest must read as zero values: %+v", last)
	}
	if last.FinishedAt.IsZero() {
		t.Fatalf("finished_at not parsed: %+v", last)
	}
}
