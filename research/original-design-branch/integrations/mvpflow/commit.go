package mvpflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
	"github.com/futurediff/futurediff/adapters/slack/outbox"
	"github.com/futurediff/futurediff/control-plane/gateway"
)

type CommitResult struct {
	Transaction        *gateway.TransactionRecord
	GitHubReceipt      *prcreate.Receipt
	SlackReceipt       *outbox.Receipt
	CompensationState  string
	GitHubCompensation *prcreate.CompensationReceipt
}

type CompensationError struct {
	Reason string
}

func (e *CompensationError) Error() string {
	return e.Reason
}

type CommitRecord struct {
	TransactionID      string                        `json:"transaction_id"`
	State              string                        `json:"state"`
	RepoState          string                        `json:"repo_state"`
	GitHubState        string                        `json:"github_state"`
	SlackState         string                        `json:"slack_state"`
	CompensationState  string                        `json:"compensation_state,omitempty"`
	CompensationReason string                        `json:"compensation_reason,omitempty"`
	GitHubReceipt      *prcreate.Receipt             `json:"github_receipt,omitempty"`
	SlackReceipt       *outbox.Receipt               `json:"slack_receipt,omitempty"`
	GitHubCompensation *prcreate.CompensationReceipt `json:"github_compensation,omitempty"`
	UpdatedAt          time.Time                     `json:"updated_at"`
}

func (s Service) Commit(ctx context.Context, repoPath string, prepared *Result) (*CommitResult, error) {
	return s.startCommit(ctx, repoPath, prepared, false)
}

func (s Service) CommitWithCompensation(ctx context.Context, repoPath string, prepared *Result) (*CommitResult, error) {
	return s.startCommit(ctx, repoPath, prepared, true)
}

func (s Service) startCommit(ctx context.Context, repoPath string, prepared *Result, allowCompensation bool) (*CommitResult, error) {
	if prepared == nil || prepared.Transaction == nil {
		return nil, fmt.Errorf("prepared result is required")
	}
	record := &CommitRecord{
		TransactionID: prepared.Transaction.ID,
		State:         "COMMITTING",
		RepoState:     "PENDING",
		GitHubState:   "PENDING",
		SlackState:    "PENDING",
		UpdatedAt:     time.Now().UTC(),
	}
	if err := saveCommitRecord(commitRecordPath(prepared), record); err != nil {
		return nil, err
	}
	return s.finishCommit(ctx, repoPath, prepared, record, false, true, allowCompensation)
}

func (s Service) CommitApproved(ctx context.Context, repoPath string, prepared *Result, approval *ApprovalSnapshot) (*CommitResult, error) {
	validation, err := ValidateApproval(approval, prepared)
	if err != nil {
		return nil, err
	}
	if !validation.Valid {
		return nil, fmt.Errorf("approval invalidated: %s", validation.Reason)
	}
	return s.Commit(ctx, repoPath, prepared)
}

func (s Service) ReconcileCommit(ctx context.Context, repoPath string, prepared *Result) (*CommitResult, error) {
	if prepared == nil || prepared.Transaction == nil {
		return nil, fmt.Errorf("prepared result is required")
	}
	record, err := loadCommitRecord(commitRecordPath(prepared))
	if err != nil {
		return nil, err
	}
	if record.State == "COMPENSATED" {
		transaction, _, err := s.Gateway.Inspect(repoPath, prepared.Transaction.ID)
		if err != nil {
			return nil, err
		}
		return commitResultFromRecord(transaction, record), nil
	}
	return s.finishCommit(ctx, repoPath, prepared, record, true, false, false)
}

func (s Service) finishCommit(ctx context.Context, repoPath string, prepared *Result, record *CommitRecord, recoverFirst bool, fireHooks bool, allowCompensation bool) (*CommitResult, error) {
	if prepared == nil || prepared.Transaction == nil {
		return nil, fmt.Errorf("prepared result is required")
	}
	freshness, err := s.GitHubClient.CheckBaseFreshness(ctx, prepared.GitHubPrepared)
	if err != nil {
		return nil, err
	}
	if !freshness.Fresh {
		return nil, fmt.Errorf("github base branch drift detected: expected %s, current %s", prepared.GitHubPrepared.Request.BaseSHA, freshness.CurrentSHA)
	}

	transaction, err := s.ensureRepoCommitted(ctx, repoPath, prepared, record)
	if err != nil {
		return nil, err
	}
	if err := saveCommitRecord(commitRecordPath(prepared), record); err != nil {
		return nil, err
	}

	githubReceipt, githubState, err := s.ensureGitHubReceipt(ctx, prepared, record, recoverFirst)
	if err != nil {
		return nil, fmt.Errorf("commit github effect: %w", err)
	}
	record.GitHubReceipt = githubReceipt
	record.GitHubState = githubState
	record.UpdatedAt = time.Now().UTC()
	if err := saveCommitRecord(commitRecordPath(prepared), record); err != nil {
		return nil, err
	}
	if fireHooks && s.AfterGitHubReceipt != nil {
		if err := s.AfterGitHubReceipt(record); err != nil {
			return nil, err
		}
	}

	slackReceipt, slackState, err := s.ensureSlackReceipt(ctx, prepared, record, recoverFirst)
	if err != nil {
		if allowCompensation && githubReceipt != nil {
			return s.compensateAfterSlackFailure(ctx, transaction, prepared, record, githubReceipt, err)
		}
		return nil, fmt.Errorf("commit slack effect: %w", err)
	}
	record.SlackReceipt = slackReceipt
	record.SlackState = slackState
	record.State = "COMMITTED"
	record.UpdatedAt = time.Now().UTC()
	if err := saveCommitRecord(commitRecordPath(prepared), record); err != nil {
		return nil, err
	}

	return &CommitResult{
		Transaction:        transaction,
		GitHubReceipt:      githubReceipt,
		SlackReceipt:       slackReceipt,
		CompensationState:  record.CompensationState,
		GitHubCompensation: record.GitHubCompensation,
	}, nil
}

