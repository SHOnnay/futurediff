package ledger

import (
	"path/filepath"
	"testing"
)

func TestAPIIdempotencyLifecycle(t *testing.T) {
	r, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	rec, created, err := r.BeginAPIRequest("uid:1", "request-123", "POST", "/v1/transactions", "abc")
	if err != nil || !created || rec.State != "in_progress" {
		t.Fatalf("begin: %#v %t %v", rec, created, err)
	}
	if err := r.CompleteAPIRequest("uid:1", "request-123", "abc", 201, "application/json", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	rec, created, err = r.BeginAPIRequest("uid:1", "request-123", "POST", "/v1/transactions", "abc")
	if err != nil || created || rec.State != "completed" || rec.StatusCode != 201 {
		t.Fatalf("replay: %#v %t %v", rec, created, err)
	}
	if _, _, err := r.BeginAPIRequest("uid:1", "request-123", "POST", "/v1/transactions", "different"); err != nil {
		t.Fatal(err)
	}
}

func TestAPIAccessHashChainDetectsTampering(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.RecordAPIAccess("uid:1", "POST", "/v1/transactions", 201, "key", "req", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordAPIAccess("uid:1", "POST", "/v1/transactions/x/seal", 200, "", "req2", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.VerifyAPIAccessChain(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE api_access_events SET status_code=500 WHERE sequence=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.VerifyAPIAccessChain(); err == nil {
		t.Fatal("expected tampering to be detected")
	}
}

func TestAPIAccessRequestIDProtectedByChain(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.RecordAPIAccess("uid:1", "POST", "/v1/transactions", 201, "", "digest", "request-123"); err != nil {
		t.Fatal(err)
	}
	summary, err := repo.APIAccessSummary(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Recent) != 1 || summary.Recent[0].RequestID != "request-123" {
		t.Fatalf("%+v", summary)
	}
	if _, err := repo.db.Exec(`UPDATE api_access_events SET request_id='changed' WHERE sequence=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.VerifyAPIAccessChain(); err == nil {
		t.Fatal("request-id tampering not detected")
	}
}
