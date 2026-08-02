package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/adapters/githubbranch"
	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/evidencecrypto"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/quota"
	"github.com/SHOnnay/futurediff/internal/repoadmission"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type Service struct {
	Ledger                 *ledger.Repository
	Staging                staging.Manager
	Verifier               verification.Engine
	OCI                    *runtimeoci.Runner
	Credentials            *credentials.Broker
	GitHub                 *githubdraft.Adapter
	GitHubBranch           *githubbranch.Adapter
	Slack                  *slackoutbox.Adapter
	CoordinatorID          string
	ApprovalKeys           *operatorapproval.Keyring
	ApprovalQuorum         *operatorapproval.QuorumPolicy
	RequireSignedApprovals bool
	EvidenceCipher         evidencecrypto.FileCipher
	Quotas                 quota.Policy
	RepositoryPolicy       *repoadmission.Policy
}

type CreateRequest struct {
	Repository     string `json:"repository"`
	Mode           string `json:"mode"`
	AgentAdapter   string `json:"agent_adapter,omitempty"`
	AgentSessionID string `json:"agent_session_id,omitempty"`
	PolicyVersion  string `json:"policy_version"`
	DirtyPolicy    string `json:"dirty_policy,omitempty"`
}

type TransactionView struct {
	Transaction domain.Transaction      `json:"transaction"`
	Workspace   domain.Workspace        `json:"workspace"`
	Patch       *domain.Patch           `json:"patch,omitempty"`
	Effects     []domain.ExternalEffect `json:"effects,omitempty"`
	Receipts    []domain.EffectReceipt  `json:"receipts,omitempty"`
}

func (s *Service) Create(req CreateRequest) (TransactionView, error) {
	return s.CreateForPrincipal(req, "local:operator")
}

func (s *Service) CreateForPrincipal(req CreateRequest, principalID string) (TransactionView, error) {
	if req.Repository == "" {
		return TransactionView{}, errors.New("repository is required")
	}
	policyLimits := s.quotaPolicy()
	openTransactions, err := s.Ledger.CountOpenTransactions()
	if err != nil {
		return TransactionView{}, err
	}
	if openTransactions >= policyLimits.MaxOpenTransactions {
		return TransactionView{}, fmt.Errorf("open transaction quota exceeded: %d/%d", openTransactions, policyLimits.MaxOpenTransactions)
	}
	if req.Mode == "" {
		req.Mode = "cooperative"
	}
	if req.Mode == "enforced" {
		if s.OCI == nil {
			return TransactionView{}, errors.New("enforced mode requires configured OCI runtime")
		}
		if _, err := s.OCI.Ready(context.Background()); err != nil {
			return TransactionView{}, fmt.Errorf("enforced runtime is not ready: %w", err)
		}
	}
	if req.PolicyVersion == "" {
		req.PolicyVersion = "policy-0.1"
	}
	policy := staging.Reject
	if req.DirtyPolicy == string(staging.StageFromHead) {
		policy = staging.StageFromHead
	}
	inspect, err := s.Staging.Inspect(req.Repository, policy)
	if err != nil {
		return TransactionView{}, err
	}
	repositoryPolicy := s.RepositoryPolicy
	if repositoryPolicy == nil {
		stableDefault, err := repoadmission.StableDefaultForPath(inspect.RepositoryRoot)
		if err != nil {
			return TransactionView{}, fmt.Errorf("configure stable repository admission: %w", err)
		}
		repositoryPolicy = &stableDefault
	}
	decision := repositoryPolicy.Evaluate(inspect, policy)
	if !decision.Allowed {
		reasons := make([]string, 0, len(decision.Reasons))
		for i, reason := range decision.Reasons {
			if i < len(decision.ReasonCodes) {
				reasons = append(reasons, decision.ReasonCodes[i]+": "+reason)
			} else {
				reasons = append(reasons, reason)
			}
		}
		return TransactionView{}, fmt.Errorf("repository admission rejected by %s: %s", decision.PolicyID, strings.Join(reasons, "; "))
	}
	id := domain.NewID("tx")
	workspace, err := s.Staging.Create(id, inspect, policy)
	if err != nil {
		return TransactionView{}, err
	}
	if principalID == "" {
		return TransactionView{}, errors.New("transaction owner principal is required")
	}
	tx := domain.Transaction{ID: id, OwnerPrincipalID: principalID, Mode: req.Mode, AgentAdapter: req.AgentAdapter, AgentSessionID: req.AgentSessionID, PolicyVersion: req.PolicyVersion, CreatedAt: time.Now().UTC()}
	created, err := s.Ledger.Create(ledger.CreateInput{Transaction: tx, Workspace: workspace})
	if err != nil {
		_ = s.Staging.Abort(workspace)
		return TransactionView{}, err
	}
	return TransactionView{Transaction: created, Workspace: workspace}, nil
}

