package transactionexpiry

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
)

func TestPlanSafeStates(t *testing.T) {
	dir := t.TempDir()
	repo, err := ledger.OpenRepository(filepath.Join(dir, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Now().UTC()
	_, err = repo.Create(ledger.CreateInput{
		Transaction: domain.Transaction{ID: "tx-old", Mode: "advisory", PolicyVersion: "p", CreatedAt: now.Add(-10 * time.Hour)},
		Workspace:   domain.Workspace{TransactionID: "tx-old", RepositoryRoot: dir, GitCommonDir: dir, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(dir, "runtime", "tx-old"), ArtifactsPath: filepath.Join(dir, "runtime", "tx-old", "artifacts")},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{Version: Version, StateAfterHours: map[string]int64{"active": 1}}
	plan, err := BuildPlan(repo, p, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Candidates) != 1 {
		t.Fatalf("candidates=%d", len(plan.Candidates))
	}
	if _, err = BuildPlan(repo, Policy{Version: Version, StateAfterHours: map[string]int64{"committing": 1}}, now); err == nil {
		t.Fatal("unsafe state accepted")
	}
}

func TestApplyAbortsSafeTransaction(t *testing.T) {
	dir := t.TempDir()
	repo, err := ledger.OpenRepository(filepath.Join(dir, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	now := time.Now().UTC()
	_, err = repo.Create(ledger.CreateInput{
		Transaction: domain.Transaction{ID: "tx-expire", Mode: "advisory", PolicyVersion: "p", CreatedAt: now.Add(-10 * time.Hour)},
		Workspace:   domain.Workspace{TransactionID: "tx-expire", RepositoryRoot: dir, GitCommonDir: dir, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: "", ArtifactsPath: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{Version: Version, ApplyEnabled: true, StateAfterHours: map[string]int64{"active": 1}}
	plan, err := BuildPlan(repo, p, now)
	if err != nil {
		t.Fatal(err)
	}
	svc := &app.Service{Ledger: repo, Staging: staging.Manager{RuntimeRoot: filepath.Join(dir, "runtime")}}
	if _, err = Apply(svc, repo, plan, "wrong", now); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	result, err := Apply(svc, repo, plan, Confirmation, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.Expired != 1 {
		t.Fatalf("expired=%d", result.Expired)
	}
	tx, err := repo.Get("tx-expire")
	if err != nil {
		t.Fatal(err)
	}
	if tx.Status != domain.StateAborted {
		t.Fatalf("status=%s", tx.Status)
	}
}
