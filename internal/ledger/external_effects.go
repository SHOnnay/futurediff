package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

type PrepareExternalEffectInput struct {
	Effect domain.ExternalEffect
}

func (r *Repository) CreateExternalEffect(input PrepareExternalEffectInput) (domain.ExternalEffect, error) {
	e := input.Effect
	if e.EffectID == "" || e.TransactionID == "" || e.AdapterIdentity == "" || e.ToolIdentity == "" || e.CredentialID == "" || e.Operation == "" || e.Destination == "" || e.InputDigest == "" || e.PreparedDigest == "" || e.PreviewDigest == "" || e.IdempotencyKey == "" {
		return domain.ExternalEffect{}, errors.New("external effect is missing required durable identity")
	}
	if e.Status == "" {
		e.Status = domain.EffectVerified
	}
	if e.Status != domain.EffectVerified {
		return domain.ExternalEffect{}, errors.New("new external effects must be verified before durable preparation")
	}
	if e.CommitRank == 0 {
		e.CommitRank = 100
	}
	now := time.Now().UTC()
	e.CreatedAt, e.UpdatedAt, e.Revision = now, now, 1
	resources, err := json.Marshal(e.ResourceVersions)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	err = r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT * FROM transactions WHERE transaction_id=?", e.TransactionID)
		if err != nil {
			return err
		}
		cur, err := transactionFromRow(row)
		if err != nil {
			return err
		}
		if cur.Status != domain.StateActive && cur.Status != domain.StateSealed {
			return fmt.Errorf("external effects may be prepared only while transaction is active or sealed, found %s", cur.Status)
		}
		_, err = tx.Exec(`INSERT INTO effects(effect_id,transaction_id,tool_identity,adapter_identity,effect_class,risk_level,input_digest,prepared_handle_ref,idempotency_key,status,preview_ref,reversibility,commit_rank,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.EffectID, e.TransactionID, e.ToolIdentity, e.AdapterIdentity, e.EffectClass, nullString(e.RiskLevel), e.InputDigest, e.PreparedDigest, e.IdempotencyKey, string(e.Status), e.PreviewDigest, e.Reversibility, int64(e.CommitRank), e.Revision, ts(now), ts(now))
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO effect_documents(effect_id,credential_id,operation,destination,input_json,prepared_json,prepared_digest,preview_json,preview_digest,resource_versions_json,support_level,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.EffectID, e.CredentialID, e.Operation, e.Destination, e.InputJSON, e.PreparedJSON, e.PreparedDigest, e.PreviewJSON, e.PreviewDigest, string(resources), e.SupportLevel, ts(now), ts(now))
		if err != nil {
			return err
		}
		seenDependencies := map[string]bool{}
		for _, dependencyID := range e.DependsOn {
			if dependencyID == "" || dependencyID == e.EffectID || seenDependencies[dependencyID] {
				return errors.New("effect dependencies must be unique, non-empty, and cannot reference self")
			}
			seenDependencies[dependencyID] = true
			dep, err := tx.QueryOne("SELECT transaction_id,status FROM effects WHERE effect_id=?", dependencyID)
			if err != nil {
				return fmt.Errorf("dependency %s: %w", dependencyID, err)
			}
			if String(dep, "transaction_id") != e.TransactionID {
				return errors.New("effect dependency belongs to another transaction")
			}
			if domain.EffectState(String(dep, "status")) == domain.EffectSuperseded {
				return errors.New("effect cannot depend on a superseded effect")
			}
			if _, err := tx.Exec("INSERT INTO effect_dependencies(effect_id,depends_on_effect_id) VALUES(?,?)", e.EffectID, dependencyID); err != nil {
				return err
			}
		}
		changes, err := tx.Exec(`UPDATE transactions SET revision=revision+1,material_revision=material_revision+1,approval_digest=NULL,updated_at=? WHERE transaction_id=? AND revision=?`, ts(now), e.TransactionID, cur.Revision)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEffectEvent(tx, e.TransactionID, e.EffectID, "effect.prepared", map[string]any{"adapter": e.AdapterIdentity, "tool": e.ToolIdentity, "input_digest": e.InputDigest, "prepared_digest": e.PreparedDigest, "preview_digest": e.PreviewDigest, "destination": e.Destination, "resource_versions": e.ResourceVersions, "idempotency_key": e.IdempotencyKey}, 0, now)
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return r.ExternalEffect(e.EffectID)
}

func (r *Repository) ExternalEffect(effectID string) (domain.ExternalEffect, error) {
	row, err := r.db.QueryOne(`SELECT e.*,d.credential_id,d.operation,d.destination,d.input_json,d.prepared_json,d.prepared_digest,d.preview_json,d.preview_digest,d.resource_versions_json,d.support_level FROM effects e JOIN effect_documents d ON d.effect_id=e.effect_id WHERE e.effect_id=?`, effectID)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	effect, err := externalEffectFromRow(row)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	effect.DependsOn, err = r.effectDependencies(effectID)
	return effect, err
}

func (r *Repository) ExternalEffects(transactionID string) ([]domain.ExternalEffect, error) {
	rows, err := r.db.Query(`SELECT e.*,d.credential_id,d.operation,d.destination,d.input_json,d.prepared_json,d.prepared_digest,d.preview_json,d.preview_digest,d.resource_versions_json,d.support_level FROM effects e JOIN effect_documents d ON d.effect_id=e.effect_id WHERE e.transaction_id=? AND e.status<>'superseded' ORDER BY e.commit_rank,e.effect_id`, transactionID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.ExternalEffect, 0, len(rows))
	for _, row := range rows {
		e, err := externalEffectFromRow(row)
		if err != nil {
			return nil, err
		}
		e.DependsOn, err = r.effectDependencies(e.EffectID)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (r *Repository) effectDependencies(effectID string) ([]string, error) {
	rows, err := r.db.Query("SELECT depends_on_effect_id FROM effect_dependencies WHERE effect_id=? ORDER BY depends_on_effect_id", effectID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, String(row, "depends_on_effect_id"))
	}
	return out, nil
}

func (r *Repository) RefreshExternalEffect(effectID, preparedJSON, preparedDigest, previewJSON, previewDigest string, resourceVersions map[string]string) (domain.ExternalEffect, error) {
	if preparedDigest == "" || previewDigest == "" {
		return domain.ExternalEffect{}, errors.New("prepared and preview digests are required")
	}
	resources, err := json.Marshal(resourceVersions)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	now := time.Now().UTC()
	err = r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne(`SELECT e.*,t.status AS transaction_status,t.revision AS transaction_revision FROM effects e JOIN transactions t ON t.transaction_id=e.transaction_id WHERE e.effect_id=?`, effectID)
		if err != nil {
			return err
		}
		txState := domain.TransactionState(String(row, "transaction_status"))
		if txState != domain.StateActive && txState != domain.StateStale {
			return fmt.Errorf("effect refresh requires active or stale transaction, found %s", txState)
		}
		state := domain.EffectState(String(row, "status"))
		if state != domain.EffectVerified && state != domain.EffectPrepared && state != domain.EffectUnknown {
			return fmt.Errorf("effect in %s cannot be refreshed", state)
		}
		transactionID := String(row, "transaction_id")
		_, err = tx.Exec(`UPDATE effect_documents SET prepared_json=?,prepared_digest=?,preview_json=?,preview_digest=?,resource_versions_json=?,updated_at=? WHERE effect_id=?`, preparedJSON, preparedDigest, previewJSON, previewDigest, string(resources), ts(now), effectID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE effects SET prepared_handle_ref=?,preview_ref=?,status='verified',commit_request_digest=NULL,commit_fencing_token=NULL,revision=revision+1,updated_at=? WHERE effect_id=?`, preparedDigest, previewDigest, ts(now), effectID)
		if err != nil {
			return err
		}
		changes, err := tx.Exec(`UPDATE transactions SET revision=revision+1,material_revision=material_revision+1,approval_digest=NULL,updated_at=? WHERE transaction_id=? AND revision=?`, ts(now), transactionID, Int64(row, "transaction_revision"))
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEffectEvent(tx, transactionID, effectID, "effect.refreshed", map[string]any{"prepared_digest": preparedDigest, "preview_digest": previewDigest, "resource_versions": resourceVersions}, 0, now)
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return r.ExternalEffect(effectID)
}