type ExecuteRequest struct {
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment,omitempty"`
}

type ExecuteView struct {
	Execution domain.RuntimeExecution `json:"execution"`
	Stdout    string                  `json:"stdout,omitempty"`
	Stderr    string                  `json:"stderr,omitempty"`
}

func (s *Service) quotaPolicy() quota.Policy {
	policy := s.Quotas
	if policy.Version == "" {
		policy = quota.Default()
	}
	return policy
}

func (s *Service) QuotaStatus() map[string]any {
	policy := s.quotaPolicy()
	return map[string]any{"configured": s.Quotas.Version != "", "policy": policy}
}

func (s *Service) SecretScanStatus() map[string]any {
	if s.Verifier.SecretScanner == nil {
		return map[string]any{"configured": false}
	}
	return map[string]any{"configured": true, "policy_version": s.Verifier.SecretScanner.Policy.Version, "enabled": s.Verifier.SecretScanner.Policy.Enabled, "block_severities": s.Verifier.SecretScanner.Policy.BlockSeverities}
}

func (s *Service) CredentialStatus() map[string]any {
	if s.Credentials == nil {
		return map[string]any{"configured": false, "secret_values_persisted": false}
	}
	return s.Credentials.Status()
}

func (s *Service) ApprovalStatus() map[string]any {
	status := map[string]any{"configured": s.ApprovalKeys != nil, "signed_required": s.RequireSignedApprovals}
	if s.ApprovalQuorum != nil {
		status["quorum_threshold"] = s.ApprovalQuorum.Threshold
		status["quorum_required_approvers"] = s.ApprovalQuorum.RequiredApprovers
	}
	return status
}

func (s *Service) RuntimeStatus(ctx context.Context) map[string]any {
	status := map[string]any{"configured": s.OCI != nil, "enforced_ready": false}
	if s.OCI == nil {
		return status
	}
	backend, err := s.OCI.Ready(ctx)
	if err != nil {
		status["error"] = err.Error()
		return status
	}
	status["enforced_ready"] = true
	status["runtime"] = backend
	status["image"] = s.OCI.Policy.Image
	return status
}

func (s *Service) Execute(ctx context.Context, id string, req ExecuteRequest) (ExecuteView, error) {
	if len(req.Command) == 0 {
		return ExecuteView{}, errors.New("command is required")
	}
	tx, err := s.Ledger.Get(id)
	if err != nil {
		return ExecuteView{}, err
	}
	if tx.Status != domain.StateActive {
		return ExecuteView{}, fmt.Errorf("transaction is %s, expected active", tx.Status)
	}
	if tx.Mode != "enforced" {
		return ExecuteView{}, errors.New("daemon command execution is available only for enforced transactions")
	}
	if s.OCI == nil {
		return ExecuteView{}, errors.New("OCI runtime is not configured")
	}
	workspace, err := s.Ledger.Workspace(id)
	if err != nil {
		return ExecuteView{}, err
	}
	executions, err := s.Ledger.CountRuntimeExecutions(id)
	if err != nil {
		return ExecuteView{}, err
	}
	if executions >= s.quotaPolicy().MaxExecutionsPerTransaction {
		return ExecuteView{}, fmt.Errorf("runtime execution quota exceeded: %d/%d", executions, s.quotaPolicy().MaxExecutionsPerTransaction)
	}
	executionID := domain.NewID("exec")
	result, runErr := s.OCI.Execute(ctx, runtimeoci.Request{
		TransactionID: id,
		ExecutionID:   executionID,
		Workspace:     workspace.WorkspacePath,
		Command:       req.Command,
		Environment:   req.Environment,
		Purpose:       runtimeoci.Mutation,
		SyncWorkspace: true,
	})
	if result.Evidence.ExecutionID == "" {
		return ExecuteView{}, runErr
	}
	record, persistErr := persistRuntimeEvidence(workspace.ArtifactsPath, result, s.EvidenceCipher)
	if persistErr != nil {
		return ExecuteView{}, persistErr
	}
	if err := s.Ledger.RecordRuntimeExecution(record); err != nil {
		return ExecuteView{}, err
	}
	view := ExecuteView{Execution: record, Stdout: string(result.Stdout), Stderr: string(result.Stderr)}
	if runErr != nil {
		return view, runErr
	}
	return view, nil
}

