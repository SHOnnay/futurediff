package smoke

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/futurediff/futurediff/adapters/github/prcreate"
	"github.com/futurediff/futurediff/adapters/slack/outbox"
	"github.com/futurediff/futurediff/integrations/mvpflow"
)

type MigrationFailureReport struct {
	DirectDuration          time.Duration
	FutureDiffDuration      time.Duration
	DirectRealDBChanged     bool
	FutureDiffRealDBChanged bool
	FutureDiffBlocked       bool
	GitHubCalls             int
	SlackCalls              int
}

func CompareMigrationFailure(ctx context.Context) (*MigrationFailureReport, error) {
	report := &MigrationFailureReport{}
	badMigration := `CREATE TABLE danger_table (id BIGSERIAL PRIMARY KEY); SELECT nonexistent_function();`
	directApply := `CREATE TABLE danger_table (id BIGSERIAL PRIMARY KEY);`
	directFail := `SELECT nonexistent_function();`

	directDB, directStop, err := startTempPostgres(ctx)
	if err != nil {
		return nil, err
	}
	defer directStop()
	directStart := time.Now()
	if err := runPSQL(ctx, directDB.BinDir, directDB.DSN, directApply); err != nil {
		return nil, fmt.Errorf("apply direct migration step: %w", err)
	}
	directErr := runPSQL(ctx, directDB.BinDir, directDB.DSN, directFail)
	report.DirectDuration = time.Since(directStart)
	if directErr == nil {
		return nil, fmt.Errorf("expected direct migration failure")
	}
	directChanged, err := tableExists(ctx, directDB.DB, "danger_table")
	if err != nil {
		return nil, err
	}
	report.DirectRealDBChanged = directChanged

	futureDB, futureStop, err := startTempPostgres(ctx)
	if err != nil {
		return nil, err
	}
	defer futureStop()

	var (
		mu          sync.Mutex
		githubCalls int
		slackCalls  int
	)
	githubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		githubCalls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer githubServer.Close()
	slackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		slackCalls++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer slackServer.Close()

	repo, err := initGitRepo("migration-failure-benchmark")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(repo)
	service := mvpflow.Service{
		GitHubClient: prcreate.Client{BaseURL: githubServer.URL},
		SlackClient:  outbox.Client{BaseURL: slackServer.URL},
	}
	futureStart := time.Now()
	_, err = service.Prepare(ctx, mvpflow.Config{
		RepoPath:         repo,
		RepoCommand:      []string{"/bin/sh", "-c", "printf 'migration failure future\n' > migration.txt"},
		VerifyCommand:    []string{"/bin/sh", "-c", "grep -q 'migration failure future' migration.txt"},
		MigrationUpSQL:   badMigration,
		MigrationDownSQL: `DROP TABLE IF EXISTS danger_table;`,
		EvidenceDir:      filepath.Join(repo, ".futurediff-benchmark"),
		GitHubRequest: prcreate.CreateRequest{
			Owner:    "acme",
			Repo:     "payments",
			Title:    "Bad migration benchmark",
			Head:     "agent/bad-migration",
			Base:     "main",
			Body:     "Prepared by FutureDiff",
			EffectID: "eff_pr_bad_migration",
		},
		SlackRequest: outbox.SendRequest{
			Channel:  "C123",
			Text:     "Bad migration benchmark",
			EffectID: "eff_slack_bad_migration",
		},
	})
	report.FutureDiffDuration = time.Since(futureStart)
	if err == nil {
		return nil, fmt.Errorf("expected futurediff migration preview failure")
	}
	report.FutureDiffBlocked = true
	futureChanged, err := tableExists(ctx, futureDB.DB, "danger_table")
	if err != nil {
		return nil, err
	}
	report.FutureDiffRealDBChanged = futureChanged
	mu.Lock()
	report.GitHubCalls = githubCalls
	report.SlackCalls = slackCalls
	mu.Unlock()
	return report, nil
}

type tempPostgres struct {
	DB      *sql.DB
	DSN     string
	BinDir  string
	dataDir string
}

func startTempPostgres(ctx context.Context) (*tempPostgres, func(), error) {
	port, err := reservePort()
	if err != nil {
		return nil, nil, err
	}
	dataDir, err := os.MkdirTemp("", "futurediff-benchmark-pg-")
	if err != nil {
		return nil, nil, fmt.Errorf("create temp postgres dir: %w", err)
	}
	binDir := "/opt/homebrew/opt/postgresql@18/bin"
	logPath := filepath.Join(dataDir, "postgres.log")
	if err := runCmd(ctx, filepath.Join(binDir, "initdb"), "-D", dataDir, "--locale=C", "-E", "UTF8", "-U", "postgres", "--auth=trust"); err != nil {
		return nil, nil, fmt.Errorf("initdb: %w", err)
	}
	if err := runCmd(ctx, filepath.Join(binDir, "pg_ctl"), "-D", dataDir, "-l", logPath, "-o", fmt.Sprintf("-F -p %d -h 127.0.0.1", port), "-w", "start"); err != nil {
		return nil, nil, fmt.Errorf("start postgres: %w", err)
	}
	dsn := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres: %w", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := db.PingContext(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = db.Close()
			return nil, nil, fmt.Errorf("postgres did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop := func() {
		_ = db.Close()
		_ = exec.Command(filepath.Join(binDir, "pg_ctl"), "-D", dataDir, "-m", "immediate", "stop").Run()
		_ = os.RemoveAll(dataDir)
	}
	return &tempPostgres{DB: db, DSN: dsn, BinDir: binDir, dataDir: dataDir}, stop, nil
}

func runPSQL(ctx context.Context, binDir, dsn, sqlText string) error {
	cmd := exec.CommandContext(ctx, filepath.Join(binDir, "psql"), dsn, "-v", "ON_ERROR_STOP=1", "-c", sqlText)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql failed: %w: %s", err, string(output))
	}
	return nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var found sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public."+name).Scan(&found); err != nil {
		return false, fmt.Errorf("query table existence: %w", err)
	}
	return found.Valid, nil
}

func reservePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve port: %w", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func runCmd(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run %s %v: %w: %s", name, args, err, string(output))
	}
	return nil
}
