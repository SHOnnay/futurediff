package ledger

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Repository struct{ db *DB }

func OpenRepository(path string) (*Repository, error) {
	db, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure ledger permissions: %w", err)
	}
	r := &Repository{db: db}
	if err := r.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}
func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) migrate() error {
	if err := r.db.ExecScript("CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);"); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for i, e := range entries {
		version := int64(i + 1)
		rows, err := r.db.Query("SELECT version FROM schema_migrations WHERE version=?", version)
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			continue
		}
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return err
		}
		if err := r.db.WithTx(func(tx *Tx) error {
			if err := tx.db.execScriptLocked(string(b)); err != nil {
				return err
			}
			_, err := tx.Exec("INSERT INTO schema_migrations(version,applied_at) VALUES(?,?)", version, time.Now().UTC().Format(time.RFC3339Nano))
			return err
		}); err != nil {
			return fmt.Errorf("migration %s: %w", e.Name(), err)
		}
	}
	if err := r.verifyMigrationArtifacts(); err != nil {
		return err
	}
	if err := r.backfillEventChains(); err != nil {
		return err
	}
	return r.backfillAPIAccessChain()
}

func (r *Repository) verifyMigrationArtifacts() error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return r.db.WithTx(func(tx *Tx) error {
		for i, entry := range entries {
			version := int64(i + 1)
			data, err := migrationFS.ReadFile("migrations/" + entry.Name())
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			digest := hex.EncodeToString(sum[:])
			rows, err := tx.Query("SELECT name,sha256 FROM migration_artifacts WHERE version=?", version)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				_, err = tx.Exec("INSERT INTO migration_artifacts(version,name,sha256,recorded_at) VALUES(?,?,?,?)", version, entry.Name(), digest, ts(time.Now().UTC()))
				if err != nil {
					return err
				}
				continue
			}
			if String(rows[0], "name") != entry.Name() || String(rows[0], "sha256") != digest {
				return fmt.Errorf("migration artifact mismatch for version %d", version)
			}
		}
		return nil
	})
}

type CreateInput struct {
	Transaction domain.Transaction
	Workspace   domain.Workspace
}