func persistRuntimeEvidence(artifactsPath string, result runtimeoci.Result, evidenceCipher evidencecrypto.FileCipher) (domain.RuntimeExecution, error) {
	dir := filepath.Join(artifactsPath, "executions", result.Evidence.ExecutionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return domain.RuntimeExecution{}, err
	}
	suffix := ""
	if evidenceCipher != nil {
		suffix = ".fde"
	}
	stdoutPath := filepath.Join(dir, "stdout.log"+suffix)
	stderrPath := filepath.Join(dir, "stderr.log"+suffix)
	evidencePath := filepath.Join(dir, "evidence.json"+suffix)
	encoded, err := json.MarshalIndent(result.Evidence, "", "  ")
	if err != nil {
		return domain.RuntimeExecution{}, err
	}
	if evidenceCipher != nil {
		baseAAD := result.Evidence.TransactionID + ":" + result.Evidence.ExecutionID + ":"
		if err := evidenceCipher.WriteFile(stdoutPath, result.Stdout, []byte(baseAAD+"stdout")); err != nil {
			return domain.RuntimeExecution{}, err
		}
		if err := evidenceCipher.WriteFile(stderrPath, result.Stderr, []byte(baseAAD+"stderr")); err != nil {
			return domain.RuntimeExecution{}, err
		}
		if err := evidenceCipher.WriteFile(evidencePath, append(encoded, '\n'), []byte(baseAAD+"evidence")); err != nil {
			return domain.RuntimeExecution{}, err
		}
	} else {
		if err := os.WriteFile(stdoutPath, result.Stdout, 0o600); err != nil {
			return domain.RuntimeExecution{}, err
		}
		if err := os.WriteFile(stderrPath, result.Stderr, 0o600); err != nil {
			return domain.RuntimeExecution{}, err
		}
		if err := os.WriteFile(evidencePath, append(encoded, '\n'), 0o600); err != nil {
			return domain.RuntimeExecution{}, err
		}
	}
	e := result.Evidence
	return domain.RuntimeExecution{
		ExecutionID:           e.ExecutionID,
		TransactionID:         e.TransactionID,
		Purpose:               string(runtimeoci.Mutation),
		CommandDigest:         e.CommandDigest,
		EnvironmentDigest:     e.EnvironmentDigest,
		PolicyDigest:          e.PolicyDigest,
		Image:                 e.Image,
		ImageDigest:           e.ImageDigest,
		RuntimeKind:           string(e.Runtime.Kind),
		RuntimeVersion:        e.Runtime.Version,
		ExitCode:              e.ExitCode,
		TerminationReason:     string(e.TerminationReason),
		StdoutPath:            stdoutPath,
		StderrPath:            stderrPath,
		EvidencePath:          evidencePath,
		WorkspaceSynchronized: e.WorkspaceSynchronized,
		StartedAt:             e.StartedAt,
		FinishedAt:            e.FinishedAt,
	}, nil
}

