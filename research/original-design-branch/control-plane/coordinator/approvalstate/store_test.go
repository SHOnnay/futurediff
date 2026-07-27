package approvalstate

import (
	"context"
	"testing"

	"github.com/futurediff/futurediff/control-plane/domain"
)

func TestPutGetAndInvalidateApprovalState(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ref := domain.ApprovalSnapshotRef{SnapshotID: "snap_123", Version: "0.1", Hash: "hash_approval"}
	if err := store.PutApproved(context.Background(), "tx_approval", ref); err != nil {
		t.Fatalf("put approved: %v", err)
	}
	loaded, err := store.Get(context.Background(), "tx_approval")
	if err != nil {
		t.Fatalf("get approval: %v", err)
	}
	if loaded.Hash != ref.Hash {
		t.Fatalf("unexpected approval hash: %s", loaded.Hash)
	}
	if err := store.Invalidate(context.Background(), "tx_approval", "prepared fingerprint changed"); err != nil {
		t.Fatalf("invalidate approval: %v", err)
	}
	if _, err := store.Get(context.Background(), "tx_approval"); err == nil {
		t.Fatal("expected invalidated approval to stop loading as approved")
	}
	record, err := store.GetRecord(context.Background(), "tx_approval")
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	if record.State != "INVALIDATED" {
		t.Fatalf("expected invalidated state, got %s", record.State)
	}
	if record.Reason != "prepared fingerprint changed" {
		t.Fatalf("unexpected invalidation reason: %s", record.Reason)
	}
}
