package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
	"github.com/SHOnnay/futurediff/internal/verification"
)

const slackSecretToken = "slack-super-secret-token"

// rejectingSlack simulates a Slack workspace whose history already contains a
// message matching the prepared effect (known-present reconciliation), or
// that rejects posts while echoing the bearer token back in the error body.
type rejectingSlack struct {
	mu        sync.Mutex
	messages  []map[string]any
	posts     int
	reject    bool // chat.postMessage returns 400 echoing the token
	echoToken string
}

func (f *rejectingSlack) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+f.echoToken {
		return appResponse(401, `{"ok":false,"error":"unauthorized"}`), nil
	}
	if strings.HasSuffix(r.URL.Path, "conversations.history") {
		b, _ := json.Marshal(map[string]any{"ok": true, "messages": f.messages})
		return appResponse(200, string(b)), nil
	}
	if strings.HasSuffix(r.URL.Path, "chat.postMessage") {
		f.posts++
		if f.reject {
			body := `{"ok":false,"error":"invalid_auth token=` + f.echoToken + `"}`
			return appResponse(400, body), nil
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		m := map[string]any{"ts": "1700.1", "client_msg_id": p["client_msg_id"], "metadata": p["metadata"]}
		f.messages = append(f.messages, m)
		return appResponse(200, `{"ok":true,"channel":"C12345678","ts":"1700.1","message":{"client_msg_id":"`+p["client_msg_id"].(string)+`"}}`), nil
	}
	return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
}

func newRejectingSlackService(t *testing.T, f *rejectingSlack) (*Service, string) {
	svc, _, repo := newSlackService(t, &fakeSlack{token: f.echoToken})
	// Replace the transport with the rejecting fake while reusing the same
	// credential broker wiring from newSlackService.
	svc.Slack = &slackoutbox.Adapter{HTTPClient: &http.Client{Transport: f}}
	return svc, repo
}

func prepareSlackReadyTransaction(t *testing.T, svc *Service, repo string) (string, domain.ExternalEffect, string) {
	t.Helper()
	created, err := svc.Create(CreateRequest{Repository: repo, Mode: "cooperative", PolicyVersion: "policy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Workspace.WorkspacePath, "README.md"), []byte("future\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	effect, err := svc.PrepareSlackMessage(created.Transaction.ID, PrepareSlackMessageRequest{CredentialID: "slack-main", Input: slackoutbox.Input{Channel: "C12345678", Text: "FutureDiff transaction ready"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(created.Transaction.ID); err != nil {
		t.Fatal(err)
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "basic", PolicyVersion: "policy-test", Checks: []verification.Check{{CheckID: "readme", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "README.md"}}}
	if _, err := svc.Verify(created.Transaction.ID, contract); err != nil {
		t.Fatal(err)
	}
	material, err := svc.ApprovalMaterial(created.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	digest := material["transaction_digest"]
	if _, err := svc.Approve(created.Transaction.ID, digest, "test-user"); err != nil {
		t.Fatal(err)
	}
	return created.Transaction.ID, effect, digest
}

func TestSlackKnownPresentCommitsWithoutPost(t *testing.T) {
	f := &rejectingSlack{echoToken: slackSecretToken}
	svc, repo := newRejectingSlackService(t, f)
	txID, effect, digest := prepareSlackReadyTransaction(t, svc, repo)
	// A message matching the prepared effect already exists in the channel
	// (for example, a previous run posted it before the receipt was lost):
	// commit must record the receipt without a second post.
	var prepared slackoutbox.Prepared
	if err := json.Unmarshal([]byte(effect.PreparedJSON), &prepared); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.messages = append(f.messages, map[string]any{"ts": "1700.42", "client_msg_id": prepared.Payload.ClientMsgID, "metadata": map[string]any{"event_type": "futurediff_effect", "event_payload": map[string]string{"effect_id": effect.EffectID}}})
	f.mu.Unlock()
	committed, err := svc.CommitContext(context.Background(), txID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if f.posts != 0 {
		t.Fatalf("duplicate post dispatched: %d", f.posts)
	}
	if committed.Transaction.Status != domain.StateCommitted || len(committed.Receipts) != 1 {
		t.Fatalf("commit state: %s receipts=%d", committed.Transaction.Status, len(committed.Receipts))
	}
	if committed.Receipts[0].ProviderResourceID != "slack://C12345678/messages/1700.42" {
		t.Fatalf("receipt=%#v", committed.Receipts[0])
	}
}

func TestSlackReceiptFaultAfterPostReconcilesWithoutRepost(t *testing.T) {
	f := &rejectingSlack{echoToken: slackSecretToken}
	svc, store, repo := newSlackService(t, &fakeSlack{token: slackSecretToken})
	f.echoToken = slackSecretToken
	svc.Slack = &slackoutbox.Adapter{HTTPClient: &http.Client{Transport: f}}
	txID, _, digest := prepareSlackReadyTransaction(t, svc, repo)
	// The post succeeds but the durable receipt cannot be persisted.
	store.Injector = durablewrite.NewOneShot(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO})
	if _, err := svc.CommitContext(context.Background(), txID, digest); err == nil {
		t.Fatal("expected post-receipt fault")
	}
	if f.posts != 1 {
		t.Fatalf("posts=%d", f.posts)
	}
	tx, _ := store.Get(txID)
	if tx.Status != domain.StateNeedsReconciliation {
		t.Fatalf("tx status=%s", tx.Status)
	}
	// Recovery status-queries history, sees the matching message, and records
	// the receipt without posting again.
	recovered, err := svc.Recover(txID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transaction.Status != domain.StateCommitted || f.posts != 1 || len(recovered.Receipts) != 1 {
		t.Fatalf("recovery: status=%s posts=%d receipts=%d", recovered.Transaction.Status, f.posts, len(recovered.Receipts))
	}
}

func TestSlackProviderRejectionRedactsToken(t *testing.T) {
	f := &rejectingSlack{echoToken: slackSecretToken, reject: true}
	svc, repo := newRejectingSlackService(t, f)
	txID, _, digest := prepareSlackReadyTransaction(t, svc, repo)
	_, err := svc.CommitContext(context.Background(), txID, digest)
	if err == nil {
		t.Fatal("expected provider rejection")
	}
	if strings.Contains(err.Error(), slackSecretToken) {
		t.Fatalf("token leaked into commit error: %v", err)
	}
	attempts, _ := svc.Ledger.EffectAttempts(txID)
	for _, attempt := range attempts {
		if strings.Contains(attempt.ErrorMessage, slackSecretToken) {
			t.Fatalf("token leaked into stored effect attempt: %s", attempt.ErrorMessage)
		}
	}
	rows, _ := svc.Ledger.Events(txID)
	encoded, _ := json.Marshal(rows)
	if strings.Contains(string(encoded), slackSecretToken) {
		t.Fatal("token leaked into transaction events")
	}
}