func (s *Service) Get(id string) (TransactionView, error) {
	tx, err := s.Ledger.Get(id)
	if err != nil {
		return TransactionView{}, err
	}
	ws, err := s.Ledger.Workspace(id)
	if err != nil {
		return TransactionView{}, err
	}
	view := TransactionView{Transaction: tx, Workspace: ws}
	if p, err := s.Ledger.Patch(id); err == nil {
		view.Patch = &p
	}
	if effects, err := s.Ledger.ExternalEffects(id); err == nil {
		view.Effects = effects
		for _, effect := range effects {
			if receipt, receiptErr := s.Ledger.EffectReceipt(effect.EffectID); receiptErr == nil {
				view.Receipts = append(view.Receipts, receipt)
			}
		}
	}
	return view, nil
}
func (s *Service) Seal(id string) (TransactionView, error) {
	ws, err := s.Ledger.Workspace(id)
	if err != nil {
		return TransactionView{}, err
	}
	patch, err := s.Staging.Capture(ws)
	if err != nil {
		return TransactionView{}, err
	}
	limits := s.quotaPolicy()
	if patch.PatchSizeBytes > limits.MaxPatchBytes || int64(len(patch.ChangedPaths)) > limits.MaxChangedPaths {
		_ = os.Remove(patch.PatchPath)
		return TransactionView{}, fmt.Errorf("staged patch exceeds quota: bytes=%d/%d changed_paths=%d/%d", patch.PatchSizeBytes, limits.MaxPatchBytes, len(patch.ChangedPaths), limits.MaxChangedPaths)
	}
	if _, err := s.Ledger.RecordPatch(id, patch); err != nil {
		return TransactionView{}, err
	}
	return s.Get(id)
}
func (s *Service) Verify(id string, contract verification.Contract) (TransactionView, error) {
	if int64(len(contract.Checks)) > s.quotaPolicy().MaxVerificationChecks {
		return TransactionView{}, fmt.Errorf("verification check quota exceeded: %d/%d", len(contract.Checks), s.quotaPolicy().MaxVerificationChecks)
	}
	tx, err := s.Ledger.Get(id)
	if err != nil {
		return TransactionView{}, err
	}
	if tx.Status != domain.StateSealed && tx.Status != domain.StateStale {
		return TransactionView{}, fmt.Errorf("transaction is %s, expected sealed or stale", tx.Status)
	}
	ws, err := s.Ledger.Workspace(id)
	if err != nil {
		return TransactionView{}, err
	}
	patch, err := s.Ledger.Patch(id)
	if err != nil {
		return TransactionView{}, err
	}
	materialDigest, err := s.Ledger.VerificationMaterial(id)
	if err != nil {
		return TransactionView{}, err
	}
	report, err := s.Verifier.RunWithMaterial(id, ws, patch, contract, materialDigest)
	if err != nil {
		return TransactionView{}, err
	}
	if _, err := s.Ledger.RecordVerification(id, report); err != nil {
		return TransactionView{}, err
	}
	return s.Get(id)
}
func (s *Service) ApprovalMaterial(id string) (map[string]string, error) {
	digest, err := s.Ledger.ApprovalMaterial(id)
	if err != nil {
		return nil, err
	}
	return map[string]string{"transaction_id": id, "transaction_digest": digest}, nil
}
func (s *Service) Approve(id, digest, approver string) (TransactionView, error) {
	if s.RequireSignedApprovals || s.ApprovalQuorum != nil {
		return TransactionView{}, errors.New("signed approval envelope or quorum bundle required")
	}
	if approver == "" {
		approver = "local-user"
	}
	if _, err := s.Ledger.Approve(id, digest, approver); err != nil {
		return TransactionView{}, err
	}
	return s.Get(id)
}

func (s *Service) ApproveSigned(id string, env operatorapproval.Envelope) (TransactionView, error) {
	if s.ApprovalQuorum != nil {
		bundle, err := operatorapproval.NewBundle([]operatorapproval.Envelope{env})
		if err != nil {
			return TransactionView{}, err
		}
		return s.ApproveSignedQuorum(id, bundle)
	}
	if s.ApprovalKeys == nil {
		return TransactionView{}, errors.New("approval keyring is not configured")
	}
	expected, err := s.Ledger.ApprovalMaterial(id)
	if err != nil {
		return TransactionView{}, err
	}
	if err := operatorapproval.Verify(*s.ApprovalKeys, env, id, expected, time.Now()); err != nil {
		return TransactionView{}, err
	}
	ref := operatorapproval.SignatureReference(env)
	if _, err := s.Ledger.ApproveWithEvidence(id, expected, env.Approver, ref, &env.ExpiresAt); err != nil {
		return TransactionView{}, err
	}
	return s.Get(id)
}