func (s Service) compensateAfterSlackFailure(ctx context.Context, transaction *gateway.TransactionRecord, prepared *Result, record *CommitRecord, githubReceipt *prcreate.Receipt, slackErr error) (*CommitResult, error) {
	record.SlackState = "FAILED"
	record.CompensationReason = slackErr.Error()
	compensation, err := s.GitHubClient.Close(ctx, prepared.GitHubPrepared, githubReceipt)
	if err != nil {
		record.State = "FAILED_MANUAL_INTERVENTION"
		record.CompensationState = "FAILED"
		record.UpdatedAt = time.Now().UTC()
		if saveErr := saveCommitRecord(commitRecordPath(prepared), record); saveErr != nil {
			return nil, saveErr
		}
		return nil, fmt.Errorf("commit slack effect: %w; compensation failed: %v", slackErr, err)
	}
	record.State = "COMPENSATED"
	record.CompensationState = "COMPENSATED"
	record.GitHubCompensation = compensation
	record.UpdatedAt = time.Now().UTC()
	if err := saveCommitRecord(commitRecordPath(prepared), record); err != nil {
		return nil, err
	}
	return &CommitResult{
		Transaction:        transaction,
		GitHubReceipt:      githubReceipt,
		CompensationState:  record.CompensationState,
		GitHubCompensation: compensation,
	}, &CompensationError{Reason: fmt.Sprintf("commit slack effect failed and github pull request %d was compensated", githubReceipt.PullNumber)}
}

func (s Service) ensureRepoCommitted(ctx context.Context, repoPath string, prepared *Result, record *CommitRecord) (*gateway.TransactionRecord, error) {
	if record.RepoState == "COMMITTED" {
		transaction, _, err := s.Gateway.Inspect(repoPath, prepared.Transaction.ID)
		return transaction, err
	}
	transaction, _, err := s.Gateway.Inspect(repoPath, prepared.Transaction.ID)
	if err != nil {
		return nil, err
	}
	switch transaction.State {
	case "AWAITING_APPROVAL":
		transaction, err = s.Gateway.Commit(ctx, repoPath, prepared.Transaction.ID)
	case "COMMITTING":
		transaction, err = s.Gateway.Recover(repoPath, prepared.Transaction.ID)
	case "COMMITTED":
		// already committed; keep current transaction
	default:
		return nil, fmt.Errorf("transaction %s is not recoverable for commit orchestration: %s", prepared.Transaction.ID, transaction.State)
	}
	if err != nil {
		return nil, fmt.Errorf("commit repo transaction: %w", err)
	}
	record.RepoState = "COMMITTED"
	record.UpdatedAt = time.Now().UTC()
	return transaction, nil
}

func (s Service) ensureGitHubReceipt(ctx context.Context, prepared *Result, record *CommitRecord, recoverFirst bool) (*prcreate.Receipt, string, error) {
	if record.GitHubReceipt != nil {
		return record.GitHubReceipt, record.GitHubState, nil
	}
	if recoverFirst {
		receipt, err := s.GitHubClient.Recover(ctx, prepared.GitHubPrepared)
		if err == nil {
			return receipt, "RECOVERED", nil
		}
	}
	receipt, err := s.GitHubClient.Create(ctx, prepared.GitHubPrepared)
	if err == nil {
		return receipt, "COMMITTED", nil
	}
	receipt, recoverErr := s.GitHubClient.Recover(ctx, prepared.GitHubPrepared)
	if recoverErr != nil {
		return nil, "PENDING", recoverErr
	}
	return receipt, "RECOVERED", nil
}

func (s Service) ensureSlackReceipt(ctx context.Context, prepared *Result, record *CommitRecord, recoverFirst bool) (*outbox.Receipt, string, error) {
	if record.SlackReceipt != nil {
		return record.SlackReceipt, record.SlackState, nil
	}
	if recoverFirst {
		receipt, err := s.SlackClient.Recover(ctx, prepared.SlackPrepared)
		if err == nil {
			return receipt, "RECOVERED", nil
		}
	}
	receipt, err := s.SlackClient.Send(ctx, prepared.SlackPrepared)
	if err == nil {
		return receipt, "COMMITTED", nil
	}
	receipt, recoverErr := s.SlackClient.Recover(ctx, prepared.SlackPrepared)
	if recoverErr != nil {
		return nil, "PENDING", recoverErr
	}
	return receipt, "RECOVERED", nil
}

func commitResultFromRecord(transaction *gateway.TransactionRecord, record *CommitRecord) *CommitResult {
	return &CommitResult{
		Transaction:        transaction,
		GitHubReceipt:      record.GitHubReceipt,
		SlackReceipt:       record.SlackReceipt,
		CompensationState:  record.CompensationState,
		GitHubCompensation: record.GitHubCompensation,
	}
}

func commitRecordPath(prepared *Result) string {
	return filepath.Join(filepath.Dir(prepared.Transaction.LedgerPath), "mvpflow-commit.json")
}

func loadCommitRecord(path string) (*CommitRecord, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read commit record: %w", err)
	}
	var record CommitRecord
	if err := json.Unmarshal(bytes, &record); err != nil {
		return nil, fmt.Errorf("decode commit record: %w", err)
	}
	return &record, nil
}

func saveCommitRecord(path string, record *CommitRecord) error {
	record.UpdatedAt = time.Now().UTC()
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode commit record: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write commit record: %w", err)
	}
	return nil
}