func (r *Repository) AcquireLease(name, owner string, ttl time.Duration) (int64, error) {
	if name == "" || owner == "" || ttl <= 0 {
		return 0, errors.New("lease name, owner, and positive ttl are required")
	}
	now := time.Now().UTC()
	nowMS := now.UnixMilli()
	expires := now.Add(ttl).UnixMilli()
	var token int64
	err := r.db.WithTx(func(tx *Tx) error {
		rows, err := tx.Query("SELECT owner_id,fencing_token,expires_at_ms FROM leases WHERE lease_name=?", name)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			token = 1
			_, err = tx.Exec(`INSERT INTO leases(lease_name,owner_id,fencing_token,acquired_at_ms,expires_at_ms) VALUES(?,?,?,?,?)`, name, owner, token, nowMS, expires)
			return err
		}
		row := rows[0]
		currentOwner := String(row, "owner_id")
		currentToken := Int64(row, "fencing_token")
		currentExpiry := Int64(row, "expires_at_ms")
		if currentOwner != owner && currentExpiry > nowMS {
			return fmt.Errorf("lease %s is held by another coordinator", name)
		}
		token = currentToken + 1
		changes, err := tx.Exec(`UPDATE leases SET owner_id=?,fencing_token=?,acquired_at_ms=?,expires_at_ms=? WHERE lease_name=? AND fencing_token=?`, owner, token, nowMS, expires, name, currentToken)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent lease acquisition")
		}
		return nil
	})
	return token, err
}

