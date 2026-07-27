package approvalstate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futurediff/futurediff/control-plane/domain"
)

type Record struct {
	TransactionID string                     `json:"transaction_id"`
	State         string                     `json:"state"`
	Reason        string                     `json:"reason,omitempty"`
	SnapshotRef   domain.ApprovalSnapshotRef `json:"snapshot_ref"`
	UpdatedAt     time.Time                  `json:"updated_at"`
}

type Store struct {
	Root string
}

func Open(root string) (*Store, error) {
	if root == "" {
		return nil, fmt.Errorf("approval store root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create approval store root: %w", err)
	}
	return &Store{Root: root}, nil
}

func (s *Store) PutApproved(ctx context.Context, transactionID string, ref domain.ApprovalSnapshotRef) error {
	_ = ctx
	return s.writeRecord(Record{
		TransactionID: transactionID,
		State:         "APPROVED",
		SnapshotRef:   ref,
		UpdatedAt:     time.Now().UTC(),
	})
}

func (s *Store) Get(ctx context.Context, transactionID string) (domain.ApprovalSnapshotRef, error) {
	_ = ctx
	record, err := s.GetRecord(context.Background(), transactionID)
	if err != nil {
		return domain.ApprovalSnapshotRef{}, err
	}
	if record.State != "APPROVED" {
		return domain.ApprovalSnapshotRef{}, fmt.Errorf("approval for transaction %s is %s: %s", transactionID, record.State, record.Reason)
	}
	return record.SnapshotRef, nil
}

func (s *Store) GetRecord(ctx context.Context, transactionID string) (*Record, error) {
	_ = ctx
	bytes, err := os.ReadFile(s.recordPath(transactionID))
	if err != nil {
		return nil, fmt.Errorf("read approval record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(bytes, &record); err != nil {
		return nil, fmt.Errorf("decode approval record: %w", err)
	}
	return &record, nil
}

func (s *Store) Invalidate(ctx context.Context, transactionID, reason string) error {
	_ = ctx
	record, err := s.GetRecord(context.Background(), transactionID)
	if err != nil {
		return err
	}
	record.State = "INVALIDATED"
	record.Reason = reason
	record.UpdatedAt = time.Now().UTC()
	return s.writeRecord(*record)
}

func (s *Store) writeRecord(record Record) error {
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode approval record: %w", err)
	}
	if err := os.WriteFile(s.recordPath(record.TransactionID), append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write approval record: %w", err)
	}
	return nil
}

func (s *Store) recordPath(transactionID string) string {
	return filepath.Join(s.Root, transactionID+".json")
}
