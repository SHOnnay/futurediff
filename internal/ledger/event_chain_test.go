package ledger

import (
	"path/filepath"
	"testing"

	"github.com/SHOnnay/futurediff/internal/domain"
)

func TestEventChainDetectsPayloadTampering(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	txID := domain.NewID("tx")
	_, err = repo.Create(CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: txID, RepositoryRoot: t.TempDir(), GitCommonDir: t.TempDir(), BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(t.TempDir(), "work"), ArtifactsPath: filepath.Join(t.TempDir(), "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := repo.VerifyEventChains()
	if err != nil || !report.Valid {
		t.Fatalf("expected valid chain: %+v %v", report, err)
	}
	if _, err := repo.db.Exec(`UPDATE events SET payload_digest='tampered' WHERE transaction_id=? AND sequence=(SELECT MIN(sequence) FROM events WHERE transaction_id=?)`, txID, txID); err != nil {
		t.Fatal(err)
	}
	report, err = repo.VerifyEventChains()
	if err == nil || report.Valid {
		t.Fatalf("expected tampering detection: %+v", report)
	}
}

func TestEventChainIsIndependentPerTransaction(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for i := 0; i < 2; i++ {
		txID := domain.NewID("tx")
		root := t.TempDir()
		_, err = repo.Create(CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: txID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "work"), ArtifactsPath: filepath.Join(root, "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	report, err := repo.VerifyEventChains()
	if err != nil {
		t.Fatal(err)
	}
	if report.Transactions != 2 || report.Events != 4 || !report.Valid {
		t.Fatalf("unexpected report: %+v", report)
	}
}