func (s *Service) ApproveSignedQuorum(id string, bundle operatorapproval.Bundle) (TransactionView, error) {
	if s.ApprovalKeys == nil || s.ApprovalQuorum == nil {
		return TransactionView{}, errors.New("approval quorum is not configured")
	}
	expected, err := s.Ledger.ApprovalMaterial(id)
	if err != nil {
		return TransactionView{}, err
	}
	result, err := operatorapproval.VerifyQuorum(*s.ApprovalKeys, *s.ApprovalQuorum, bundle, id, expected, time.Now())
	if err != nil {
		return TransactionView{}, err
	}
	expires := bundle.Envelopes[0].ExpiresAt
	for _, env := range bundle.Envelopes[1:] {
		if env.ExpiresAt.Before(expires) {
			expires = env.ExpiresAt
		}
	}
	approver := "quorum:" + strings.Join(result.DistinctApprovers, ",")
	if _, err := s.Ledger.ApproveWithEvidence(id, expected, approver, operatorapproval.QuorumSignatureReference(result), &expires); err != nil {
		return TransactionView{}, err
	}
	return s.Get(id)
}
func (s *Service) Commit(id, digest string) (TransactionView, error) {
	return s.CommitContext(context.Background(), id, digest)
}

func (s *Service) CommitContext(ctx context.Context, id, digest string) (TransactionView, error) {
	tx, err := s.Ledger.Get(id)
	if err != nil {
		return TransactionView{}, err
	}
	if tx.Status != domain.StateReady {
		return TransactionView{}, fmt.Errorf("transaction is %s, expected ready", tx.Status)
	}
	ws, err := s.Ledger.Workspace(id)
	if err != nil {
		return TransactionView{}, err
	}
	pinned, current, err := s.Staging.SourcePinned(ws)
	if err != nil {
		return TransactionView{}, err
	}
	if !pinned {
		_, _ = s.Ledger.Transition(id, domain.StateReady, domain.StateStale, "git-staging", "source moved to "+current, false, true)
		return TransactionView{}, errors.New("transaction became stale because the source branch moved")
	}
	if err := s.preflightExternalEffects(ctx, id); err != nil {
		return TransactionView{}, err
	}
	fencingToken, err := s.acquireTransactionLease(id)
	if err != nil {
		return TransactionView{}, err
	}
	if _, err := s.Ledger.BeginCommit(id, digest); err != nil {
		return TransactionView{}, err
	}
	patch, err := s.Ledger.Patch(id)
	if err != nil {
		_, _ = s.Ledger.MarkNeedsReconciliation(id, "durable patch lookup failed after commit began")
		return TransactionView{}, err
	}
	ref, exists, err := s.Staging.InspectIntegrationRef(ws, patch)
	if err != nil {
		_, _ = s.Ledger.MarkNeedsReconciliation(id, err.Error())
		return TransactionView{}, err
	}
	if !exists {
		ref, err = s.Staging.Materialize(ws, patch, digest)
		if err != nil {
			_, _ = s.Ledger.MarkNeedsReconciliation(id, err.Error())
			return TransactionView{}, err
		}
	}
	if err := s.Ledger.RecordMaterializedRef(id, ref); err != nil {
		_, _ = s.Ledger.MarkNeedsReconciliation(id, "repository ref was published but its receipt could not be persisted")
		return TransactionView{}, err
	}
	if err := s.commitExternalEffects(ctx, id, fencingToken); err != nil {
		if current, getErr := s.Ledger.Get(id); getErr == nil && current.Status == domain.StateCommitting {
			_, _ = s.Ledger.MarkNeedsReconciliation(id, "external effect commit did not complete")
		}
		return TransactionView{}, err
	}
	if _, err := s.Ledger.FinalizeTransactionCommit(id); err != nil {
		_, _ = s.Ledger.MarkNeedsReconciliation(id, "commit receipts exist but transaction finalization failed")
		return TransactionView{}, err
	}
	return s.Get(id)
}

