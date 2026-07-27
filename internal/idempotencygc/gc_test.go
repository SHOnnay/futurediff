package idempotencygc

import (
	"encoding/json"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanRedactsKeys(t *testing.T) {
	repo, e := ledger.OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer repo.Close()
	rec, created, e := repo.BeginAPIRequest("uid:1000", "private-key-0001", "POST", "/v1/transactions", "abc")
	if e != nil || !created {
		t.Fatalf("begin %v %v", created, e)
	}
	if e = repo.CompleteAPIRequest(rec.PrincipalID, rec.IdempotencyKey, rec.RequestDigest, 201, "application/json", []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	p := Policy{Version: Version, CompletedAfterHours: 1, InProgressAfterHours: 1}
	plan, e := BuildPlan(repo, p, time.Now().Add(2*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if len(plan.Candidates) != 1 {
		t.Fatalf("candidates=%d", len(plan.Candidates))
	}
	b, _ := json.Marshal(plan)
	if strings.Contains(string(b), "private-key") || strings.Contains(string(b), "uid:1000") {
		t.Fatal("plan leaked key or principal")
	}
}
func TestApplyDeletesRecords(t *testing.T) {
	repo, e := ledger.OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer repo.Close()
	rec, _, e := repo.BeginAPIRequest("uid:1", "cleanup-key-0001", "POST", "/v1/x", "digest")
	if e != nil {
		t.Fatal(e)
	}
	if e = repo.CompleteAPIRequest(rec.PrincipalID, rec.IdempotencyKey, rec.RequestDigest, 201, "application/json", []byte(`{}`)); e != nil {
		t.Fatal(e)
	}
	p := Policy{Version: Version, ApplyEnabled: true, CompletedAfterHours: 1, InProgressAfterHours: 1}
	plan, e := BuildPlan(repo, p, time.Now().Add(2*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Apply(repo, plan, "wrong", time.Now()); e == nil {
		t.Fatal("wrong confirmation accepted")
	}
	result, e := Apply(repo, plan, Confirmation, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if result.CompletedDeleted != 1 {
		t.Fatalf("deleted=%d", result.CompletedDeleted)
	}
	records, e := repo.IdempotencyBefore("completed", time.Now().Add(24*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if len(records) != 0 {
		t.Fatal("record remains")
	}
}
