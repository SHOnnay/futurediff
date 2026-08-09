package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SHOnnay/futurediff/internal/adapters/githubbranch"
	"github.com/SHOnnay/futurediff/internal/adapters/githubdraft"
	"github.com/SHOnnay/futurediff/internal/adapters/slackoutbox"
	"github.com/SHOnnay/futurediff/internal/credentials"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

type PrepareGitHubBranchRequest struct {
	CredentialID string `json:"credential_id"`
	Owner        string `json:"owner"`
	Repo         string `json:"repo"`
	Branch       string `json:"branch"`
	RemoteURL    string `json:"remote_url"`
}

type PrepareSlackMessageRequest struct {
	CredentialID string            `json:"credential_id"`
	Input        slackoutbox.Input `json:"input"`
}

func (s *Service) enforceEffectQuota(transactionID string) error {
	count, err := s.Ledger.CountEffects(transactionID)
	if err != nil {
		return err
	}
	limit := s.quotaPolicy().MaxEffectsPerTransaction
	if count >= limit {
		return fmt.Errorf("external effect quota exceeded: %d/%d", count, limit)
	}
	return nil
}

func (s *Service) PrepareSlackMessage(transactionID string, req PrepareSlackMessageRequest) (domain.ExternalEffect, error) {
	if err := s.enforceEffectQuota(transactionID); err != nil {
		return domain.ExternalEffect{}, err
	}
	if s.Credentials == nil || s.Slack == nil {
		return domain.ExternalEffect{}, errors.New("Slack outbox is not configured")
	}
	if req.CredentialID == "" {
		return domain.ExternalEffect{}, errors.New("credential_id is required")
	}
	tx, err := s.Ledger.Get(transactionID)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	if tx.Status != domain.StateActive && tx.Status != domain.StateSealed {
		return domain.ExternalEffect{}, fmt.Errorf("Slack effects may be prepared only while transaction is active or sealed, found %s", tx.Status)
	}
	for _, dependencyID := range req.Input.DependsOn {
		dependency, err := s.Ledger.ExternalEffect(dependencyID)
		if err != nil {
			return domain.ExternalEffect{}, fmt.Errorf("Slack dependency %s: %w", dependencyID, err)
		}
		if dependency.TransactionID != transactionID {
			return domain.ExternalEffect{}, errors.New("Slack dependency belongs to another transaction")
		}
	}
	effectID := domain.NewID("eff")
	prepared, preview, err := s.Slack.Prepare(effectID, req.Input)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	destination, err := s.Slack.CommitDestination()
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return s.persistPreparedEffect(domain.ExternalEffect{
		EffectID: effectID, TransactionID: transactionID, ToolIdentity: slackoutbox.ToolIdentity, AdapterIdentity: slackoutbox.AdapterID,
		EffectClass: "outbox", RiskLevel: "medium", CredentialID: req.CredentialID, Operation: slackoutbox.CommitOperation, Destination: destination,
		ResourceVersions: map[string]string{"slack://" + prepared.Input.Channel: "prepared"}, IdempotencyKey: "slack-message:" + effectID, Status: domain.EffectVerified,
		Reversibility: "irreversible_social", CommitRank: 300, SupportLevel: slackoutbox.SupportLevel, DependsOn: prepared.Input.DependsOn,
	}, prepared.Input, prepared, preview)
}

type PrepareGitHubDraftPRRequest struct {
	CredentialID string            `json:"credential_id"`
	Input        githubdraft.Input `json:"input"`
}

