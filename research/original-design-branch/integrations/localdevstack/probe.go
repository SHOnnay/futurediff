package localdevstack

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/futurediff/futurediff/control-plane/gateway"
	"github.com/futurediff/futurediff/staging/postgrespreview"
	"github.com/futurediff/futurediff/verifier/evidence/artifactstore"
)

type Report struct {
	RuntimeMode                     string            `json:"runtime_mode"`
	LayoutRoot                      string            `json:"layout_root"`
	RequiredBinaries                map[string]string `json:"required_binaries"`
	WorktreeTransactionState        string            `json:"worktree_transaction_state"`
	PostgresPreviewRollbackVerified bool              `json:"postgres_preview_rollback_verified"`
	ArtifactStoreReady              bool              `json:"artifact_store_ready"`
}

type Prober struct {
	Gateway  gateway.SpikeService
	PGBinDir string
}

func (p Prober) Probe(ctx context.Context, root string) (*Report, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("probe root is required")
	}
	layoutRoot := filepath.Join(root, ".futurediff-dev")
	for _, dir := range []string{
		filepath.Join(layoutRoot, "transactions"),
		filepath.Join(layoutRoot, "runtime"),
		filepath.Join(layoutRoot, "logs"),
		filepath.Join(layoutRoot, "cache"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create local-dev layout %s: %w", dir, err)
		}
	}

	required := map[string]string{}
	for _, name := range []string{"git", "go"} {
		path, err := exec.LookPath(name)
		if err != nil {
			return nil, fmt.Errorf("find %s: %w", name, err)
		}
		required[name] = path
	}
	binDir := p.PGBinDir
	if strings.TrimSpace(binDir) == "" {
		binDir = "/opt/homebrew/opt/postgresql@18/bin"
	}
	for _, name := range []string{"initdb", "pg_ctl", "psql", "pg_dump"} {
		path := filepath.Join(binDir, name)
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("find %s: %w", path, err)
		}
		required[name] = path
	}

	store, err := artifactstore.Open(filepath.Join(layoutRoot, "artifacts"))
	if err != nil {
		return nil, err
	}
	if _, err := store.PutBytes("bootstrap-check.txt", []byte("local-dev artifact store ok\n")); err != nil {
		return nil, fmt.Errorf("write artifact-store probe: %w", err)
	}

	repo, err := initGitRepo(filepath.Join(root, "repo-probe"))
	if err != nil {
		return nil, err
	}
	record, err := p.Gateway.RunWithOptions(ctx, repo, gateway.RunOptions{
		Command:       []string{"/bin/sh", "-c", "printf 'local dev stack\n' > probe.txt"},
		VerifyCommand: []string{"/bin/sh", "-c", "grep -q 'local dev stack' probe.txt"},
	})
	if err != nil {
		return nil, fmt.Errorf("run worktree probe: %w", err)
	}

	preview, err := postgrespreview.Run(ctx, postgrespreview.Config{
		UpSQL:       `CREATE TABLE probe_table (id BIGSERIAL PRIMARY KEY, note TEXT NOT NULL);`,
		DownSQL:     `DROP TABLE probe_table;`,
		EvidenceDir: filepath.Join(layoutRoot, "postgres-preview"),
		BinDir:      binDir,
	})
	if err != nil {
		return nil, fmt.Errorf("run postgres preview probe: %w", err)
	}

	return &Report{
		RuntimeMode:                     "host-shell-bootstrap",
		LayoutRoot:                      layoutRoot,
		RequiredBinaries:                required,
		WorktreeTransactionState:        record.State,
		PostgresPreviewRollbackVerified: preview.RollbackVerified,
		ArtifactStoreReady:              true,
	}, nil
}

func initGitRepo(repo string) (string, error) {
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return "", fmt.Errorf("create probe repo: %w", err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "devstack@example.com"},
		{"config", "user.name", "Dev Stack"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		if _, err := gitOutput(repo, args...); err != nil {
			return "", err
		}
	}
	return repo, nil
}

func gitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
