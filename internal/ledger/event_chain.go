package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/SHOnnay/futurediff/internal/domain"
)

// EventChainReport describes verification of the tamper-evident per-transaction
// event chain. The chain detects row modification, deletion, insertion, and
// reordering after FutureDiff has written an event.
type EventChainReport struct {
	Transactions int64    `json:"transactions"`
	Events       int64    `json:"events"`
	Valid        bool     `json:"valid"`
	Findings     []string `json:"findings,omitempty"`
}

func computeEventHash(previous, eventID, transactionID, effectID, eventType, payloadDigest, createdAt string, fencingToken int64) string {
	fields := []string{
		"futurediff-event-chain-v1",
		previous,
		eventID,
		transactionID,
		effectID,
		eventType,
		payloadDigest,
		createdAt,
		strconv.FormatInt(fencingToken, 10),
	}
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) backfillEventChains() error {
	return r.db.WithTx(func(tx *Tx) error {
		rows, err := tx.Query(`SELECT sequence,event_id,transaction_id,effect_id,event_type,payload_digest,fencing_token,created_at,previous_event_hash,event_hash FROM events ORDER BY transaction_id,sequence`)
		if err != nil {
			return err
		}
		previous := map[string]string{}
		for _, row := range rows {
			transactionID := String(row, "transaction_id")
			expectedPrevious := previous[transactionID]
			expected := computeEventHash(expectedPrevious, String(row, "event_id"), transactionID, String(row, "effect_id"), String(row, "event_type"), String(row, "payload_digest"), String(row, "created_at"), Int64(row, "fencing_token"))
			storedPrevious := String(row, "previous_event_hash")
			stored := String(row, "event_hash")
			if stored == "" && storedPrevious == "" {
				if _, err := tx.Exec(`UPDATE events SET previous_event_hash=?,event_hash=? WHERE sequence=? AND event_hash IS NULL`, nullString(expectedPrevious), expected, Int64(row, "sequence")); err != nil {
					return err
				}
			} else if storedPrevious != expectedPrevious || stored != expected {
				return fmt.Errorf("event chain mismatch while opening ledger at transaction %s sequence %d", transactionID, Int64(row, "sequence"))
			}
			previous[transactionID] = expected
		}
		return nil
	})
}

func (r *Repository) VerifyEventChains() (EventChainReport, error) {
	rows, err := r.db.Query(`SELECT sequence,event_id,transaction_id,effect_id,event_type,payload_digest,fencing_token,created_at,previous_event_hash,event_hash FROM events ORDER BY transaction_id,sequence`)
	if err != nil {
		return EventChainReport{}, err
	}
	report := EventChainReport{Events: int64(len(rows)), Valid: true}
	previous := map[string]string{}
	seenTransactions := map[string]struct{}{}
	for _, row := range rows {
		transactionID := String(row, "transaction_id")
		seenTransactions[transactionID] = struct{}{}
		expectedPrevious := previous[transactionID]
		expected := computeEventHash(expectedPrevious, String(row, "event_id"), transactionID, String(row, "effect_id"), String(row, "event_type"), String(row, "payload_digest"), String(row, "created_at"), Int64(row, "fencing_token"))
		if String(row, "previous_event_hash") != expectedPrevious {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("transaction %s sequence %d has invalid previous hash", transactionID, Int64(row, "sequence")))
		}
		if String(row, "event_hash") != expected {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("transaction %s sequence %d has invalid event hash", transactionID, Int64(row, "sequence")))
		}
		previous[transactionID] = expected
	}
	report.Transactions = int64(len(seenTransactions))
	if !report.Valid {
		return report, errors.New("event hash chain verification failed")
	}
	return report, nil
}

func appendChainedEvent(tx *Tx, transactionID, effectID, eventType string, payloadJSON []byte, payloadDigest string, fencingToken int64, createdAt string) error {
	row, err := tx.Query(`SELECT event_hash FROM events WHERE transaction_id=? ORDER BY sequence DESC LIMIT 1`, transactionID)
	if err != nil {
		return err
	}
	previous := ""
	if len(row) > 0 {
		previous = String(row[0], "event_hash")
		if previous == "" {
			return errors.New("latest event is missing event hash")
		}
	}
	eventID := domain.NewID("event")
	eventHash := computeEventHash(previous, eventID, transactionID, effectID, eventType, payloadDigest, createdAt, fencingToken)
	var effect any
	if effectID != "" {
		effect = effectID
	}
	var fence any
	if fencingToken > 0 {
		fence = fencingToken
	}
	_, err = tx.Exec(`INSERT INTO events(event_id,transaction_id,effect_id,event_type,payload_json,payload_digest,fencing_token,created_at,previous_event_hash,event_hash) VALUES(?,?,?,?,?,?,?,?,?,?)`, eventID, transactionID, effect, eventType, string(payloadJSON), payloadDigest, fence, createdAt, nullString(previous), eventHash)
	return err
}
