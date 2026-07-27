package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SHOnnay/futurediff/internal/credentials"
)

func (r *Repository) RegisterAdapterIdentity(adapter credentials.AdapterIdentity) error {
	if err := adapter.Validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	return r.db.WithTx(func(tx *Tx) error {
		rows, err := tx.Query("SELECT trust_level,executable_digest FROM adapter_identities WHERE adapter_id=?", adapter.ID)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			if String(rows[0], "trust_level") != string(adapter.TrustLevel) || String(rows[0], "executable_digest") != adapter.ExecutableDigest {
				return fmt.Errorf("adapter identity %q changed trust level or executable digest", adapter.ID)
			}
			_, err = tx.Exec(`UPDATE adapter_identities SET version=?,enabled=?,updated_at=? WHERE adapter_id=?`, adapter.Version, adapter.Enabled, ts(now), adapter.ID)
			return err
		}
		_, err = tx.Exec(`INSERT INTO adapter_identities(adapter_id,version,trust_level,executable_digest,enabled,registered_at,updated_at) VALUES(?,?,?,?,?,?,?)`, adapter.ID, adapter.Version, string(adapter.TrustLevel), nullString(adapter.ExecutableDigest), adapter.Enabled, ts(now), ts(now))
		return err
	})
}

func (r *Repository) RegisterCredentialBinding(binding credentials.Binding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	adapters, err := json.Marshal(binding.AllowedAdapters)
	if err != nil {
		return err
	}
	operations, err := json.Marshal(binding.AllowedOperations)
	if err != nil {
		return err
	}
	destinations, err := json.Marshal(binding.AllowedDestinations)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(binding.Source.Reference))
	sourceDigest := "sha256:" + hex.EncodeToString(sum[:])
	now := time.Now().UTC()
	var expires any
	if binding.ExpiresAt != nil {
		expires = ts(binding.ExpiresAt.UTC())
	}
	return r.db.WithTx(func(tx *Tx) error {
		rows, err := tx.Query("SELECT source_kind,source_reference_digest FROM credential_bindings WHERE credential_id=?", binding.ID)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			if String(rows[0], "source_kind") != binding.Source.Kind || String(rows[0], "source_reference_digest") != sourceDigest {
				return fmt.Errorf("credential binding %q changed secret source identity", binding.ID)
			}
			_, err = tx.Exec(`UPDATE credential_bindings SET provider=?,account=?,allowed_adapters_json=?,allowed_operations_json=?,allowed_destinations_json=?,expires_at=?,enabled=?,updated_at=? WHERE credential_id=?`, binding.Provider, nullString(binding.Account), string(adapters), string(operations), string(destinations), expires, binding.Enabled, ts(now), binding.ID)
			return err
		}
		_, err = tx.Exec(`INSERT INTO credential_bindings(credential_id,provider,account,source_kind,source_reference_digest,allowed_adapters_json,allowed_operations_json,allowed_destinations_json,expires_at,enabled,registered_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, binding.ID, binding.Provider, nullString(binding.Account), binding.Source.Kind, sourceDigest, string(adapters), string(operations), string(destinations), expires, binding.Enabled, ts(now), ts(now))
		return err
	})
}

func (r *Repository) RecordCredentialAccess(event credentials.AccessEvent) error {
	if event.EventID == "" || event.AdapterID == "" || event.CredentialID == "" || event.Operation == "" || event.Destination == "" || event.Reason == "" {
		return errors.New("incomplete credential access audit event")
	}
	switch event.Decision {
	case credentials.DecisionGranted, credentials.DecisionDenied, credentials.DecisionError:
	default:
		return errors.New("invalid credential access decision")
	}
	when := event.CreatedAt.UTC()
	if when.IsZero() {
		when = time.Now().UTC()
	}
	_, err := r.db.Exec(`INSERT INTO credential_access_events(event_id,transaction_id,effect_id,adapter_id,credential_id,operation,destination,decision,reason,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, event.EventID, nullString(event.TransactionID), nullString(event.EffectID), event.AdapterID, event.CredentialID, event.Operation, event.Destination, string(event.Decision), event.Reason, ts(when))
	return err
}

func (r *Repository) CredentialAccessEvents(credentialID string) ([]credentials.AccessEvent, error) {
	rows, err := r.db.Query(`SELECT event_id,transaction_id,effect_id,adapter_id,credential_id,operation,destination,decision,reason,created_at FROM credential_access_events WHERE credential_id=? ORDER BY sequence`, credentialID)
	if err != nil {
		return nil, err
	}
	out := make([]credentials.AccessEvent, 0, len(rows))
	for _, row := range rows {
		created, err := time.Parse(time.RFC3339Nano, String(row, "created_at"))
		if err != nil {
			return nil, err
		}
		out = append(out, credentials.AccessEvent{EventID: String(row, "event_id"), TransactionID: String(row, "transaction_id"), EffectID: String(row, "effect_id"), AdapterID: String(row, "adapter_id"), CredentialID: String(row, "credential_id"), Operation: String(row, "operation"), Destination: String(row, "destination"), Decision: credentials.AccessDecision(String(row, "decision")), Reason: String(row, "reason"), CreatedAt: created})
	}
	return out, nil
}

func (r *Repository) CredentialMetadataCounts() (map[string]int64, error) {
	adapters, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM adapter_identities WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	bindings, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM credential_bindings WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	events, err := r.db.QueryOne(`SELECT COUNT(*) AS n FROM credential_access_events`)
	if err != nil {
		return nil, err
	}
	return map[string]int64{"enabled_adapters": Int64(adapters, "n"), "enabled_credentials": Int64(bindings, "n"), "access_events": Int64(events, "n")}, nil
}
