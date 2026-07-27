package ledger

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

func TestAuditHealthyFreshTransaction(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	root := t.TempDir()
	txID := domain.NewID("tx")
	_, err = repo.Create(CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: txID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "work"), ArtifactsPath: filepath.Join(root, "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	if err != nil {
		t.Fatal(err)
	}
	report, err := repo.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Healthy || report.ErrorCount != 0 {
		t.Fatalf("unexpected audit: %+v", report)
	}
}

func TestAuditFindsCommittedEffectWithoutReceipt(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	root := t.TempDir()
	txID := domain.NewID("tx")
	_, err = repo.Create(CreateInput{Transaction: domain.Transaction{ID: txID, Mode: "cooperative", PolicyVersion: "v1"}, Workspace: domain.Workspace{TransactionID: txID, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, "work"), ArtifactsPath: filepath.Join(root, "artifacts"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	if err != nil {
		t.Fatal(err)
	}
	now := ts(domainNow())
	_, err = repo.db.Exec(`INSERT INTO effects(effect_id,transaction_id,tool_identity,adapter_identity,effect_class,status,reversibility,revision,created_at,updated_at) VALUES('eff',?,'tool','adapter','outbox','committed','none',1,?,?)`, txID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	report, err := repo.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if report.Healthy {
		t.Fatal("expected unhealthy audit")
	}
	found := false
	for _, f := range report.Findings {
		if f.Code == "committed_effect_without_receipt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing finding: %+v", report.Findings)
	}
}

func domainNow() time.Time { return time.Now().UTC() }