func (s *Service) PrepareGitHubBranch(ctx context.Context, transactionID string, req PrepareGitHubBranchRequest) (domain.ExternalEffect, error) {
	if err := s.enforceEffectQuota(transactionID); err != nil {
		return domain.ExternalEffect{}, err
	}
	if s.Credentials == nil || s.GitHubBranch == nil {
		return domain.ExternalEffect{}, errors.New("GitHub branch credential path is not configured")
	}
	if req.CredentialID == "" {
		return domain.ExternalEffect{}, errors.New("credential_id is required")
	}
	tx, err := s.Ledger.Get(transactionID)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	if tx.Status != domain.StateSealed {
		return domain.ExternalEffect{}, fmt.Errorf("GitHub branch effects require a sealed transaction, found %s", tx.Status)
	}
	ws, err := s.Ledger.Workspace(transactionID)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	patch, err := s.Ledger.Patch(transactionID)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	predicted, err := s.Staging.PredictMaterializedRef(ws, patch)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	input := githubbranch.Input{Owner: req.Owner, Repo: req.Repo, Branch: req.Branch, RemoteURL: req.RemoteURL, CommitOID: predicted.CommitOID, TreeOID: predicted.ResultingTreeOID, Repository: ws.RepositoryRoot}
	destination, err := s.GitHubBranch.Destination(input)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	effectID := domain.NewID("eff")
	var prepared githubbranch.Prepared
	var preview githubbranch.Preview
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effectID, AdapterID: githubbranch.AdapterID, CredentialID: req.CredentialID, Operation: githubbranch.ReadOperation, Destination: destination}, func(token []byte) error {
		var prepareErr error
		prepared, preview, prepareErr = s.GitHubBranch.Prepare(ctx, input, token)
		return prepareErr
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return s.persistPreparedEffect(domain.ExternalEffect{
		EffectID: effectID, TransactionID: transactionID, ToolIdentity: githubbranch.ToolIdentity, AdapterIdentity: githubbranch.AdapterID,
		EffectClass: "external_api", RiskLevel: "high", CredentialID: req.CredentialID, Operation: githubbranch.CommitOperation, Destination: destination,
		ResourceVersions: prepared.ResourceVersions, IdempotencyKey: "github-branch:" + effectID, Status: domain.EffectVerified,
		Reversibility: "compensatable", CommitRank: 100, SupportLevel: githubbranch.SupportLevel,
	}, prepared.Input, prepared, preview)
}

func (s *Service) PrepareGitHubDraftPR(ctx context.Context, transactionID string, req PrepareGitHubDraftPRRequest) (domain.ExternalEffect, error) {
	if err := s.enforceEffectQuota(transactionID); err != nil {
		return domain.ExternalEffect{}, err
	}
	if s.Credentials == nil || s.GitHub == nil {
		return domain.ExternalEffect{}, errors.New("GitHub adapter is not configured")
	}
	if req.CredentialID == "" {
		return domain.ExternalEffect{}, errors.New("credential_id is required")
	}
	tx, err := s.Ledger.Get(transactionID)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	if tx.Status != domain.StateActive && tx.Status != domain.StateSealed {
		return domain.ExternalEffect{}, fmt.Errorf("GitHub effects may be prepared only while transaction is active or sealed, found %s", tx.Status)
	}
	depends := []string(nil)
	if req.Input.DependsOnEffectID != "" {
		dependency, err := s.Ledger.ExternalEffect(req.Input.DependsOnEffectID)
		if err != nil {
			return domain.ExternalEffect{}, fmt.Errorf("branch dependency: %w", err)
		}
		if dependency.TransactionID != transactionID || dependency.AdapterIdentity != githubbranch.AdapterID {
			return domain.ExternalEffect{}, errors.New("draft PR dependency must be a GitHub branch effect in the same transaction")
		}
		var branch githubbranch.Prepared
		if err := json.Unmarshal([]byte(dependency.PreparedJSON), &branch); err != nil {
			return domain.ExternalEffect{}, err
		}
		if req.Input.Owner != branch.Input.Owner || req.Input.Repo != branch.Input.Repo {
			return domain.ExternalEffect{}, errors.New("draft PR and branch dependency must target the same repository")
		}
		req.Input.Head = branch.Input.Branch
		req.Input.ExpectedHeadSHA = branch.Input.CommitOID
		depends = []string{dependency.EffectID}
	}
	effectID := domain.NewID("eff")
	readDestination, err := s.GitHub.ReadDestination(req.Input)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	var prepared githubdraft.Prepared
	var preview githubdraft.Preview
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effectID, AdapterID: githubdraft.AdapterID, CredentialID: req.CredentialID, Operation: githubdraft.ReadOperation, Destination: readDestination}, func(token []byte) error {
		var prepareErr error
		prepared, preview, prepareErr = s.GitHub.Prepare(ctx, effectID, req.Input, token)
		return prepareErr
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	destination, err := s.GitHub.Destination(prepared.Input)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	return s.persistPreparedEffect(domain.ExternalEffect{
		EffectID: effectID, TransactionID: transactionID, ToolIdentity: githubdraft.ToolIdentity, AdapterIdentity: githubdraft.AdapterID,
		EffectClass: "external_api", RiskLevel: "medium", CredentialID: req.CredentialID, Operation: githubdraft.CommitOperation, Destination: destination,
		ResourceVersions: prepared.ResourceVersions, IdempotencyKey: "github-draft-pr:" + effectID, Status: domain.EffectVerified,
		Reversibility: "compensatable", CommitRank: 200, SupportLevel: githubdraft.SupportLevel, DependsOn: depends,
	}, prepared.Input, prepared, preview)
}

