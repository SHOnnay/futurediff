package testpostgres

import (
	"database/sql"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type Instance struct {
	DB     *sql.DB
	DSN    string
	BinDir string
}

func Start(t testing.TB) *Instance {
	t.Helper()

	port := reservePort(t)
	dataDir := t.TempDir()
	binDir := "/opt/homebrew/opt/postgresql@18/bin"
	initdb := filepath.Join(binDir, "initdb")
	pgctl := filepath.Join(binDir, "pg_ctl")
	logPath := filepath.Join(dataDir, "postgres.log")

	mustRun(t, initdb, "-D", dataDir, "--locale=C", "-E", "UTF8", "-U", "postgres", "--auth=trust")
	mustRun(t, pgctl, "-D", dataDir, "-l", logPath, "-o", fmt.Sprintf("-F -p %d -h 127.0.0.1", port), "-w", "start")
	t.Cleanup(func() {
		_ = exec.Command(pgctl, "-D", dataDir, "-m", "immediate", "stop").Run()
	})

	dsn := fmt.Sprintf("postgres://postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := db.Ping(); err == nil {
			return &Instance{DB: db, DSN: dsn, BinDir: binDir}
		}
		if time.Now().After(deadline) {
			t.Fatal("postgres did not become ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func reservePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func mustRun(t testing.TB, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %v: %v: %s", name, args, err, string(output))
	}
}
