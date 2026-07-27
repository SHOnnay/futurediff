package mvpflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/futurediff/futurediff/verifier/evidence/artifactstore"
)

type ExportResult struct {
	FuturepackPath string
	Manifest       artifactstore.Manifest
}

func ExportBundle(outputDir string, result *Result) (*ExportResult, error) {
	if result == nil || result.Transaction == nil || result.PostgresPreview == nil {
		return nil, fmt.Errorf("complete result is required")
	}
	store, err := artifactstore.Open(filepath.Join(outputDir, "artifact-store"))
	if err != nil {
		return nil, err
	}
	artifacts := make([]artifactstore.Ref, 0, 12)

	for _, item := range []struct {
		name string
		path string
	}{
		{name: "staged.patch", path: result.Transaction.PatchPath},
		{name: "ledger.jsonl", path: result.Transaction.LedgerPath},
		{name: "transaction.json", path: filepath.Join(filepath.Dir(result.Transaction.LedgerPath), "transaction.json")},
		{name: "verification.log", path: result.Transaction.VerificationOutputPath},
		{name: "postgres-schema-before.sql", path: result.PostgresPreview.SchemaBeforePath},
		{name: "postgres-schema-after.sql", path: result.PostgresPreview.SchemaAfterPath},
		{name: "postgres-schema-after-rollback.sql", path: result.PostgresPreview.SchemaAfterRollbackPath},
		{name: "postgres-schema.diff", path: result.PostgresPreview.SchemaDiffPath},
	} {
		if item.path == "" {
			continue
		}
		if _, err := os.Stat(item.path); err != nil {
			return nil, fmt.Errorf("stat artifact %s: %w", item.path, err)
		}
		ref, err := store.PutFile(item.name, item.path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, ref)
	}

	for _, item := range []struct {
		name    string
		payload any
	}{
		{name: "github-prepared.json", payload: result.GitHubPrepared},
		{name: "slack-prepared.json", payload: result.SlackPrepared},
		{name: "cross-tool-result.json", payload: result},
	} {
		bytes, err := json.MarshalIndent(item.payload, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", item.name, err)
		}
		ref, err := store.PutBytes(item.name, append(bytes, '\n'))
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, ref)
	}

	manifest := artifactstore.Manifest{
		FormatVersion: "0.1",
		RunID:         fmt.Sprintf("cross-tool-prepare-%d", time.Now().UnixNano()),
		Scenario:      "cross-tool-prepare",
		Verdict:       "pass",
		Metadata: map[string]any{
			"transaction_id":         result.Transaction.ID,
			"transaction_state":      result.Transaction.State,
			"github_support_level":   result.GitHubPrepared.SupportLevel,
			"slack_support_level":    result.SlackPrepared.SupportLevel,
			"postgres_support_level": result.PostgresPreview.SupportLevel,
		},
		Artifacts: artifacts,
	}
	futurepackPath := filepath.Join(outputDir, "cross-tool-prepare.futurepack")
	if err := store.ExportFuturepack(futurepackPath, manifest); err != nil {
		return nil, err
	}
	return &ExportResult{FuturepackPath: futurepackPath, Manifest: manifest}, nil
}