func (s *Service) persistPreparedEffect(effect domain.ExternalEffect, input, prepared, preview any) (domain.ExternalEffect, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	preparedJSON, err := json.Marshal(prepared)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	previewJSON, err := json.Marshal(preview)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	effect.InputJSON = string(inputJSON)
	effect.InputDigest = domain.SHA256Bytes(inputJSON)
	effect.PreparedJSON = string(preparedJSON)
	effect.PreparedDigest = domain.SHA256Bytes(preparedJSON)
	effect.PreviewJSON = string(previewJSON)
	effect.PreviewDigest = domain.SHA256Bytes(previewJSON)
	return s.Ledger.CreateExternalEffect(ledger.PrepareExternalEffectInput{Effect: effect})
}

func (s *Service) RefreshGitHubEffect(ctx context.Context, transactionID, effectID string) (domain.ExternalEffect, error) {
	effect, err := s.Ledger.ExternalEffect(effectID)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	if effect.TransactionID != transactionID {
		return domain.ExternalEffect{}, errors.New("effect does not belong to transaction")
	}
	if effect.AdapterIdentity != githubdraft.AdapterID {
		return domain.ExternalEffect{}, errors.New("only GitHub draft-PR effects are refreshable in protocol 0.1")
	}
	var input githubdraft.Input
	if err := json.Unmarshal([]byte(effect.InputJSON), &input); err != nil {
		return domain.ExternalEffect{}, err
	}
	readDestination, err := s.GitHub.ReadDestination(input)
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	var prepared githubdraft.Prepared
	var preview githubdraft.Preview
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effectID, AdapterID: githubdraft.AdapterID, CredentialID: effect.CredentialID, Operation: githubdraft.ReadOperation, Destination: readDestination}, func(token []byte) error {
		var prepareErr error
		prepared, preview, prepareErr = s.GitHub.Prepare(ctx, effectID, input, token)
		return prepareErr
	})
	if err != nil {
		return domain.ExternalEffect{}, err
	}
	preparedJSON, _ := json.Marshal(prepared)
	previewJSON, _ := json.Marshal(preview)
	return s.Ledger.RefreshExternalEffect(effectID, string(preparedJSON), domain.SHA256Bytes(preparedJSON), string(previewJSON), domain.SHA256Bytes(previewJSON), prepared.ResourceVersions)
}

func (s *Service) ExternalEffects(transactionID string) ([]domain.ExternalEffect, error) {
	if _, err := s.Ledger.Get(transactionID); err != nil {
		return nil, err
	}
	return s.Ledger.ExternalEffects(transactionID)
}