func validateFencing(tx *Tx, transactionID string, token int64) error {
	row, err := tx.QueryOne("SELECT fencing_token,expires_at_ms FROM leases WHERE lease_name=?", "transaction:"+transactionID)
	if err != nil {
		return err
	}
	if Int64(row, "fencing_token") != token {
		return errors.New("stale coordinator fencing token")
	}
	if Int64(row, "expires_at_ms") <= time.Now().UTC().UnixMilli() {
		return errors.New("coordinator lease expired")
	}
	return nil
}

func (r *Repository) BeginEffectAttempt(effectID, phase, requestDigest string, fencingToken int64) (domain.EffectAttempt, error) {
	if phase != "commit" && phase != "status" && phase != "abort" && phase != "compensate" {
		return domain.EffectAttempt{}, errors.New("unsupported effect attempt phase")
	}
	if requestDigest == "" || fencingToken <= 0 {
		return domain.EffectAttempt{}, errors.New("request digest and fencing token are required")
	}
	attempt := domain.EffectAttempt{AttemptID: domain.NewID("attempt"), EffectID: effectID, Phase: phase, RequestDigest: requestDigest, FencingToken: fencingToken, Outcome: "intent", StartedAt: time.Now().UTC()}
	err := r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT transaction_id,status,revision FROM effects WHERE effect_id=?", effectID)
		if err != nil {
			return err
		}
		attempt.TransactionID = String(row, "transaction_id")
		if err := validateFencing(tx, attempt.TransactionID, fencingToken); err != nil {
			return err
		}
		txRow, err := tx.QueryOne("SELECT status FROM transactions WHERE transaction_id=?", attempt.TransactionID)
		if err != nil {
			return err
		}
		if state := domain.TransactionState(String(txRow, "status")); state != domain.StateCommitting && state != domain.StateNeedsReconciliation {
			return fmt.Errorf("effect attempts require committing or reconciliation transaction, found %s", state)
		}
		current := domain.EffectState(String(row, "status"))
		if phase == "commit" && current != domain.EffectVerified && current != domain.EffectPrepared {
			return fmt.Errorf("commit attempt requires verified/prepared effect, found %s", current)
		}
		if phase == "status" && current != domain.EffectUnknown && current != domain.EffectCommitting {
			return fmt.Errorf("status attempt requires unknown/committing effect, found %s", current)
		}
		_, err = tx.Exec(`INSERT INTO effect_attempts(attempt_id,effect_id,transaction_id,phase,request_digest,fencing_token,outcome,started_at) VALUES(?,?,?,?,?,?,?,?)`, attempt.AttemptID, effectID, attempt.TransactionID, phase, requestDigest, fencingToken, attempt.Outcome, ts(attempt.StartedAt))
		if err != nil {
			return err
		}
		if phase == "commit" {
			changes, err := tx.Exec(`UPDATE effects SET status='committing',commit_request_digest=?,commit_fencing_token=?,revision=revision+1,updated_at=? WHERE effect_id=? AND revision=?`, requestDigest, fencingToken, ts(attempt.StartedAt), effectID, Int64(row, "revision"))
			if err != nil {
				return err
			}
			if changes != 1 {
				return errors.New("concurrent effect update")
			}
		}
		return appendEffectEvent(tx, attempt.TransactionID, effectID, "effect."+phase+".intent", map[string]any{"attempt_id": attempt.AttemptID, "request_digest": requestDigest}, fencingToken, attempt.StartedAt)
	})
	return attempt, err
}