func (r *Repository) Create(input CreateInput) (domain.Transaction, error) {
	now := input.Transaction.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	t := input.Transaction
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.OwnerPrincipalID == "" {
		t.OwnerPrincipalID = "local:operator"
	}
	t.ProtocolVersion = "0.1"
	t.Status = domain.StateActive
	t.Revision = 1
	w := input.Workspace
	w.CreatedAt = now
	err := r.db.WithTx(func(tx *Tx) error {
		_, err := tx.Exec(`INSERT INTO transactions(transaction_id,owner_principal_id,protocol_version,mode,agent_adapter,agent_session_id,workspace_identity,base_revision,status,policy_version,revision,material_revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, t.ID, t.OwnerPrincipalID, t.ProtocolVersion, t.Mode, nullString(t.AgentAdapter), nullString(t.AgentSessionID), w.WorkspacePath, w.BaseOID, string(t.Status), t.PolicyVersion, t.Revision, t.MaterialRevision, ts(now), ts(now))
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO transaction_workspaces(transaction_id,repository_root,git_common_dir,source_head_ref,base_oid,object_format,workspace_path,artifacts_path,dirty_policy,source_status_digest,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, t.ID, w.RepositoryRoot, w.GitCommonDir, nullString(w.SourceHeadRef), w.BaseOID, w.ObjectFormat, w.WorkspacePath, w.ArtifactsPath, w.DirtyPolicy, w.SourceStatusDigest, ts(now))
		if err != nil {
			return err
		}
		if err := appendEvent(tx, t.ID, "transaction.created", map[string]any{"state": "created", "mode": t.Mode, "owner_principal_id": t.OwnerPrincipalID}, now); err != nil {
			return err
		}
		if err := appendTransactionAccessEvent(tx, t.ID, t.OwnerPrincipalID, t.OwnerPrincipalID, "created", AccessAdmin, "", now); err != nil {
			return err
		}
		return appendEvent(tx, t.ID, "transaction.activated", map[string]any{"from": "created", "to": "active", "revision": 1}, now)
	})
	return t, err
}

func (r *Repository) Get(id string) (domain.Transaction, error) {
	row, err := r.db.QueryOne("SELECT * FROM transactions WHERE transaction_id=?", id)
	if err != nil {
		return domain.Transaction{}, err
	}
	return transactionFromRow(row)
}
func (r *Repository) Workspace(id string) (domain.Workspace, error) {
	row, err := r.db.QueryOne("SELECT * FROM transaction_workspaces WHERE transaction_id=?", id)
	if err != nil {
		return domain.Workspace{}, err
	}
	return workspaceFromRow(row)
}
func (r *Repository) Patch(id string) (domain.Patch, error) {
	row, err := r.db.QueryOne("SELECT * FROM staged_patches WHERE transaction_id=?", id)
	if err != nil {
		return domain.Patch{}, err
	}
	return patchFromRow(row)
}

func (r *Repository) Transition(id string, expected, next domain.TransactionState, actor, reason string, materialChanged, invalidateApproval bool) (domain.Transaction, error) {
	if err := domain.ValidateTransition(expected, next); err != nil {
		return domain.Transaction{}, err
	}
	now := time.Now().UTC()
	err := r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT * FROM transactions WHERE transaction_id=?", id)
		if err != nil {
			return err
		}
		current, err := transactionFromRow(row)
		if err != nil {
			return err
		}
		if current.Status != expected {
			return fmt.Errorf("state conflict: expected %s found %s", expected, current.Status)
		}
		material := int64(0)
		if materialChanged {
			material = 1
		}
		approval := any(current.ApprovalDigest)
		if invalidateApproval {
			approval = nil
		}
		changes, err := tx.Exec(`UPDATE transactions SET status=?,revision=revision+1,material_revision=material_revision+?,approval_digest=?,sealed_at=CASE WHEN ?='sealed' THEN ? ELSE sealed_at END,updated_at=? WHERE transaction_id=? AND revision=?`, string(next), material, approval, string(next), ts(now), ts(now), id, current.Revision)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEvent(tx, id, "transaction."+string(next), map[string]any{"from": expected, "to": next, "actor": actor, "reason": reason, "material_changed": materialChanged}, now)
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(id)
}

func (r *Repository) RecordPatch(id string, p domain.Patch) (domain.Transaction, error) {
	now := p.GeneratedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	p.GeneratedAt = now
	err := r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT * FROM transactions WHERE transaction_id=?", id)
		if err != nil {
			return err
		}
		cur, err := transactionFromRow(row)
		if err != nil {
			return err
		}
		if cur.Status != domain.StateActive {
			return fmt.Errorf("expected active, found %s", cur.Status)
		}
		paths, _ := json.Marshal(p.ChangedPaths)
		_, err = tx.Exec(`INSERT INTO staged_patches(transaction_id,patch_path,patch_sha256,patch_size_bytes,staged_tree_oid,changed_path_count,changed_paths_json,approval_material_digest,generated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, p.PatchPath, p.PatchSHA256, p.PatchSizeBytes, p.StagedTreeOID, int64(len(p.ChangedPaths)), string(paths), p.ApprovalMaterialDigest, ts(now))
		if err != nil {
			return err
		}
		changes, err := tx.Exec(`UPDATE transactions SET status='sealed',revision=revision+1,material_revision=material_revision+1,approval_digest=NULL,sealed_at=?,updated_at=? WHERE transaction_id=? AND revision=?`, ts(now), ts(now), id, cur.Revision)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEvent(tx, id, "repository.patch_sealed", p, now)
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(id)
}

func (r *Repository) RecordVerification(id string, report domain.VerificationReport) (domain.Transaction, error) {
	now := report.CreatedAt.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	err := r.db.WithTx(func(tx *Tx) error {
		row, err := tx.QueryOne("SELECT * FROM transactions WHERE transaction_id=?", id)
		if err != nil {
			return err
		}
		cur, err := transactionFromRow(row)
		if err != nil {
			return err
		}
		if cur.Status != domain.StateSealed && cur.Status != domain.StateStale {
			return fmt.Errorf("expected sealed or stale, found %s", cur.Status)
		}
		recalculated := verificationOutcome(report.Results)
		if recalculated != report.Outcome {
			return fmt.Errorf("verification outcome mismatch: declared %s calculated %s", report.Outcome, recalculated)
		}
		target := domain.StateFailedVerification
		if report.Outcome == "pass" {
			target = domain.StateReady
		}
		_, err = tx.Exec(`INSERT INTO verification_runs(verification_id,transaction_id,contract_id,contract_digest,material_digest,material_revision,outcome,verification_digest,policy_version,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, report.VerificationID, id, report.ContractID, report.ContractDigest, report.MaterialDigest, cur.MaterialRevision, report.Outcome, report.VerificationDigest, report.PolicyVersion, ts(now))
		if err != nil {
			return err
		}
		for i, res := range report.Results {
			_, err = tx.Exec(`INSERT INTO verification_check_results(verification_id,check_id,ordinal,required,status,cache_hit,check_spec_digest,cache_key,evidence_digest,message) VALUES(?,?,?,?,?,0,?,?,?,?)`, report.VerificationID, res.CheckID, int64(i), res.Required, res.Status, res.CheckSpecDigest, res.CacheKey, res.EvidenceDigest, nullString(res.Message))
			if err != nil {
				return err
			}
		}
		changes, err := tx.Exec(`UPDATE transactions SET status=?,revision=revision+1,approval_digest=NULL,updated_at=? WHERE transaction_id=? AND revision=?`, string(target), ts(now), id, cur.Revision)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEvent(tx, id, "verification.completed", report, now)
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(id)
}

func (r *Repository) Approve(id, digest, approver string) (domain.Transaction, error) {
	return r.ApproveWithEvidence(id, digest, approver, "", nil)
}

func (r *Repository) ApproveWithEvidence(id, digest, approver, signatureRef string, expiresAt *time.Time) (domain.Transaction, error) {
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
		if cur.Status != domain.StateReady {
			return fmt.Errorf("expected ready, found %s", cur.Status)
		}
		expected, err := r.approvalMaterialTx(tx, id, cur)
		if err != nil {
			return err
		}
		if digest != expected {
			return errors.New("approval digest mismatch")
		}
		_, err = tx.Exec(`INSERT INTO approvals(approval_id,transaction_id,transaction_digest,material_revision,approver_identity,scope,decision,signature_ref,created_at,expires_at) VALUES(?,?,?,?,?,'entire_transaction','approved',?,?,?)`, domain.NewID("approval"), id, digest, cur.MaterialRevision, approver, nullString(signatureRef), ts(now), nullTime(expiresAt))
		if err != nil {
			return err
		}
		changes, err := tx.Exec(`UPDATE transactions SET approval_digest=?,revision=revision+1,updated_at=? WHERE transaction_id=? AND revision=?`, digest, ts(now), id, cur.Revision)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEvent(tx, id, "transaction.approved", map[string]any{"digest": digest, "approver": approver, "signature_ref": signatureRef, "expires_at": expiresAt}, now)
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(id)
}

func (r *Repository) ApprovalMaterial(id string) (string, error) {
	cur, err := r.Get(id)
	if err != nil {
		return "", err
	}
	return r.approvalMaterial(id, cur)
}
func (r *Repository) approvalMaterial(id string, cur domain.Transaction) (string, error) {
	return r.approvalMaterialWith(func(q string, args ...Value) ([]Row, error) { return r.db.Query(q, args...) }, id, cur)
}
func (r *Repository) approvalMaterialTx(tx *Tx, id string, cur domain.Transaction) (string, error) {
	return r.approvalMaterialWith(func(q string, args ...Value) ([]Row, error) { return tx.Query(q, args...) }, id, cur)
}
func (r *Repository) approvalMaterialWith(query func(string, ...Value) ([]Row, error), id string, cur domain.Transaction) (string, error) {
	patchRows, err := query("SELECT * FROM staged_patches WHERE transaction_id=?", id)
	if err != nil || len(patchRows) == 0 {
		return "", fmt.Errorf("patch required")
	}
	patch, err := patchFromRow(patchRows[0])
	if err != nil {
		return "", err
	}
	verRows, err := query("SELECT verification_digest,contract_digest,material_digest,policy_version,outcome FROM verification_runs WHERE transaction_id=? ORDER BY created_at DESC LIMIT 1", id)
	if err != nil || len(verRows) == 0 {
		return "", fmt.Errorf("verification required")
	}
	v := verRows[0]
	effectRows, err := query(`SELECT e.effect_id,e.tool_identity,e.adapter_identity,e.input_digest,e.idempotency_key,e.status,e.reversibility,e.commit_rank,d.credential_id,d.operation,d.destination,d.prepared_digest,d.preview_digest,d.resource_versions_json,d.support_level FROM effects e JOIN effect_documents d ON d.effect_id=e.effect_id WHERE e.transaction_id=? AND e.status<>'superseded' ORDER BY e.commit_rank,e.effect_id`, id)
	if err != nil {
		return "", err
	}
	effects := make([]map[string]any, 0, len(effectRows))
	for _, row := range effectRows {
		status := domain.EffectState(String(row, "status"))
		if status != domain.EffectVerified && status != domain.EffectCommitted {
			return "", fmt.Errorf("effect %s is not approval-ready: %s", String(row, "effect_id"), status)
		}
		var versions map[string]string
		if err := json.Unmarshal([]byte(String(row, "resource_versions_json")), &versions); err != nil {
			return "", err
		}
		effects = append(effects, map[string]any{
			"effect_id":         String(row, "effect_id"),
			"tool_identity":     String(row, "tool_identity"),
			"adapter_identity":  String(row, "adapter_identity"),
			"credential_id":     String(row, "credential_id"),
			"operation":         String(row, "operation"),
			"destination":       String(row, "destination"),
			"input_digest":      String(row, "input_digest"),
			"prepared_digest":   String(row, "prepared_digest"),
			"preview_digest":    String(row, "preview_digest"),
			"resource_versions": versions,
			"idempotency_key":   String(row, "idempotency_key"),
			"status":            string(status),
			"reversibility":     String(row, "reversibility"),
			"commit_rank":       Int64(row, "commit_rank"),
			"support_level":     String(row, "support_level"),
		})
	}
	material := map[string]any{"format_version": "0.2", "transaction_id": id, "material_revision": cur.MaterialRevision, "base_revision": cur.BaseRevision, "patch_sha256": patch.PatchSHA256, "staged_tree_oid": patch.StagedTreeOID, "approval_material_digest": patch.ApprovalMaterialDigest, "verification_digest": String(v, "verification_digest"), "contract_digest": String(v, "contract_digest"), "material_digest": String(v, "material_digest"), "policy_version": String(v, "policy_version"), "outcome": String(v, "outcome"), "external_effects": effects}
	return domain.Digest(material)
}

func (r *Repository) BeginCommit(id, digest string) (domain.Transaction, error) {
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
		if cur.Status != domain.StateReady {
			return fmt.Errorf("expected ready, found %s", cur.Status)
		}
		if cur.ApprovalDigest == "" || cur.ApprovalDigest != digest {
			return errors.New("valid approval required")
		}
		pending, err := tx.Query(`SELECT effect_id,status FROM effects WHERE transaction_id=? AND status<>'superseded' AND status NOT IN ('verified','committed')`, id)
		if err != nil {
			return err
		}
		if len(pending) > 0 {
			return fmt.Errorf("effect %s is not commit-ready: %s", String(pending[0], "effect_id"), String(pending[0], "status"))
		}
		changes, err := tx.Exec(`UPDATE transactions SET status='committing',revision=revision+1,updated_at=? WHERE transaction_id=? AND revision=?`, ts(now), id, cur.Revision)
		if err != nil {
			return err
		}
		if changes != 1 {
			return errors.New("concurrent transaction update")
		}
		return appendEvent(tx, id, "transaction.committing", map[string]any{"approval_digest": digest}, now)
	})
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Get(id)
}

func (r *Repository) FinishCommit(id string, ref domain.MaterializedRef) (domain.Transaction, error) {
	if err := r.RecordMaterializedRef(id, ref); err != nil {
		return domain.Transaction{}, err
	}
	return r.FinalizeTransactionCommit(id)
}

func (r *Repository) MarkNeedsReconciliation(id, reason string) (domain.Transaction, error) {
	cur, err := r.Get(id)
	if err != nil {
		return domain.Transaction{}, err
	}
	return r.Transition(id, cur.Status, domain.StateNeedsReconciliation, "daemon", reason, false, false)
}

func (r *Repository) Events(id string) ([]Row, error) {
	return r.db.Query("SELECT sequence,event_id,event_type,payload_json,payload_digest,previous_event_hash,event_hash,created_at FROM events WHERE transaction_id=? ORDER BY sequence", id)
}

func appendEvent(tx *Tx, id, kind string, payload any, at time.Time) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return appendChainedEvent(tx, id, "", kind, b, domain.SHA256Bytes(b), 0, ts(at))
}
func verificationOutcome(results []domain.VerificationCheckResult) string {
	required := 0
	all := true
	for _, r := range results {
		if !r.Required {
			continue
		}
		required++
		if r.Status == "error" || r.Status == "cancelled" {
			return "error"
		}
		if r.Status != "pass" {
			all = false
		}
	}
	if required == 0 {
		return "error"
	}
	if all {
		return "pass"
	}
	return "fail"
}
func transactionFromRow(row Row) (domain.Transaction, error) {
	created, err := parseTime(String(row, "created_at"))
	if err != nil {
		return domain.Transaction{}, err
	}
	updated, err := parseTime(String(row, "updated_at"))
	if err != nil {
		return domain.Transaction{}, err
	}
	var sealed *time.Time
	if s := String(row, "sealed_at"); s != "" {
		v, e := parseTime(s)
		if e != nil {
			return domain.Transaction{}, e
		}
		sealed = &v
	}
	return domain.Transaction{ID: String(row, "transaction_id"), OwnerPrincipalID: String(row, "owner_principal_id"), ProtocolVersion: String(row, "protocol_version"), Mode: String(row, "mode"), AgentAdapter: String(row, "agent_adapter"), AgentSessionID: String(row, "agent_session_id"), WorkspaceIdentity: String(row, "workspace_identity"), BaseRevision: String(row, "base_revision"), Status: domain.TransactionState(String(row, "status")), PolicyVersion: String(row, "policy_version"), Revision: Int64(row, "revision"), MaterialRevision: Int64(row, "material_revision"), ApprovalDigest: String(row, "approval_digest"), CreatedAt: created, SealedAt: sealed, UpdatedAt: updated}, nil
}
func workspaceFromRow(row Row) (domain.Workspace, error) {
	created, err := parseTime(String(row, "created_at"))
	if err != nil {
		return domain.Workspace{}, err
	}
	return domain.Workspace{TransactionID: String(row, "transaction_id"), RepositoryRoot: String(row, "repository_root"), GitCommonDir: String(row, "git_common_dir"), SourceHeadRef: String(row, "source_head_ref"), BaseOID: String(row, "base_oid"), ObjectFormat: String(row, "object_format"), WorkspacePath: String(row, "workspace_path"), ArtifactsPath: String(row, "artifacts_path"), DirtyPolicy: String(row, "dirty_policy"), SourceStatusDigest: String(row, "source_status_digest"), CreatedAt: created}, nil
}
func patchFromRow(row Row) (domain.Patch, error) {
	generated, err := parseTime(String(row, "generated_at"))
	if err != nil {
		return domain.Patch{}, err
	}
	var paths []string
	if err := json.Unmarshal([]byte(String(row, "changed_paths_json")), &paths); err != nil {
		return domain.Patch{}, err
	}
	return domain.Patch{TransactionID: String(row, "transaction_id"), PatchPath: String(row, "patch_path"), PatchSHA256: String(row, "patch_sha256"), PatchSizeBytes: Int64(row, "patch_size_bytes"), StagedTreeOID: String(row, "staged_tree_oid"), ChangedPaths: paths, ApprovalMaterialDigest: String(row, "approval_material_digest"), GeneratedAt: generated}, nil
}
func ts(t time.Time) string                 { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func (r *Repository) RecordRuntimeExecution(record domain.RuntimeExecution) error {
	if record.ExecutionID == "" || record.TransactionID == "" || record.EvidencePath == "" {
		return errors.New("execution_id, transaction_id, and evidence_path are required")
	}
	return r.db.WithTx(func(tx *Tx) error {
		if _, err := tx.QueryOne("SELECT transaction_id FROM transactions WHERE transaction_id=?", record.TransactionID); err != nil {
			return err
		}
		synchronized := int64(0)
		if record.WorkspaceSynchronized {
			synchronized = 1
		}
		_, err := tx.Exec(`INSERT INTO runtime_executions(execution_id,transaction_id,purpose,command_digest,environment_digest,policy_digest,image,image_digest,runtime_kind,runtime_version,exit_code,termination_reason,stdout_path,stderr_path,evidence_path,workspace_synchronized,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, record.ExecutionID, record.TransactionID, record.Purpose, record.CommandDigest, record.EnvironmentDigest, record.PolicyDigest, record.Image, record.ImageDigest, record.RuntimeKind, record.RuntimeVersion, int64(record.ExitCode), record.TerminationReason, nullString(record.StdoutPath), nullString(record.StderrPath), record.EvidencePath, synchronized, ts(record.StartedAt), ts(record.FinishedAt))
		if err != nil {
			return err
		}
		return appendEvent(tx, record.TransactionID, "runtime.execution", record, record.FinishedAt)
	})
}

func (r *Repository) RuntimeExecutions(transactionID string) ([]domain.RuntimeExecution, error) {
	rows, err := r.db.Query("SELECT * FROM runtime_executions WHERE transaction_id=? ORDER BY started_at,execution_id", transactionID)
	if err != nil {
		return nil, err
	}
	result := make([]domain.RuntimeExecution, 0, len(rows))
	for _, row := range rows {
		started, err := parseTime(String(row, "started_at"))
		if err != nil {
			return nil, err
		}
		finished, err := parseTime(String(row, "finished_at"))
		if err != nil {
			return nil, err
		}
		result = append(result, domain.RuntimeExecution{ExecutionID: String(row, "execution_id"), TransactionID: String(row, "transaction_id"), Purpose: String(row, "purpose"), CommandDigest: String(row, "command_digest"), EnvironmentDigest: String(row, "environment_digest"), PolicyDigest: String(row, "policy_digest"), Image: String(row, "image"), ImageDigest: String(row, "image_digest"), RuntimeKind: String(row, "runtime_kind"), RuntimeVersion: String(row, "runtime_version"), ExitCode: int(Int64(row, "exit_code")), TerminationReason: String(row, "termination_reason"), StdoutPath: String(row, "stdout_path"), StderrPath: String(row, "stderr_path"), EvidencePath: String(row, "evidence_path"), WorkspaceSynchronized: Int64(row, "workspace_synchronized") == 1, StartedAt: started, FinishedAt: finished})
	}
	return result, nil
}
