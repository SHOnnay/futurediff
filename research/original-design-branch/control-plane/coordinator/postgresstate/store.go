package postgresstate

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/futurediff/futurediff/control-plane/domain"
)

type Bundle struct {
	DB           *sql.DB
	Transactions *TransactionStore
	Effects      *EffectStore
	Ledger       *LedgerStore
	Approvals    *ApprovalStore
}

type TransactionStore struct {
	DB *sql.DB
}

type EffectStore struct {
	DB *sql.DB
}

type LedgerStore struct {
	DB *sql.DB
}

type ApprovalStore struct {
	DB *sql.DB
}

func Open(db *sql.DB) *Bundle {
	return &Bundle{
		DB:           db,
		Transactions: &TransactionStore{DB: db},
		Effects:      &EffectStore{DB: db},
		Ledger:       &LedgerStore{DB: db},
		Approvals:    &ApprovalStore{DB: db},
	}
}

func (b *Bundle) Bootstrap(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS coordinator_transaction_state (
			transaction_id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS coordinator_effect_state (
			effect_id TEXT PRIMARY KEY,
			state TEXT NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS coordinator_ledger (
			id BIGSERIAL PRIMARY KEY,
			transaction_id TEXT NOT NULL,
			effect_id TEXT NOT NULL DEFAULT '',
			previous_state TEXT NOT NULL,
			next_state TEXT NOT NULL,
			reason TEXT NOT NULL,
			actor_type TEXT NOT NULL,
			attempt_number INTEGER NOT NULL,
			evidence_ref TEXT NOT NULL,
			at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS coordinator_approval_state (
			transaction_id TEXT PRIMARY KEY,
			snapshot_id TEXT NOT NULL,
			version TEXT NOT NULL,
			hash TEXT NOT NULL,
			state TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL
		)`,
	} {
		if _, err := b.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("bootstrap coordinator postgres store: %w", err)
		}
	}
	return nil
}

func (b *Bundle) SeedTransaction(ctx context.Context, transactionID string, state domain.TransactionState) error {
	return b.Transactions.SetState(ctx, transactionID, state)
}

func (b *Bundle) SeedEffect(ctx context.Context, effectID string, state domain.EffectState) error {
	return b.Effects.SetState(ctx, effectID, state)
}

func (b *Bundle) SeedApproval(ctx context.Context, transactionID string, ref domain.ApprovalSnapshotRef) error {
	return b.Approvals.PutApproved(ctx, transactionID, ref)
}

func (s *TransactionStore) Get(ctx context.Context, transactionID string) (domain.TransactionState, error) {
	var state string
	if err := s.DB.QueryRowContext(ctx, `SELECT state FROM coordinator_transaction_state WHERE transaction_id = $1`, transactionID).Scan(&state); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("transaction %s not found", transactionID)
		}
		return "", fmt.Errorf("load transaction state: %w", err)
	}
	return domain.TransactionState(state), nil
}

func (s *TransactionStore) SetState(ctx context.Context, transactionID string, state domain.TransactionState) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO coordinator_transaction_state (transaction_id, state, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (transaction_id) DO UPDATE SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at
	`, transactionID, string(state), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("persist transaction state: %w", err)
	}
	return nil
}

func (s *EffectStore) Get(ctx context.Context, effectID string) (domain.EffectState, error) {
	var state string
	if err := s.DB.QueryRowContext(ctx, `SELECT state FROM coordinator_effect_state WHERE effect_id = $1`, effectID).Scan(&state); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("effect %s not found", effectID)
		}
		return "", fmt.Errorf("load effect state: %w", err)
	}
	return domain.EffectState(state), nil
}

func (s *EffectStore) SetState(ctx context.Context, effectID string, state domain.EffectState) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO coordinator_effect_state (effect_id, state, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (effect_id) DO UPDATE SET state = EXCLUDED.state, updated_at = EXCLUDED.updated_at
	`, effectID, string(state), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("persist effect state: %w", err)
	}
	return nil
}

func (s *LedgerStore) Append(ctx context.Context, record domain.TransitionRecord) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO coordinator_ledger (
			transaction_id,
			effect_id,
			previous_state,
			next_state,
			reason,
			actor_type,
			attempt_number,
			evidence_ref,
			at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, record.TransactionID, record.EffectID, record.PreviousState, record.NextState, record.Reason, record.ActorType, record.AttemptNumber, record.EvidenceRef, record.At)
	if err != nil {
		return fmt.Errorf("append coordinator ledger: %w", err)
	}
	return nil
}

func (s *LedgerStore) ListByTransaction(ctx context.Context, transactionID string) ([]domain.TransitionRecord, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT transaction_id, effect_id, previous_state, next_state, reason, actor_type, attempt_number, evidence_ref, at
		FROM coordinator_ledger
		WHERE transaction_id = $1
		ORDER BY id ASC
	`, transactionID)
	if err != nil {
		return nil, fmt.Errorf("query coordinator ledger: %w", err)
	}
	defer rows.Close()
	var records []domain.TransitionRecord
	for rows.Next() {
		var record domain.TransitionRecord
		if err := rows.Scan(&record.TransactionID, &record.EffectID, &record.PreviousState, &record.NextState, &record.Reason, &record.ActorType, &record.AttemptNumber, &record.EvidenceRef, &record.At); err != nil {
			return nil, fmt.Errorf("scan coordinator ledger: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate coordinator ledger: %w", err)
	}
	return records, nil
}

func (s *ApprovalStore) Get(ctx context.Context, transactionID string) (domain.ApprovalSnapshotRef, error) {
	var ref domain.ApprovalSnapshotRef
	var state string
	var reason string
	if err := s.DB.QueryRowContext(ctx, `SELECT snapshot_id, version, hash, state, reason FROM coordinator_approval_state WHERE transaction_id = $1`, transactionID).Scan(&ref.SnapshotID, &ref.Version, &ref.Hash, &state, &reason); err != nil {
		if err == sql.ErrNoRows {
			return domain.ApprovalSnapshotRef{}, fmt.Errorf("approval %s not found", transactionID)
		}
		return domain.ApprovalSnapshotRef{}, fmt.Errorf("load approval state: %w", err)
	}
	if state != "APPROVED" {
		return domain.ApprovalSnapshotRef{}, fmt.Errorf("approval %s is %s: %s", transactionID, state, reason)
	}
	return ref, nil
}

func (s *ApprovalStore) PutApproved(ctx context.Context, transactionID string, ref domain.ApprovalSnapshotRef) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO coordinator_approval_state (transaction_id, snapshot_id, version, hash, state, reason, updated_at)
		VALUES ($1, $2, $3, $4, 'APPROVED', '', $5)
		ON CONFLICT (transaction_id) DO UPDATE SET snapshot_id = EXCLUDED.snapshot_id, version = EXCLUDED.version, hash = EXCLUDED.hash, state = EXCLUDED.state, reason = EXCLUDED.reason, updated_at = EXCLUDED.updated_at
	`, transactionID, ref.SnapshotID, ref.Version, ref.Hash, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("persist approval state: %w", err)
	}
	return nil
}

func (s *ApprovalStore) Invalidate(ctx context.Context, transactionID, reason string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE coordinator_approval_state
		SET state = 'INVALIDATED', reason = $2, updated_at = $3
		WHERE transaction_id = $1
	`, transactionID, reason, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("invalidate approval state: %w", err)
	}
	return nil
}
