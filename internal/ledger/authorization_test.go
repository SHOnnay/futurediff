package ledger

import (
	"path/filepath"
	"testing"
)

func TestAuthorizationAuditAndCapabilityUse(t *testing.T) {
	r, err := OpenRepository(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.RecordAuthorizationDecision(AuthorizationDecisionInput{PrincipalID: "uid:1", OperationID: "health", Allowed: true, Source: "role", ReasonCode: "role_grant", PolicyDigest: "p", Roles: []string{"reader"}, RequestID: "r1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.VerifyAuthorizationDecisionChain(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`UPDATE authorization_decisions SET reason_code='changed' WHERE sequence=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.VerifyAuthorizationDecisionChain(); err == nil {
		t.Fatal("expected tamper detection")
	}
	if err := r.ConsumeAuthorizationCapability("cap-1", "uid:1", "transaction_commit", "tx-1", "d"); err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeAuthorizationCapability("cap-1", "uid:1", "transaction_commit", "tx-1", "d"); err == nil {
		t.Fatal("expected single-use rejection")
	}
}
