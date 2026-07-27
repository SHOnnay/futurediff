package postgrespreview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	SupportLevelPreviewWithFreshnessCheck = "preview_with_freshness_check"
	CommitModeFreshnessCheckRequired      = "freshness_check_required"
)

type Config struct {
	UpSQL       string
	DownSQL     string
	EvidenceDir string
	BinDir      string
}

type Report struct {
	SupportLevel            string `json:"support_level"`
	CommitMode              string `json:"commit_mode"`
	EvidenceDir             string `json:"evidence_dir"`
	SchemaBeforePath        string `json:"schema_before_path"`
	SchemaAfterPath         string `json:"schema_after_path"`
	SchemaAfterRollbackPath string `json:"schema_after_rollback_path"`
	SchemaDiffPath          string `json:"schema_diff_path"`
	RollbackVerified        bool   `json:"rollback_verified"`
}

func Run(ctx context.Context, cfg Config) (*Report, error) {
	if strings.TrimSpace(cfg.UpSQL) == "" {
		return nil, errors.New("up sql is required")
	}
	if strings.TrimSpace(cfg.DownSQL) == "" {
		return nil, errors.New("down sql is required")
	}

	binDir := cfg.BinDir
	if strings.TrimSpace(binDir) == "" {
		binDir = "/opt/homebrew/opt/postgresql@18/bin"
	}

	evidenceDir := cfg.EvidenceDir
	if strings.TrimSpace(evidenceDir) == "" {
		var err error
		evidenceDir, err = os.MkdirTemp("", "futurediff-pgpreview-")
		if err != nil {
			return nil, fmt.Errorf("create evidence dir: %w", err)
		}
	} else if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		return nil, fmt.Errorf("create evidence dir: %w", err)
	}

	dataDir, err := os.MkdirTemp("", "futurediff-pgcluster-")
	if err != nil {
		return nil, fmt.Errorf("create cluster dir: %w", err)
	}
	defer os.RemoveAll(dataDir)

	instance, err := startDisposablePostgres(ctx, binDir, dataDir)
	if err != nil {
		return nil, err
	}
	defer instance.stop()

	beforePath := filepath.Join(evidenceDir, "schema-before.sql")
	afterPath := filepath.Join(evidenceDir, "schema-after.sql")
	afterRollbackPath := filepath.Join(evidenceDir, "schema-after-rollback.sql")
	diffPath := filepath.Join(evidenceDir, "schema.diff")

	if err := dumpSchema(ctx, instance.bin("pg_dump"), instance.dsn, beforePath); err != nil {
		return nil, err
	}
	if err := runSQL(ctx, instance.bin("psql"), instance.dsn, cfg.UpSQL); err != nil {
		return nil, fmt.Errorf("apply up migration: %w", err)
	}
	if err := dumpSchema(ctx, instance.bin("pg_dump"), instance.dsn, afterPath); err != nil {
		return nil, err
	}
	if err := writeDiff(ctx, beforePath, afterPath, diffPath); err != nil {
		return nil, err
	}
	if err := runSQL(ctx, instance.bin("psql"), instance.dsn, cfg.DownSQL); err != nil {
		return nil, fmt.Errorf("apply down migration: %w", err)
	}
	if err := dumpSchema(ctx, instance.bin("pg_dump"), instance.dsn, afterRollbackPath); err != nil {
		return nil, err
	}

	rollbackVerified, err := sameFileContents(beforePath, afterRollbackPath)
	if err != nil {
		return nil, err
	}

	return &Report{
		SupportLevel:            SupportLevelPreviewWithFreshnessCheck,
		CommitMode:              CommitModeFreshnessCheckRequired,
		EvidenceDir:             evidenceDir,
		SchemaBeforePath:        beforePath,
		SchemaAfterPath:         afterPath,
		SchemaAfterRollbackPath: afterRollbackPath,
		SchemaDiffPath:          diffPath,
		RollbackVerified:        rollbackVerified,
	}, nil
}

type disposablePostgres struct {
	db      *sql.DB
	dsn     string
	dataDir string
	binDir  string
}

func (d *disposablePostgres) stop() {
	if d.db != nil {
		_ = d.db.Close()
	}
	_ = exec.Command(d.bin("pg_ctl"), "-D", d.dataDir, "-m", "immediate", "stop").Run()
}

func (d *disposablePostgres) bin(name string) string {
	return filepath.Join(d.binDir, name)
}

func startDisposablePostgres(ctx context.Context, binDir, dataDir string) (*disposablePostgres, error) {
	port, err := pickPort()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(dataDir, "postgres.log")
	if err := runCmd(ctx, filepath.Join(binDir, "initdb"), "-D", dataDir, "--locale=C", "-E", "UTF8", "-U", "postgres", "--auth=trust"); err != nil {
		return nil, fmt.Errorf("initdb: %w", err)
	}
	if err := runCmd(ctx, filepath.Join(binDir, "pg_ctl"), "-D", dataDir, "-l", logPath, "-o", fmt.Sprintf("-F -p %d -h 127.0.0.1", port), "-w", "start"); err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}

	dsn := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			return &disposablePostgres{db: db, dsn: dsn, dataDir: dataDir, binDir: binDir}, nil
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			return nil, errors.New("postgres did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func dumpSchema(ctx context.Context, pgDumpPath, dsn, outPath string) error {
	cmd := exec.CommandContext(ctx, pgDumpPath, "--schema-only", dsn)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pg_dump schema: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.WriteFile(outPath, output, 0o644); err != nil {
		return fmt.Errorf("write schema dump: %w", err)
	}
	return nil
}

func runSQL(ctx context.Context, psqlPath, dsn, sqlText string) error {
	cmd := exec.CommandContext(ctx, psqlPath, dsn, "-v", "ON_ERROR_STOP=1", "-c", sqlText)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeDiff(ctx context.Context, beforePath, afterPath, outPath string) error {
	cmd := exec.CommandContext(ctx, "git", "diff", "--no-index", "--", beforePath, afterPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr := new(exec.ExitError); errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			if err := os.WriteFile(outPath, output, 0o644); err != nil {
				return fmt.Errorf("write diff: %w", err)
			}
			return nil
		}
		return fmt.Errorf("build schema diff: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.WriteFile(outPath, output, 0o644); err != nil {
		return fmt.Errorf("write diff: %w", err)
	}
	return nil
}

func sameFileContents(a, b string) (bool, error) {
	aBytes, err := os.ReadFile(a)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", a, err)
	}
	bBytes, err := os.ReadFile(b)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", b, err)
	}
	return normalizeSchemaDump(string(aBytes)) == normalizeSchemaDump(string(bBytes)), nil
}

func normalizeSchemaDump(input string) string {
	lines := strings.Split(input, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `\restrict `) || strings.HasPrefix(trimmed, `\unrestrict `) {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s %v: %w: %s", name, args, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func pickPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("pick port: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
