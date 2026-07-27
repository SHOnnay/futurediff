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

func TestApprovalQuorumRequiredAndRecorded(t *testing.T) {
	fake := &appFakeGitHub{refs: map[string]string{"feature/futurediff": strings.Repeat("a", 40), "main": strings.Repeat("b", 40)}, token: "token"}
	svc, store, repoPath := newExternalEffectService(t, fake)
	now := time.Now()
	alice, alicePub, _ := operatorapproval.Generate("alice@example.com", now)
	bob, bobPub, _ := operatorapproval.Generate("bob@example.com", now)
	ring := operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{alicePub, bobPub}}
	policy := operatorapproval.QuorumPolicy{Version: operatorapproval.QuorumVersion, Threshold: 2, RequiredApprovers: []string{"alice@example.com"}}
	svc.ApprovalKeys = &ring
	svc.ApprovalQuorum = &policy
	svc.RequireSignedApprovals = true
	created, err := svc.Create(CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareGitHubDraftPR(context.Background(), created.Transaction.ID, PrepareGitHubDraftPRRequest{CredentialID: "github-main", Input: githubdraft.Input{Owner: "acme", Repo: "app", Title: "Quorum", Head: "feature/futurediff", Base: "main"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "q", PolicyVersion: "policy-test", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err := svc.Verify(created.Transaction.ID, contract); err != nil {
		t.Fatal(err)
	}
	material, _ := svc.ApprovalMaterial(created.Transaction.ID)
	digest := material["transaction_digest"]
	e1, _ := operatorapproval.Sign(alice, created.Transaction.ID, digest, time.Hour, now)
	if _, err := svc.ApproveSigned(created.Transaction.ID, e1); err == nil {
		t.Fatal("single approval met two-person quorum")
	}
	e2, _ := operatorapproval.Sign(bob, created.Transaction.ID, digest, time.Hour, now)
	bundle, _ := operatorapproval.NewBundle([]operatorapproval.Envelope{e1, e2})
	if _, err := svc.ApproveSignedQuorum(created.Transaction.ID, bundle); err != nil {
		t.Fatal(err)
	}
	snap, err := store.Snapshot(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows := snap.Rows["approvals"]
	if len(rows) != 1 {
		t.Fatalf("approvals=%d", len(rows))
	}
	if got := rows[0]["approver_identity"]; got != "quorum:alice@example.com,bob@example.com" {
		t.Fatalf("approver=%v", got)
	}
	if ref, _ := rows[0]["signature_ref"].(string); !strings.HasPrefix(ref, "ed25519-quorum:") {
		t.Fatalf("signature_ref=%v", rows[0]["signature_ref"])
	}
}
