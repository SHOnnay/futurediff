package mvpflow

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
	"github.com/futurediff/futurediff/adapters/slack/outbox"
)

func TestExportBundleForPreparedCrossToolFlow(t *testing.T) {
	repo := initGitRepo(t)
	evidenceDir := t.TempDir()
	service := Service{}
	result, err := service.Prepare(context.Background(), Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'exportable future\n' > export.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'exportable future' export.txt"},
		MigrationUpSQL:   `CREATE TABLE export_widgets (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		MigrationDownSQL: `DROP TABLE export_widgets;`,
		EvidenceDir:      evidenceDir,
		GitHubRequest: prcreate.CreateRequest{
			Owner:    "acme",
			Repo:     "payments",
			Title:    "Export widgets",
			Head:     "agent/export-widgets",
			Base:     "main",
			Body:     "Prepared by FutureDiff",
			EffectID: "eff_pr_export",
		},
		SlackRequest: outbox.SendRequest{
			Channel:  "C123",
			Text:     "Prepared export widgets",
			EffectID: "eff_slack_export",
		},
	})
	if err != nil {
		t.Fatalf("prepare flow: %v", err)
	}

	bundle, err := ExportBundle(t.TempDir(), result)
	if err != nil {
		t.Fatalf("export bundle: %v", err)
	}
	if _, err := os.Stat(bundle.FuturepackPath); err != nil {
		t.Fatalf("expected futurepack path: %v", err)
	}
	if bundle.Manifest.Metadata["transaction_state"] != "AWAITING_APPROVAL" {
		t.Fatalf("unexpected transaction state: %#v", bundle.Manifest.Metadata["transaction_state"])
	}

	archive, err := zip.OpenReader(bundle.FuturepackPath)
	if err != nil {
		t.Fatalf("open futurepack: %v", err)
	}
	defer archive.Close()

	entries := map[string]string{}
	for _, file := range archive.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", file.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", file.Name, err)
		}
		entries[file.Name] = string(content)
	}
	if _, ok := entries["manifest.json"]; !ok {
		t.Fatal("expected manifest.json entry")
	}
	joined := strings.Join(mapsKeys(entries), "\n")
	for _, needle := range []string{"staged.patch", "ledger.jsonl", "github-prepared.json", "slack-prepared.json", "postgres-schema.diff"} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("expected futurepack to contain %s, entries=%s", needle, joined)
		}
	}
}

func mapsKeys(entries map[string]string) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	return keys
}
