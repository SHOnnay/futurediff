package postgrespreview

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCapturesSchemaDiffAndRollback(t *testing.T) {
	evidenceDir := t.TempDir()
	report, err := Run(context.Background(), Config{
		UpSQL:       `CREATE TABLE widgets (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL);`,
		DownSQL:     `DROP TABLE widgets;`,
		EvidenceDir: evidenceDir,
	})
	if err != nil {
		t.Fatalf("run preview: %v", err)
	}

	if report.SupportLevel != SupportLevelPreviewWithFreshnessCheck {
		t.Fatalf("unexpected support level: %s", report.SupportLevel)
	}
	if report.CommitMode != CommitModeFreshnessCheckRequired {
		t.Fatalf("unexpected commit mode: %s", report.CommitMode)
	}
	if !report.RollbackVerified {
		t.Fatal("expected rollback verification to succeed")
	}

	for _, path := range []string{report.SchemaBeforePath, report.SchemaAfterPath, report.SchemaAfterRollbackPath, report.SchemaDiffPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected evidence file %s: %v", path, err)
		}
	}

	diffBytes, err := os.ReadFile(report.SchemaDiffPath)
	if err != nil {
		t.Fatalf("read diff: %v", err)
	}
	diffText := string(diffBytes)
	if !strings.Contains(diffText, "CREATE TABLE public.widgets") {
		t.Fatalf("expected schema diff to mention widgets table, got %q", diffText)
	}

	rollbackBytes, err := os.ReadFile(report.SchemaAfterRollbackPath)
	if err != nil {
		t.Fatalf("read rollback schema: %v", err)
	}
	if strings.Contains(string(rollbackBytes), "widgets") {
		t.Fatalf("expected rollback schema to remove widgets table")
	}
	if filepath.Dir(report.SchemaBeforePath) != evidenceDir {
		t.Fatalf("expected evidence to be written in provided directory")
	}
}
