package ledger

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

func createTenantTransaction(t *testing.T, r *Repository, id, owner string) domain.Transaction {
	t.Helper()
	tx, err := r.Create(CreateInput{Transaction: domain.Transaction{ID: id, OwnerPrincipalID: owner, Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()}, Workspace: domain.Workspace{TransactionID: id, RepositoryRoot: "/repo/" + id, GitCommonDir: "/repo/" + id + "/.git", BaseOID: "0123456789012345678901234567890123456789", ObjectFormat: "sha1", WorkspacePath: "/runtime/" + id, ArtifactsPath: "/runtime/" + id + "/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestTransactionOwnershipGrantsAndAuditChain(t *testing.T) {
	r, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tx1 := createTenantTransaction(t, r, "tx-owner-a", "uid:1000")
	_ = createTenantTransaction(t, r, "tx-owner-b", "uid:1001")
	if tx1.OwnerPrincipalID != "uid:1000" {
		t.Fatalf("owner %q", tx1.OwnerPrincipalID)
	}
	if ok, _ := r.CheckTransactionAccess(tx1.ID, "uid:1001", AccessRead); ok {
		t.Fatal("unexpected access")
	}
	if err := r.GrantTransactionAccess(tx1.ID, "uid:1000", "uid:1001", AccessRead, false, "req-1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.CheckTransactionAccess(tx1.ID, "uid:1001", AccessRead); !ok {
		t.Fatal("read grant missing")
	}
	if ok, _ := r.CheckTransactionAccess(tx1.ID, "uid:1001", AccessOperate); ok {
		t.Fatal("read grant must not operate")
	}
	if err := r.GrantTransactionAccess(tx1.ID, "uid:1000", "uid:1001", AccessOperate, false, "req-2"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.CheckTransactionAccess(tx1.ID, "uid:1001", AccessOperate); !ok {
		t.Fatal("operate grant missing")
	}
	own, err := r.ListTransactionsForPrincipal("uid:1000", false)
	if err != nil || len(own) != 1 {
		t.Fatalf("owner list %d %v", len(own), err)
	}
	shared, err := r.ListTransactionsForPrincipal("uid:1001", false)
	if err != nil || len(shared) != 2 {
		t.Fatalf("shared list %d %v", len(shared), err)
	}
	if err := r.RevokeTransactionAccess(tx1.ID, "uid:1000", "uid:1001", false, "req-3"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := r.CheckTransactionAccess(tx1.ID, "uid:1001", AccessRead); ok {
		t.Fatal("revoke failed")
	}
	head, err := r.VerifyTransactionAccessChain()
	if err != nil || head == "" {
		t.Fatalf("chain %q %v", head, err)
	}
	events, err := r.TransactionAccessEvents(20)
	if err != nil || len(events) != 5 {
		t.Fatalf("events %d %v", len(events), err)
	}
	if _, err := r.db.Exec(`UPDATE transaction_access_events SET subject_principal_id='uid:9999' WHERE sequence=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.VerifyTransactionAccessChain(); err == nil {
		t.Fatal("expected tamper detection")
	}
}

func TestTransactionGrantRequiresOwnerOrAllScope(t *testing.T) {
	r, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tx := createTenantTransaction(t, r, "tx-scope", "uid:1000")
	if err := r.GrantTransactionAccess(tx.ID, "uid:1002", "uid:1001", AccessRead, false, ""); err == nil {
		t.Fatal("non-owner grant should fail")
	}
	if err := r.GrantTransactionAccess(tx.ID, "uid:2000", "uid:1001", AccessRead, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := r.GrantTransactionAccess(tx.ID, "uid:1000", "uid:1000", AccessRead, false, ""); err == nil {
		t.Fatal("owner self-grant should fail")
	}
	if err := r.GrantTransactionAccess(tx.ID, "uid:1000", "uid:1001", AccessAdmin, false, ""); err == nil {
		t.Fatal("admin grant should fail")
	}
}