func (s *Service) Recover(id string) (TransactionView, error) {
	ctx := context.Background()
	tx, err := s.Ledger.Get(id)
	if err != nil {
		return TransactionView{}, err
	}
	if tx.Status != domain.StateCommitting && tx.Status != domain.StateNeedsReconciliation {
		return TransactionView{}, fmt.Errorf("transaction %s is not recoverable", tx.Status)
	}
	if tx.Status == domain.StateCommitting {
		if _, err := s.Ledger.MarkNeedsReconciliation(id, "recovery started"); err != nil {
			return TransactionView{}, err
		}
	}
	fencingToken, err := s.acquireTransactionLease(id)
	if err != nil {
		return TransactionView{}, err
	}
	ws, err := s.Ledger.Workspace(id)
	if err != nil {
		return TransactionView{}, err
	}
	patch, err := s.Ledger.Patch(id)
	if err != nil {
		return TransactionView{}, err
	}
	ref, exists, err := s.Staging.InspectIntegrationRef(ws, patch)
	if err != nil {
		return TransactionView{}, err
	}
	if exists {
		if err := s.Ledger.RecordMaterializedRef(id, ref); err != nil {
			return TransactionView{}, err
		}
	}
	allCommitted, anyCommitted, err := s.reconcileExternalEffects(ctx, id, fencingToken)
	if err != nil {
		return TransactionView{}, err
	}
	if exists && allCommitted {
		if _, err := s.Ledger.FinalizeTransactionCommit(id); err != nil {
			return TransactionView{}, err
		}
		return s.Get(id)
	}
	if exists && anyCommitted && !allCommitted {
		if err := s.commitExternalEffects(ctx, id, fencingToken); err != nil {
			return TransactionView{}, err
		}
		if _, err := s.Ledger.FinalizeTransactionCommit(id); err != nil {
			return TransactionView{}, err
		}
		return s.Get(id)
	}
	if anyCommitted && !exists {
		_, _ = s.Ledger.Transition(id, domain.StateNeedsReconciliation, domain.StateManualIntervention, "recovery", "external effect committed but repository ref is absent", false, false)
		return TransactionView{}, errors.New("manual intervention required: external effect committed but repository ref is absent")
	}
	pinned, current, err := s.Staging.SourcePinned(ws)
	if err != nil {
		return TransactionView{}, err
	}
	if pinned {
		if _, err := s.Ledger.Transition(id, domain.StateNeedsReconciliation, domain.StateReady, "recovery", "all unresolved effects proved absent or remain prepared", false, false); err != nil {
			return TransactionView{}, err
		}
	} else {
		if _, err := s.Ledger.Transition(id, domain.StateNeedsReconciliation, domain.StateStale, "recovery", "source moved to "+current, false, true); err != nil {
			return TransactionView{}, err
		}
	}
	return s.Get(id)
}

func (s *Service) Abort(id string) (TransactionView, error) {
	return s.AbortWithReason(id, "user", "abort requested")
}

func (s *Service) AbortWithReason(id, actor, reason string) (TransactionView, error) {
	if actor == "" {
		actor = "user"
	}
	if reason == "" {
		reason = "abort requested"
	}
	tx, err := s.Ledger.Get(id)
	if err != nil {
		return TransactionView{}, err
	}
	allowed := map[domain.TransactionState]bool{domain.StateCreated: true, domain.StateActive: true, domain.StateSealed: true, domain.StateFailedVerification: true, domain.StateReady: true, domain.StateStale: true}
	if !allowed[tx.Status] {
		return TransactionView{}, fmt.Errorf("cannot abort transaction in %s", tx.Status)
	}
	effects, err := s.Ledger.ExternalEffects(id)
	if err != nil {
		return TransactionView{}, err
	}
	for _, effect := range effects {
		if effect.Status == domain.EffectCommitted || effect.Status == domain.EffectCommitting || effect.Status == domain.EffectUnknown {
			return TransactionView{}, fmt.Errorf("cannot abort while external effect %s is %s", effect.EffectID, effect.Status)
		}
	}
	if _, err := s.Ledger.Transition(id, tx.Status, domain.StateAborting, actor, reason, false, true); err != nil {
		return TransactionView{}, err
	}
	if err := s.Ledger.AbortPreparedEffects(id, "transaction aborted before provider release"); err != nil {
		return TransactionView{}, err
	}
	ws, err := s.Ledger.Workspace(id)
	if err == nil {
		_ = s.Staging.Abort(ws)
	}
	if _, err := s.Ledger.Transition(id, domain.StateAborting, domain.StateAborted, "daemon", "workspace removed", false, true); err != nil {
		return TransactionView{}, err
	}
	return s.Get(id)
}
func (s *Service) Events(id string) ([]ledger.Row, error) { return s.Ledger.Events(id) }
func LoadContract(path string) (verification.Contract, error) {
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return verification.Contract{}, err
	}
	return verification.Parse(b)
}
func DecodeContract(raw json.RawMessage) (verification.Contract, error) {
	var c verification.Contract
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	return c, verification.Validate(c)
}
