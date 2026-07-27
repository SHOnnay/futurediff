package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type EventChainHead struct {
	TransactionID string `json:"transaction_id"`
	Sequence      int64  `json:"sequence"`
	EventHash     string `json:"event_hash"`
}
type EventChainHeads struct {
	Count  int              `json:"count"`
	Heads  []EventChainHead `json:"heads"`
	Digest string           `json:"digest"`
}

func (r *Repository) EventChainHeads() (EventChainHeads, error) {
	rows, err := r.db.Query(`SELECT e.transaction_id,e.sequence,e.event_hash FROM events e JOIN (SELECT transaction_id,MAX(sequence) AS max_sequence FROM events GROUP BY transaction_id) h ON h.transaction_id=e.transaction_id AND h.max_sequence=e.sequence ORDER BY e.transaction_id`)
	if err != nil {
		return EventChainHeads{}, err
	}
	out := EventChainHeads{Heads: make([]EventChainHead, 0, len(rows))}
	for _, row := range rows {
		out.Heads = append(out.Heads, EventChainHead{TransactionID: String(row, "transaction_id"), Sequence: Int64(row, "sequence"), EventHash: String(row, "event_hash")})
	}
	out.Count = len(out.Heads)
	b, _ := json.Marshal(out.Heads)
	sum := sha256.Sum256(b)
	out.Digest = hex.EncodeToString(sum[:])
	return out, nil
}

func (r *Repository) OptimizeLedger() error {
	if err := r.db.Checkpoint(); err != nil {
		return err
	}
	if err := r.db.ExecScript(`PRAGMA optimize; ANALYZE; VACUUM;`); err != nil {
		return err
	}
	if err := r.db.Checkpoint(); err != nil {
		return err
	}
	return r.db.IntegrityCheck()
}

type LeaseRecord struct {
	LeaseName    string    `json:"lease_name"`
	OwnerID      string    `json:"owner_id"`
	FencingToken int64     `json:"fencing_token"`
	AcquiredAt   time.Time `json:"acquired_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Expired      bool      `json:"expired"`
}

func (r *Repository) Leases(now time.Time) ([]LeaseRecord, error) {
	rows, err := r.db.Query(`SELECT lease_name,owner_id,fencing_token,acquired_at_ms,expires_at_ms FROM leases ORDER BY lease_name`)
	if err != nil {
		return nil, err
	}
	out := make([]LeaseRecord, 0, len(rows))
	for _, row := range rows {
		a := time.UnixMilli(Int64(row, "acquired_at_ms")).UTC()
		e := time.UnixMilli(Int64(row, "expires_at_ms")).UTC()
		out = append(out, LeaseRecord{LeaseName: String(row, "lease_name"), OwnerID: String(row, "owner_id"), FencingToken: Int64(row, "fencing_token"), AcquiredAt: a, ExpiresAt: e, Expired: !e.After(now.UTC())})
	}
	return out, nil
}
func (r *Repository) DeleteExpiredLeases(now time.Time) (int64, error) {
	return r.db.Exec(`DELETE FROM leases WHERE expires_at_ms<=?`, now.UTC().UnixMilli())
}