func (s *Service) preflightExternalEffects(ctx context.Context, transactionID string) error {
	effects, err := s.Ledger.ExternalEffects(transactionID)
	if err != nil {
		return err
	}
	for _, effect := range effects {
		if effect.Status == domain.EffectCommitted {
			continue
		}
		var checkErr, adapterErr error
		switch effect.AdapterIdentity {
		case githubbranch.AdapterID:
			var p githubbranch.Prepared
			if err := json.Unmarshal([]byte(effect.PreparedJSON), &p); err != nil {
				return err
			}
			checkErr = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effect.EffectID, AdapterID: githubbranch.AdapterID, CredentialID: effect.CredentialID, Operation: githubbranch.ReadOperation, Destination: effect.Destination}, func(token []byte) error { adapterErr = s.GitHubBranch.VerifyFresh(ctx, p, token); return adapterErr })
		case githubdraft.AdapterID:
			var p githubdraft.Prepared
			if err := json.Unmarshal([]byte(effect.PreparedJSON), &p); err != nil {
				return err
			}
			readDestination, e := s.GitHub.ReadDestination(p.Input)
			if e != nil {
				return e
			}
			checkErr = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effect.EffectID, AdapterID: githubdraft.AdapterID, CredentialID: effect.CredentialID, Operation: githubdraft.ReadOperation, Destination: readDestination}, func(token []byte) error { adapterErr = s.GitHub.VerifyPreCommit(ctx, p, token); return adapterErr })
		case slackoutbox.AdapterID:
			var p slackoutbox.Prepared
			if err := json.Unmarshal([]byte(effect.PreparedJSON), &p); err != nil {
				return err
			}
			statusDestination, e := s.Slack.StatusDestination()
			if e != nil {
				return e
			}
			checkErr = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effect.EffectID, AdapterID: slackoutbox.AdapterID, CredentialID: effect.CredentialID, Operation: slackoutbox.StatusOperation, Destination: statusDestination}, func(token []byte) error {
				var status slackoutbox.StatusResult
				status, adapterErr = s.Slack.Status(ctx, p, token)
				if adapterErr == nil && status.Status == slackoutbox.StatusCommitted {
					return nil
				}
				return adapterErr
			})
		default:
			return fmt.Errorf("unsupported built-in adapter %s", effect.AdapterIdentity)
		}
		if checkErr != nil {
			classified := checkErr
			if adapterErr != nil {
				classified = adapterErr
			}
			if isStaleProviderError(classified) {
				_, transitionErr := s.Ledger.Transition(transactionID, domain.StateReady, domain.StateStale, "provider-preflight", checkErr.Error(), false, true)
				if transitionErr != nil {
					return fmt.Errorf("provider changed and stale transition failed: %w", transitionErr)
				}
			}
			return checkErr
		}
	}
	return nil
}

func isStaleProviderError(err error) bool {
	var a *githubdraft.ProviderError
	if errors.As(err, &a) && a.Class == "stale_resource_version" {
		return true
	}
	var b *githubbranch.ProviderError
	return errors.As(err, &b) && (b.Class == "stale_resource_version" || b.Class == "branch_already_exists")
}

func (s *Service) commitExternalEffects(ctx context.Context, transactionID string, fencingToken int64) error {
	effects, err := s.Ledger.ExternalEffects(transactionID)
	if err != nil {
		return err
	}
	committed := map[string]bool{}
	for _, e := range effects {
		if e.Status == domain.EffectCommitted {
			committed[e.EffectID] = true
		}
	}
	for _, effect := range effects {
		if effect.Status == domain.EffectCommitted {
			continue
		}
		if effect.Status != domain.EffectVerified && effect.Status != domain.EffectPrepared {
			return fmt.Errorf("effect %s is %s and cannot be committed", effect.EffectID, effect.Status)
		}
		for _, dependency := range effect.DependsOn {
			if !committed[dependency] {
				return fmt.Errorf("effect %s dependency %s is not committed", effect.EffectID, dependency)
			}
		}
		switch effect.AdapterIdentity {
		case githubbranch.AdapterID:
			err = s.commitGitHubBranch(ctx, effect, fencingToken)
		case githubdraft.AdapterID:
			err = s.commitGitHubPR(ctx, effect, fencingToken)
		case slackoutbox.AdapterID:
			err = s.commitSlackMessage(ctx, effect, fencingToken)
		default:
			err = fmt.Errorf("unsupported built-in adapter %s", effect.AdapterIdentity)
		}
		if err != nil {
			return err
		}
		committed[effect.EffectID] = true
	}
	return nil
}

