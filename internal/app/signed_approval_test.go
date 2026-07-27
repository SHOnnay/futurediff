package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/verification"
)

func TestSignedApprovalRequiredAndRecorded(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "token"}
	svc, store, repoPath := newExternalEffectService(t, fake)
	priv, pub, err := operatorapproval.Generate("operator@example.com", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}
	svc.ApprovalKeys = &ring
	svc.RequireSignedApprovals = true
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareGitHubDraftPR(context.Background(), created.Transaction.ID, PrepareGitHubDraftPRRequest{CredentialID: "github-main", Input: githubdraft.Input{Owner: "acme", Repo: "app", Title: "Signed", Head: "feature/futurediff", Base: "main"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "signed", PolicyVersion: "policy-test", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err := svc.Verify(created.Transaction.ID, contract); err != nil {
		t.Fatal(err)
	}
	material, err := svc.ApprovalMaterial(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest := material["transaction_digest"]
	if _, err := svc.Approve(created.Transaction.ID, digest, "unsigned"); err == nil {
		t.Fatal("unsigned approval accepted")
	}
	env, err := operatorapproval.Sign(priv, created.Transaction.ID, digest, time.Hour, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveSigned(created.Transaction.ID, env); err != nil {
		t.Fatal(err)
	}
	snap, err := store.Snapshot(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	approvals := snap.Rows["approvals"]
	if len(approvals) != 1 {
		t.Fatalf("approvals=%d", len(approvals))
	}
	if v := approvals[0]["signature_ref"]; v == nil || v == "" {
		t.Fatal("signature reference missing")
	}
}
