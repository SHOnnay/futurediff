package mvpflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

type ApprovalSnapshot struct {
	SnapshotVersion           string    `json:"snapshot_version"`
	SnapshotID                string    `json:"snapshot_id"`
	TransactionID             string    `json:"transaction_id"`
	TransactionFingerprint    string    `json:"transaction_fingerprint"`
	GitHubPreparedFingerprint string    `json:"github_prepared_fingerprint"`
	SlackPreparedFingerprint  string    `json:"slack_prepared_fingerprint"`
	GitHubBaseSHA             string    `json:"github_base_sha"`
	CreatedAt                 time.Time `json:"created_at"`
	ApprovalMode              string    `json:"approval_mode"`
	ApprovalDecision          string    `json:"approval_decision"`
	PolicyBundleVersion       string    `json:"policy_bundle_version"`
	PolicyBundleHash          string    `json:"policy_bundle_hash"`
	VerificationBundleHash    string    `json:"verification_bundle_hash"`
	EffectSetHash             string    `json:"effect_set_hash"`
	CommitPlanHash            string    `json:"commit_plan_hash"`
	ResourceSetHash           string    `json:"resource_set_hash"`
}

type ApprovalValidation struct {
	Valid  bool
	Reason string
}

func CaptureApproval(prepared *Result, approvalMode, decision, policyBundleVersion, policyBundleHash, verificationBundleHash string) (*ApprovalSnapshot, error) {
	if prepared == nil || prepared.Transaction == nil || prepared.PostgresPreview == nil {
		return nil, fmt.Errorf("complete prepared result is required")
	}
	transactionFingerprint, err := transactionFingerprint(prepared)
	if err != nil {
		return nil, err
	}
	return &ApprovalSnapshot{
		SnapshotVersion:           "0.1",
		SnapshotID:                makeHash(time.Now().UTC().Format(time.RFC3339Nano), prepared.Transaction.ID, prepared.GitHubPrepared.Fingerprint),
		TransactionID:             prepared.Transaction.ID,
		TransactionFingerprint:    transactionFingerprint,
		GitHubPreparedFingerprint: prepared.GitHubPrepared.Fingerprint,
		SlackPreparedFingerprint:  prepared.SlackPrepared.Fingerprint,
		GitHubBaseSHA:             prepared.GitHubPrepared.Request.BaseSHA,
		CreatedAt:                 time.Now().UTC(),
		ApprovalMode:              approvalMode,
		ApprovalDecision:          decision,
		PolicyBundleVersion:       policyBundleVersion,
		PolicyBundleHash:          policyBundleHash,
		VerificationBundleHash:    verificationBundleHash,
		EffectSetHash:             makeHash(prepared.Transaction.Effects[1].PreparedFingerprint, prepared.GitHubPrepared.Fingerprint, prepared.SlackPrepared.Fingerprint),
		CommitPlanHash:            makeHash("repo", "github", "slack"),
		ResourceSetHash:           makeHash(prepared.GitHubPrepared.Request.Base, prepared.GitHubPrepared.Request.Head, prepared.SlackPrepared.Request.Channel),
	}, nil
}

func ValidateApproval(snapshot *ApprovalSnapshot, prepared *Result) (*ApprovalValidation, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("approval snapshot is required")
	}
	if prepared == nil || prepared.Transaction == nil || prepared.PostgresPreview == nil {
		return nil, fmt.Errorf("complete prepared result is required")
	}
	transactionFingerprint, err := transactionFingerprint(prepared)
	if err != nil {
		return nil, err
	}
	checks := []struct {
		valid  bool
		reason string
	}{
		{snapshot.ApprovalDecision == "approved", "approval decision is not approved"},
		{snapshot.TransactionID == prepared.Transaction.ID, "transaction id changed"},
		{snapshot.TransactionFingerprint == transactionFingerprint, "transaction fingerprint changed"},
		{snapshot.GitHubPreparedFingerprint == prepared.GitHubPrepared.Fingerprint, "github prepared fingerprint changed"},
		{snapshot.SlackPreparedFingerprint == prepared.SlackPrepared.Fingerprint, "slack prepared fingerprint changed"},
		{snapshot.GitHubBaseSHA == prepared.GitHubPrepared.Request.BaseSHA, "github base sha changed"},
	}
	for _, check := range checks {
		if !check.valid {
			return &ApprovalValidation{Valid: false, Reason: check.reason}, nil
		}
	}
	return &ApprovalValidation{Valid: true}, nil
}

func transactionFingerprint(prepared *Result) (string, error) {
	schemaDiffBytes, err := os.ReadFile(prepared.PostgresPreview.SchemaDiffPath)
	if err != nil {
		return "", fmt.Errorf("read postgres schema diff: %w", err)
	}
	return makeHash(prepared.StagedPatch, string(schemaDiffBytes), prepared.GitHubPrepared.Fingerprint, prepared.SlackPrepared.Fingerprint), nil
}

func makeHash(parts ...string) string {
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:])
}