func (s *Service) commitGitHubBranch(ctx context.Context, effect domain.ExternalEffect, fencingToken int64) error {
	var p githubbranch.Prepared
	if err := json.Unmarshal([]byte(effect.PreparedJSON), &p); err != nil {
		return err
	}
	attempt, err := s.Ledger.BeginEffectAttempt(effect.EffectID, "commit", p.RequestDigest, fencingToken)
	if err != nil {
		return err
	}
	var status githubbranch.StatusResult
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: effect.TransactionID, EffectID: effect.EffectID, AdapterID: githubbranch.AdapterID, CredentialID: effect.CredentialID, Operation: githubbranch.ReadOperation, Destination: effect.Destination}, func(token []byte) error { var e error; status, e = s.GitHubBranch.Status(ctx, p, token); return e })
	if err != nil {
		return s.effectUnknown(attempt, "branch_status_unknown", err)
	}
	if status.Status == githubbranch.StatusCommitted && status.Receipt != nil {
		return s.recordBranchReceipt(attempt, effect, p, *status.Receipt)
	}
	if status.Status == githubbranch.StatusConflict {
		return s.effectDefiniteFailure(attempt, "branch_conflict", fmt.Errorf("remote branch exists at %s", status.ObservedOID))
	}
	var receipt githubbranch.Receipt
	var publishAdapterErr error
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: effect.TransactionID, EffectID: effect.EffectID, AdapterID: githubbranch.AdapterID, CredentialID: effect.CredentialID, Operation: githubbranch.CommitOperation, Destination: effect.Destination}, func(token []byte) error {
		receipt, publishAdapterErr = s.GitHubBranch.Publish(ctx, p, token)
		return publishAdapterErr
	})
	if err != nil {
		// The credential wrapper redacts and re-strings the adapter error, so
		// classify from the raw adapter error to preserve ambiguity.
		classified := err
		if publishAdapterErr != nil {
			classified = publishAdapterErr
		}
		var pe *githubbranch.ProviderError
		if errors.As(classified, &pe) && pe.Ambiguous {
			return s.effectUnknown(attempt, pe.Class, err)
		}
		return s.effectDefiniteFailure(attempt, "branch_publish_failed", err)
	}
	return s.recordBranchReceipt(attempt, effect, p, receipt)
}

func (s *Service) recordBranchReceipt(attempt domain.EffectAttempt, effect domain.ExternalEffect, p githubbranch.Prepared, r githubbranch.Receipt) error {
	b, _ := json.Marshal(r)
	receipt := domain.EffectReceipt{ProviderOperationID: "github.git.push", ProviderResourceID: fmt.Sprintf("github://%s/%s/refs/heads/%s", p.Input.Owner, p.Input.Repo, p.Input.Branch), RequestDigest: p.RequestDigest, ResponseDigest: domain.SHA256Bytes(b), StatusQueryRef: effect.Destination, CommittedAt: r.ObservedAt}
	_, err := s.Ledger.RecordEffectCommitted(attempt, receipt)
	if err != nil {
		_, _ = s.Ledger.MarkNeedsReconciliation(effect.TransactionID, "branch published but receipt persistence failed")
	}
	return err
}