func (r *Repository) MarkEffectUnknown(attempt domain.EffectAttempt, class, message string) (domain.ExternalEffect, error) {
	now := time.Now().UTC()
	err := r.db.WithTx(func(tx *Tx) error {
		if err := validateFencing(tx, attempt.TransactionID, attempt.FencingToken); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE effect_attempts SET outcome='unknown',error_class=?,error_message=?,finished_at=? WHERE attempt_id=? AND outcome='intent'`, nullString(class), nullString(message), ts(now), attempt.AttemptID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE effects SET status='unknown',revision=revision+1,updated_at=? WHERE effect_id=?`, ts(now), attempt.EffectID)
		if err != nil {
			return err
		}
		return appendEffectEvent(tx, attempt.TransactionID, attempt.EffectID, "effect.unknown", map[string]any{"attempt_id": attempt.AttemptID, "error_class": class, "message": message}, attempt.FencingToken, now)
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return r.ExternalEffect(attempt.EffectID)
}

func (r *Repository) MarkEffectDefiniteFailure(attempt domain.EffectAttempt, status int, class, message string) (domain.ExternalEffect, error) {
	now := time.Now().UTC()
	err := r.db.WithTx(func(tx *Tx) error {
		if err := validateFencing(tx, attempt.TransactionID, attempt.FencingToken); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE effect_attempts SET outcome='definite_failure',http_status=?,error_class=?,error_message=?,finished_at=? WHERE attempt_id=? AND outcome='intent'`, int64(status), nullString(class), nullString(message), ts(now), attempt.AttemptID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE effects SET status='verified',revision=revision+1,updated_at=? WHERE effect_id=?`, ts(now), attempt.EffectID)
		if err != nil {
			return err
		}
		return appendEffectEvent(tx, attempt.TransactionID, attempt.EffectID, "effect.commit.rejected", map[string]any{"attempt_id": attempt.AttemptID, "http_status": status, "error_class": class, "message": message}, attempt.FencingToken, now)
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return r.ExternalEffect(attempt.EffectID)
}

func (r *Repository) RecordEffectCommitted(attempt domain.EffectAttempt, receipt domain.EffectReceipt) (domain.ExternalEffect, error) {
	now := receipt.CommittedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if receipt.ReceiptID == "" {
		receipt.ReceiptID = domain.NewID("receipt")
	}
	receipt.EffectID, receipt.FencingToken, receipt.CreatedAt = attempt.EffectID, attempt.FencingToken, now
	err := r.db.WithTx(func(tx *Tx) error {
		if err := validateFencing(tx, attempt.TransactionID, attempt.FencingToken); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE effect_attempts SET outcome='success',response_digest=?,finished_at=? WHERE attempt_id=? AND outcome='intent'`, nullString(receipt.ResponseDigest), ts(now), attempt.AttemptID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT OR IGNORE INTO receipts(receipt_id,effect_id,provider_operation_id,provider_resource_id,request_digest,response_digest,status_query_ref,fencing_token,committed_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, receipt.ReceiptID, receipt.EffectID, nullString(receipt.ProviderOperationID), nullString(receipt.ProviderResourceID), receipt.RequestDigest, nullString(receipt.ResponseDigest), nullString(receipt.StatusQueryRef), receipt.FencingToken, ts(now), ts(now))
		if err != nil {
			return err
		}
		row, err := tx.QueryOne("SELECT request_digest,provider_resource_id FROM receipts WHERE effect_id=?", receipt.EffectID)
		if err != nil {
			return err
		}
		if String(row, "request_digest") != receipt.RequestDigest || String(row, "provider_resource_id") != receipt.ProviderResourceID {
			return errors.New("existing effect receipt conflicts with provider result")
		}
		_, err = tx.Exec(`UPDATE effects SET status='committed',revision=revision+1,updated_at=? WHERE effect_id=?`, ts(now), receipt.EffectID)
		if err != nil {
			return err
		}
		return appendEffectEvent(tx, attempt.TransactionID, receipt.EffectID, "effect.committed", receipt, attempt.FencingToken, now)
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return r.ExternalEffect(receipt.EffectID)
}

func (r *Repository) RearmEffect(effectID string, attempt *domain.EffectAttempt, reason string) (domain.ExternalEffect, error) {
	now := time.Now().UTC()
	err := r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT transaction_id,status FROM effects WHERE effect_id=?", effectID)
		if err != nil {
			return err
		}
		transactionID := String(row, "transaction_id")
		if attempt != nil {
			if err := validateFencing(tx, transactionID, attempt.FencingToken); err != nil {
				return err
			}
			_, err = tx.Exec(`UPDATE effect_attempts SET outcome='not_found',error_message=?,finished_at=? WHERE attempt_id=? AND outcome='intent'`, nullString(reason), ts(now), attempt.AttemptID)
			if err != nil {
				return err
			}
		}
		_, err = tx.Exec(`UPDATE effects SET status='verified',revision=revision+1,updated_at=? WHERE effect_id=?`, ts(now), effectID)
		if err != nil {
			return err
		}
		return appendEffectEvent(tx, transactionID, effectID, "effect.rearmed", map[string]any{"reason": reason}, 0, now)
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return r.ExternalEffect(effectID)
}

func (r *Repository) EffectReceipt(effectID string) (domain.EffectReceipt, error) {
	row, err := r.db.QueryOne("SELECT * FROM receipts WHERE effect_id=?", effectID)
	if err != nil {
		return domain.EffectReceipt{}, err
	}
	committed, err := parseTime(String(row, "committed_at"))
	if err != nil {
		return domain.EffectReceipt{}, err
	}
	created, err := parseTime(String(row, "created_at"))
	if err != nil {
		return domain.EffectReceipt{}, err
	}
	return domain.EffectReceipt{ReceiptID: String(row, "receipt_id"), EffectID: String(row, "effect_id"), ProviderOperationID: String(row, "provider_operation_id"), ProviderResourceID: String(row, "provider_resource_id"), RequestDigest: String(row, "request_digest"), ResponseDigest: String(row, "response_digest"), StatusQueryRef: String(row, "status_query_ref"), FencingToken: Int64(row, "fencing_token"), CommittedAt: committed, CreatedAt: created}, nil
}

func (r *Repository) RecordMaterializedRef(id string, ref domain.MaterializedRef) error {
	now := ref.MaterializedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT status FROM transactions WHERE transaction_id=?", id)
		if err != nil {
			return err
		}
		state := domain.TransactionState(String(row, "status"))
		if state != domain.StateCommitting && state != domain.StateNeedsReconciliation {
			return fmt.Errorf("cannot record materialized ref while transaction is %s", state)
		}
		_, err = tx.Exec(`INSERT OR IGNORE INTO materialized_repository_refs(transaction_id,ref_name,commit_oid,resulting_tree_oid,materialized_at) VALUES(?,?,?,?,?)`, id, ref.RefName, ref.CommitOID, ref.ResultingTreeOID, ts(now))
		if err != nil {
			return err
		}
		existing, err := tx.QueryOne("SELECT ref_name,commit_oid,resulting_tree_oid FROM materialized_repository_refs WHERE transaction_id=?", id)
		if err != nil {
			return err
		}
		if String(existing, "ref_name") != ref.RefName || String(existing, "commit_oid") != ref.CommitOID || String(existing, "resulting_tree_oid") != ref.ResultingTreeOID {
			return errors.New("materialized repository ref conflicts with durable record")
		}
		return appendEvent(tx, id, "repository.materialized", ref, now)
	})
}

func (r *Repository) MaterializedRef(id string) (domain.MaterializedRef, error) {
	row, err := r.db.QueryOne("SELECT * FROM materialized_repository_refs WHERE transaction_id=?", id)
	if err != nil {
		return domain.MaterializedRef{}, err
	}
	at, err := parseTime(String(row, "materialized_at"))
	if err != nil {
		return domain.MaterializedRef{}, err
	}
	return domain.MaterializedRef{TransactionID: id, RefName: String(row, "ref_name"), CommitOID: String(row, "commit_oid"), ResultingTreeOID: String(row, "resulting_tree_oid"), MaterializedAt: at}, nil
}

func (r *Repository) FinalizeTransactionCommit(id string) (domain.Transaction, error) {
	now := time.Now().UTC()
	err := r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT * FROM transactions WHERE transaction_id=?", id)
		if err != nil {
			return err
		}
		cur, err := transactionFromRow(row)
		if err != nil {
			return err
		}
		if cur.Status != domain.StateCommitting && cur.Status != domain.StateNeedsReconciliation {
			return fmt.Errorf("expected committing or needs_reconciliation, found %s", cur.Status)
		}
		if _, err := tx.QueryOne("SELECT transaction_id FROM materialized_repository_refs WHERE transaction_id=?", id); err != nil {
			return errors.New("repository materialization receipt is required")
		}
		pending, err := tx.Query(`SELECT effect_id,status FROM effects WHERE transaction_id=? AND status<>'superseded' AND status<>'committed'`, id)
		if err != nil {
			return err
		}
		if len(pending) > 0 {
			return fmt.Errorf("external effects are not committed: %s=%s", String(pending[0], "effect_id"), String(pending[0], "status"))
		}
		changes, err := tx.Exec(`UPDATE transactions SET status='committed',revision=revision+1,updated_at=? WHERE transaction_id=? AND revision=?`, ts(now), id, cur.Revision)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEvent(tx, id, "transaction.committed", map[string]any{"external_effects_complete": true}, now)
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(id)
}

func externalEffectFromRow(row Row) (domain.ExternalEffect, error) {
	created, err := parseTime(String(row, "created_at"))
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	updated, err := parseTime(String(row, "updated_at"))
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	resources := map[string]string{}
	if raw := String(row, "resource_versions_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &resources); err != nil {
			return domain.ExternalEffect{}, err
		}
	}
	return domain.ExternalEffect{EffectID: String(row, "effect_id"), TransactionID: String(row, "transaction_id"), ToolIdentity: String(row, "tool_identity"), AdapterIdentity: String(row, "adapter_identity"), EffectClass: String(row, "effect_class"), RiskLevel: String(row, "risk_level"), CredentialID: String(row, "credential_id"), Operation: String(row, "operation"), Destination: String(row, "destination"), InputJSON: String(row, "input_json"), InputDigest: String(row, "input_digest"), PreparedJSON: String(row, "prepared_json"), PreparedDigest: String(row, "prepared_digest"), PreviewJSON: String(row, "preview_json"), PreviewDigest: String(row, "preview_digest"), ResourceVersions: resources, IdempotencyKey: String(row, "idempotency_key"), CommitRequestDigest: String(row, "commit_request_digest"), CommitFencingToken: Int64(row, "commit_fencing_token"), Status: domain.EffectState(String(row, "status")), Reversibility: String(row, "reversibility"), CommitRank: int(Int64(row, "commit_rank")), SupportLevel: String(row, "support_level"), Revision: Int64(row, "revision"), CreatedAt: created, UpdatedAt: updated}, nil
}

func appendEffectEvent(tx *Tx, transactionID, effectID, kind string, payload any, fencingToken int64, at time.Time) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return appendChainedEvent(tx, transactionID, effectID, kind, b, domain.SHA256Bytes(b), fencingToken, ts(at))
}

func (r *Repository) AbortPreparedEffects(transactionID, reason string) error {
	now := time.Now().UTC()
	return r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT status FROM transactions WHERE transaction_id=?", transactionID)
		if err != nil {
			return err
		}
		if state := domain.TransactionState(String(row, "status")); state != domain.StateAborting {
			return fmt.Errorf("prepared effects may be aborted only while transaction is aborting, found %s", state)
		}
		rows, err := tx.Query(`SELECT effect_id,status FROM effects WHERE transaction_id=? AND status IN ('prepared','verified')`, transactionID)
		if err != nil {
			return err
		}
		for _, effectRow := range rows {
			effectID := String(effectRow, "effect_id")
			if _, err := tx.Exec(`UPDATE effects SET status='aborted',revision=revision+1,updated_at=? WHERE effect_id=?`, ts(now), effectID); err != nil {
				return err
			}
			if err := appendEffectEvent(tx, transactionID, effectID, "effect.aborted", map[string]any{"reason": reason}, 0, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// VerificationMaterial binds deterministic verification to both the exact
// repository patch and every prepared external-effect preview/resource version.
// It deliberately excludes verification results to avoid a digest cycle.
func (r *Repository) VerificationMaterial(transactionID string) (string, error) {
	patch, err := r.Patch(transactionID)
	if err != nil {
		return "", err
	}
	effects, err := r.ExternalEffects(transactionID)
	if err != nil {
		return "", err
	}
	materialEffects := make([]map[string]any, 0, len(effects))
	for _, effect := range effects {
		if effect.Status == domain.EffectSuperseded || effect.Status == domain.EffectAborted {
			continue
		}
		materialEffects = append(materialEffects, map[string]any{
			"effect_id":         effect.EffectID,
			"adapter_identity":  effect.AdapterIdentity,
			"tool_identity":     effect.ToolIdentity,
			"input_digest":      effect.InputDigest,
			"prepared_digest":   effect.PreparedDigest,
			"preview_digest":    effect.PreviewDigest,
			"resource_versions": effect.ResourceVersions,
			"destination":       effect.Destination,
			"operation":         effect.Operation,
			"depends_on":        effect.DependsOn,
			"idempotency_key":   effect.IdempotencyKey,
			"commit_rank":       effect.CommitRank,
		})
	}
	return domain.Digest(map[string]any{"format_version": "0.2", "transaction_id": transactionID, "patch_approval_material_digest": patch.ApprovalMaterialDigest, "patch_sha256": patch.PatchSHA256, "staged_tree_oid": patch.StagedTreeOID, "external_effects": materialEffects})
}
