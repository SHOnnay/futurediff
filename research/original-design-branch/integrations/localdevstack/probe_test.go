package localdevstack

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeBootstrapStack(t *testing.T) {
	root := t.TempDir()
	report, err := Prober{}.Probe(context.Background(), root)
	if err != nil {
		t.Fatalf("probe local dev stack: %v", err)
	}
	if report.RuntimeMode != "host-shell-bootstrap" {
		t.Fatalf("unexpected runtime mode: %s", report.RuntimeMode)
	}
	if report.WorktreeTransactionState != "AWAITING_APPROVAL" {
		t.Fatalf("expected awaiting approval worktree state, got %s", report.WorktreeTransactionState)
	}
	if !report.PostgresPreviewRollbackVerified {
		t.Fatal("expected rollback-verified postgres preview")
	}
	if !report.ArtifactStoreReady {
		t.Fatal("expected artifact store to be ready")
	}
	for _, name := range []string{"git", "go", "initdb", "pg_ctl", "psql", "pg_dump"} {
		if report.RequiredBinaries[name] == "" {
			t.Fatalf("expected binary path for %s", name)
		}
	}
	for _, dir := range []string{"transactions", "runtime", "logs", "cache", "artifacts", "postgres-preview"} {
		if _, err := os.Stat(filepath.Join(report.LayoutRoot, dir)); err != nil {
			t.Fatalf("expected layout dir %s: %v", dir, err)
		}
	}
}
