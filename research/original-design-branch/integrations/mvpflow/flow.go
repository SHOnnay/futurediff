package mvpflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
	"github.com/futurediff/futurediff/adapters/slack/outbox"
	"github.com/futurediff/futurediff/control-plane/gateway"
	"github.com/futurediff/futurediff/staging/postgrespreview"
)

type Config struct {
	RepoPath         string
	RepoCommand      []string
	VerifyCommand    []string
	CommandExecutor  gateway.CommandExecutor
	VerifyExecutor   gateway.CommandExecutor
	MigrationUpSQL   string
	MigrationDownSQL string
	EvidenceDir      string
	GitHubRequest    prcreate.CreateRequest
	SlackRequest     outbox.SendRequest
}

type Result struct {
	Transaction     *gateway.TransactionRecord
	StagedPatch     string
	PostgresPreview *postgrespreview.Report
	GitHubPrepared  prcreate.PreparedRequest
	SlackPrepared   outbox.PreparedMessage
}

type Service struct {
	Gateway            gateway.SpikeService
	GitHubClient       prcreate.Client
	SlackClient        outbox.Client
	AfterGitHubReceipt func(record *CommitRecord) error
}

func (s Service) Prepare(ctx context.Context, cfg Config) (*Result, error) {
	if strings.TrimSpace(cfg.RepoPath) == "" {
		return nil, errors.New("repo path is required")
	}
	if len(cfg.RepoCommand) == 0 {
		return nil, errors.New("repo command is required")
	}
	if strings.TrimSpace(cfg.MigrationUpSQL) == "" {
		return nil, errors.New("migration up SQL is required")
	}
	if strings.TrimSpace(cfg.MigrationDownSQL) == "" {
		return nil, errors.New("migration down SQL is required")
	}
	if strings.TrimSpace(cfg.EvidenceDir) == "" {
		return nil, errors.New("evidence dir is required")
	}
	if err := os.MkdirAll(cfg.EvidenceDir, 0o755); err != nil {
		return nil, fmt.Errorf("create evidence dir: %w", err)
	}

	result := &Result{
		GitHubPrepared: s.GitHubClient.Prepare(cfg.GitHubRequest),
		SlackPrepared:  s.SlackClient.Prepare(cfg.SlackRequest),
	}

	postgresEvidenceDir := filepath.Join(cfg.EvidenceDir, "postgres-preview")
	if err := os.MkdirAll(postgresEvidenceDir, 0o755); err != nil {
		return nil, fmt.Errorf("create postgres evidence dir: %w", err)
	}
	preview, err := postgrespreview.Run(ctx, postgrespreview.Config{
		UpSQL:       cfg.MigrationUpSQL,
		DownSQL:     cfg.MigrationDownSQL,
		EvidenceDir: postgresEvidenceDir,
	})
	if err != nil {
		return nil, fmt.Errorf("run postgres preview: %w", err)
	}
	result.PostgresPreview = preview

	record, err := s.Gateway.RunWithOptions(ctx, cfg.RepoPath, gateway.RunOptions{
		Command:         cfg.RepoCommand,
		VerifyCommand:   cfg.VerifyCommand,
		CommandExecutor: cfg.CommandExecutor,
		VerifyExecutor:  cfg.VerifyExecutor,
	})
	if err != nil {
		return nil, fmt.Errorf("run repo staging flow: %w", err)
	}
	inspected, patch, err := s.Gateway.Inspect(cfg.RepoPath, record.ID)
	if err != nil {
		return nil, fmt.Errorf("inspect staged patch: %w", err)
	}
	result.Transaction = inspected
	result.StagedPatch = patch
	return result, nil
}