func (s *Service) commitGitHubPR(ctx context.Context, effect domain.ExternalEffect, fencingToken int64) error {
	var p githubdraft.Prepared
	if err := json.Unmarshal([]byte(effect.PreparedJSON), &p); err != nil {
		return err
	}
	attempt, err := s.Ledger.BeginEffectAttempt(effect.EffectID, "commit", p.RequestDigest, fencingToken)
	if err != nil {
		return err
	}
	readDestination, err := s.GitHub.ReadDestination(p.Input)
	if err != nil {
		return err
	}
	var verifyAdapterErr error
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: effect.TransactionID, EffectID: effect.EffectID, AdapterID: githubdraft.AdapterID, CredentialID: effect.CredentialID, Operation: githubdraft.ReadOperation, Destination: readDestination}, func(token []byte) error {
		verifyAdapterErr = s.GitHub.VerifyFresh(ctx, p, token)
		return verifyAdapterErr
	})
	if err != nil {
		classified := err
		if verifyAdapterErr != nil {
			classified = verifyAdapterErr
		}
		if isStaleProviderError(classified) {
			return s.effectDefiniteFailure(attempt, "stale_resource_version", err)
		}
		return s.effectUnknown(attempt, "precommit_ref_read_unknown", err)
	}
	var existing githubdraft.StatusResult
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: effect.TransactionID, EffectID: effect.EffectID, AdapterID: githubdraft.AdapterID, CredentialID: effect.CredentialID, Operation: githubdraft.StatusOperation, Destination: effect.Destination}, func(token []byte) error { var e error; existing, e = s.GitHub.Status(ctx, p, token); return e })
	if err != nil {
		return s.effectUnknown(attempt, "precommit_status_unknown", err)
	}
	if existing.Status == githubdraft.StatusCommitted && existing.Receipt != nil {
		return s.recordGitHubReceipt(attempt, effect, p, *existing.Receipt)
	}
	var receipt githubdraft.Receipt
	var createAdapterErr error
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: effect.TransactionID, EffectID: effect.EffectID, AdapterID: githubdraft.AdapterID, CredentialID: effect.CredentialID, Operation: githubdraft.CommitOperation, Destination: effect.Destination}, func(token []byte) error {
		receipt, createAdapterErr = s.GitHub.Create(ctx, p, token)
		return createAdapterErr
	})
	if err != nil {
		classified := err
		if createAdapterErr != nil {
			classified = createAdapterErr
		}
		var pe *githubdraft.ProviderError
		if errors.As(classified, &pe) && pe.Ambiguous {
			return s.effectUnknown(attempt, pe.Class, err)
		}
		return s.effectDefiniteFailure(attempt, "github_pr_failed", err)
	}
	return s.recordGitHubReceipt(attempt, effect, p, receipt)
}

func (s *Service) commitSlackMessage(ctx context.Context, effect domain.ExternalEffect, fencingToken int64) error {
	var p slackoutbox.Prepared
	if err := json.Unmarshal([]byte(effect.PreparedJSON), &p); err != nil {
		return err
	}
	attempt, err := s.Ledger.BeginEffectAttempt(effect.EffectID, "commit", p.RequestDigest, fencingToken)
	if err != nil {
		return err
	}
	statusDestination, err := s.Slack.StatusDestination()
	if err != nil {
		return err
	}
	var status slackoutbox.StatusResult
	var statusAdapterErr error
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: effect.TransactionID, EffectID: effect.EffectID, AdapterID: slackoutbox.AdapterID, CredentialID: effect.CredentialID, Operation: slackoutbox.StatusOperation, Destination: statusDestination}, func(token []byte) error {
		status, statusAdapterErr = s.Slack.Status(ctx, p, token)
		return statusAdapterErr
	})
	if err != nil {
		return s.effectUnknown(attempt, "slack_status_unknown", err)
	}
	if status.Status == slackoutbox.StatusCommitted && status.Receipt != nil {
		return s.recordSlackReceipt(attempt, effect, p, *status.Receipt)
	}
	var receipt slackoutbox.Receipt
	var postAdapterErr error
	err = s.withCredential(ctx, credentials.AccessRequest{TransactionID: effect.TransactionID, EffectID: effect.EffectID, AdapterID: slackoutbox.AdapterID, CredentialID: effect.CredentialID, Operation: slackoutbox.CommitOperation, Destination: effect.Destination}, func(token []byte) error {
		receipt, postAdapterErr = s.Slack.Post(ctx, p, token)
		return postAdapterErr
	})
	if err != nil {
		classified := err
		if postAdapterErr != nil {
			classified = postAdapterErr
		}
		var pe *slackoutbox.ProviderError
		if errors.As(classified, &pe) && pe.Ambiguous {
			return s.effectUnknown(attempt, pe.Class, err)
		}
		return s.effectDefiniteFailure(attempt, "slack_post_failed", err)
	}
	return s.recordSlackReceipt(attempt, effect, p, receipt)
}

func (s *Service) recordSlackReceipt(attempt domain.EffectAttempt, effect domain.ExternalEffect, p slackoutbox.Prepared, r slackoutbox.Receipt) error {
	b, _ := json.Marshal(r)
	receipt := domain.EffectReceipt{
		ProviderOperationID: "slack.chat.postMessage",
		ProviderResourceID:  fmt.Sprintf("slack://%s/messages/%s", r.Channel, r.Timestamp),
		RequestDigest:       p.RequestDigest,
		ResponseDigest:      domain.SHA256Bytes(b),
		StatusQueryRef:      effect.Destination,
		CommittedAt:         r.ObservedAt,
	}
	_, err := s.Ledger.RecordEffectCommitted(attempt, receipt)
	if err != nil {
		_, _ = s.Ledger.MarkNeedsReconciliation(effect.TransactionID, "Slack posted but receipt persistence failed")
	}
	return err
}

