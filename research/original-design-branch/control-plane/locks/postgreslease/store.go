package postgreslease

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Store struct {
	db *sql.DB
}

type Lease struct {
	TransactionID  string
	OwnerID        string
	LeaseExpiresAt time.Time
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Bootstrap(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS transaction_leases (
	transaction_id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL,
	lease_expires_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`)
	if err != nil {
		return fmt.Errorf("create transaction_leases: %w", err)
	}
	return nil
}

func (s *Store) Claim(ctx context.Context, transactionID, ownerID string, duration time.Duration) (bool, error) {
	row := s.db.QueryRowContext(ctx, `
INSERT INTO transaction_leases (transaction_id, owner_id, lease_expires_at, updated_at)
VALUES ($1, $2, NOW() + ($3 * INTERVAL '1 microsecond'), NOW())
ON CONFLICT (transaction_id) DO UPDATE
SET owner_id = EXCLUDED.owner_id,
	lease_expires_at = EXCLUDED.lease_expires_at,
	updated_at = NOW()
WHERE transaction_leases.owner_id = EXCLUDED.owner_id
	OR transaction_leases.lease_expires_at <= NOW()
RETURNING owner_id
`, transactionID, ownerID, duration.Microseconds())

	var claimedOwner string
	if err := row.Scan(&claimedOwner); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("claim lease: %w", err)
	}
	return claimedOwner == ownerID, nil
}

func (s *Store) Renew(ctx context.Context, transactionID, ownerID string, duration time.Duration) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
UPDATE transaction_leases
SET lease_expires_at = NOW() + ($3 * INTERVAL '1 microsecond'),
	updated_at = NOW()
WHERE transaction_id = $1
	AND owner_id = $2
	AND lease_expires_at > NOW()
`, transactionID, ownerID, duration.Microseconds())
	if err != nil {
		return false, fmt.Errorf("renew lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("renew lease rows affected: %w", err)
	}
	return rows == 1, nil
}

func (s *Store) Release(ctx context.Context, transactionID, ownerID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
DELETE FROM transaction_leases
WHERE transaction_id = $1
	AND owner_id = $2
`, transactionID, ownerID)
	if err != nil {
		return false, fmt.Errorf("release lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("release lease rows affected: %w", err)
	}
	return rows == 1, nil
}

func (s *Store) Lookup(ctx context.Context, transactionID string) (*Lease, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT transaction_id, owner_id, lease_expires_at
FROM transaction_leases
WHERE transaction_id = $1
`, transactionID)

	var lease Lease
	if err := row.Scan(&lease.TransactionID, &lease.OwnerID, &lease.LeaseExpiresAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup lease: %w", err)
	}
	return &lease, nil
}