func (s *Service) effectUnknown(attempt domain.EffectAttempt, class string, err error) error {
	_, _ = s.Ledger.MarkEffectUnknown(attempt, class, err.Error())
	_, _ = s.Ledger.MarkNeedsReconciliation(attempt.TransactionID, "external effect outcome is unknown")
	return err
}
func (s *Service) effectDefiniteFailure(attempt domain.EffectAttempt, class string, err error) error {
	_, _ = s.Ledger.MarkEffectDefiniteFailure(attempt, 0, class, err.Error())
	_, _ = s.Ledger.MarkNeedsReconciliation(attempt.TransactionID, "external effect was definitely not committed")
	return err
}

func (s *Service) recordGitHubReceipt(attempt domain.EffectAttempt, effect domain.ExternalEffect, p githubdraft.Prepared, r githubdraft.Receipt) error {
	b, _ := json.Marshal(r)
	receipt := domain.EffectReceipt{ProviderOperationID: fmt.Sprintf("github.pull_request.%d", r.PullNumber), ProviderResourceID: fmt.Sprintf("github://%s/%s/pulls/%d", p.Input.Owner, p.Input.Repo, r.PullNumber), RequestDigest: p.RequestDigest, ResponseDigest: domain.SHA256Bytes(b), StatusQueryRef: effect.Destination, CommittedAt: r.ObservedAt}
	_, err := s.Ledger.RecordEffectCommitted(attempt, receipt)
	if err != nil {
		_, _ = s.Ledger.MarkNeedsReconciliation(effect.TransactionID, "provider committed but receipt persistence failed")
	}
	return err
}

func (s *Service) reconcileExternalEffects(ctx context.Context, transactionID string, fencingToken int64) (allCommitted bool, anyCommitted bool, err error) {
	effects, err := s.Ledger.ExternalEffects(transactionID)
	if err != nil {
		return false, false, err
	}
	allCommitted = true
	for _, effect := range effects {
		switch effect.Status {
		case domain.EffectCommitted:
			anyCommitted = true
			continue
		case domain.EffectVerified, domain.EffectPrepared:
			allCommitted = false
			continue
		case domain.EffectUnknown, domain.EffectCommitting:
			requestDigest, _ := domain.Digest(map[string]any{"phase": "status", "prepared_digest": effect.PreparedDigest, "idempotency_key": effect.IdempotencyKey})
			attempt, e := s.Ledger.BeginEffectAttempt(effect.EffectID, "status", requestDigest, fencingToken)
			if e != nil {
				return false, anyCommitted, e
			}
			switch effect.AdapterIdentity {
			case githubbranch.AdapterID:
				var p githubbranch.Prepared
				if e := json.Unmarshal([]byte(effect.PreparedJSON), &p); e != nil {
					return false, anyCommitted, e
				}
				var status githubbranch.StatusResult
				e = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effect.EffectID, AdapterID: githubbranch.AdapterID, CredentialID: effect.CredentialID, Operation: githubbranch.ReadOperation, Destination: effect.Destination}, func(token []byte) error { var x error; status, x = s.GitHubBranch.Status(ctx, p, token); return x })
				if e != nil {
					_, _ = s.Ledger.MarkEffectUnknown(attempt, "status_query_failed", e.Error())
					return false, anyCommitted, e
				}
				if status.Status == githubbranch.StatusNotFound {
					if _, e = s.Ledger.RearmEffect(effect.EffectID, &attempt, "remote branch is absent"); e != nil {
						return false, anyCommitted, e
					}
					allCommitted = false
					continue
				}
				if status.Status == githubbranch.StatusConflict {
					_, _ = s.Ledger.Transition(transactionID, domain.StateNeedsReconciliation, domain.StateManualIntervention, "github-branch", "remote branch conflicts with approved commit", false, false)
					return false, anyCommitted, errors.New("manual intervention required: remote branch conflict")
				}
				if status.Receipt == nil {
					return false, anyCommitted, errors.New("branch status committed without receipt")
				}
				if e = s.recordBranchReceipt(attempt, effect, p, *status.Receipt); e != nil {
					return false, anyCommitted, e
				}
				anyCommitted = true
			case githubdraft.AdapterID:
				var p githubdraft.Prepared
				if e := json.Unmarshal([]byte(effect.PreparedJSON), &p); e != nil {
					return false, anyCommitted, e
				}
				var status githubdraft.StatusResult
				e = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effect.EffectID, AdapterID: githubdraft.AdapterID, CredentialID: effect.CredentialID, Operation: githubdraft.StatusOperation, Destination: effect.Destination}, func(token []byte) error { var x error; status, x = s.GitHub.Status(ctx, p, token); return x })
				if e != nil {
					_, _ = s.Ledger.MarkEffectUnknown(attempt, "status_query_failed", e.Error())
					return false, anyCommitted, e
				}
				if status.Status == githubdraft.StatusNotFound {
					if _, e = s.Ledger.RearmEffect(effect.EffectID, &attempt, "no matching draft pull request exists"); e != nil {
						return false, anyCommitted, e
					}
					allCommitted = false
					continue
				}
				if status.Receipt == nil {
					return false, anyCommitted, errors.New("PR status committed without receipt")
				}
				if e = s.recordGitHubReceipt(attempt, effect, p, *status.Receipt); e != nil {
					return false, anyCommitted, e
				}
				anyCommitted = true
			case slackoutbox.AdapterID:
				var p slackoutbox.Prepared
				if e := json.Unmarshal([]byte(effect.PreparedJSON), &p); e != nil {
					return false, anyCommitted, e
				}
				statusDestination, e := s.Slack.StatusDestination()
				if e != nil {
					return false, anyCommitted, e
				}
				var status slackoutbox.StatusResult
				e = s.withCredential(ctx, credentials.AccessRequest{TransactionID: transactionID, EffectID: effect.EffectID, AdapterID: slackoutbox.AdapterID, CredentialID: effect.CredentialID, Operation: slackoutbox.StatusOperation, Destination: statusDestination}, func(token []byte) error { var x error; status, x = s.Slack.Status(ctx, p, token); return x })
				if e != nil {
					_, _ = s.Ledger.MarkEffectUnknown(attempt, "status_query_failed", e.Error())
					return false, anyCommitted, e
				}
				if status.Status == slackoutbox.StatusNotFound {
					if _, e = s.Ledger.RearmEffect(effect.EffectID, &attempt, "no matching Slack message exists"); e != nil {
						return false, anyCommitted, e
					}
					allCommitted = false
					continue
				}
				if status.Receipt == nil {
					return false, anyCommitted, errors.New("Slack status committed without receipt")
				}
				if e = s.recordSlackReceipt(attempt, effect, p, *status.Receipt); e != nil {
					return false, anyCommitted, e
				}
				anyCommitted = true
			default:
				return false, anyCommitted, fmt.Errorf("unsupported adapter during reconciliation: %s", effect.AdapterIdentity)
			}
		default:
			return false, anyCommitted, fmt.Errorf("effect %s requires manual intervention from state %s", effect.EffectID, effect.Status)
		}
	}
	return allCommitted, anyCommitted, nil
}

func (s *Service) withCredential(ctx context.Context, request credentials.AccessRequest, use func([]byte) error) error {
	if s.Credentials == nil {
		return errors.New("credential broker is not configured")
	}
	return s.Credentials.WithCredential(ctx, request, func(secret credentials.Secret) error {
		token := secret.CopyBytes()
		defer zero(token)
		return use(token)
	})
}
func zero(v []byte) {
	for i := range v {
		v[i] = 0
	}
}
func (s *Service) coordinatorID() string {
	if s.CoordinatorID != "" {
		return s.CoordinatorID
	}
	return "local-daemon"
}
func (s *Service) acquireTransactionLease(transactionID string) (int64, error) {
	return s.Ledger.AcquireLease("transaction:"+transactionID, s.coordinatorID(), 5*time.Minute)
}
