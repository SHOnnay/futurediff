package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/api"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/runtimeoci"
)

type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
	Skip Status = "skip"
)

type Check struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type Options struct {
	DataRoot         string
	Socket           string
	CredentialConfig string
	Runtime          string
}

type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	GOOS        string    `json:"goos"`
	GOARCH      string    `json:"goarch"`
	Healthy     bool      `json:"healthy"`
	Checks      []Check   `json:"checks"`
}

func Run(ctx context.Context, opts Options) Report {
	report := Report{GeneratedAt: time.Now().UTC(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, Healthy: true}
	add := func(c Check) {
		report.Checks = append(report.Checks, c)
		if c.Status == Fail {
			report.Healthy = false
		}
	}
	if opts.DataRoot == "" {
		add(Check{Name: "data_root", Status: Fail, Message: "data root is required"})
		return report
	}
	if st, err := os.Stat(opts.DataRoot); err != nil {
		add(Check{Name: "data_root", Status: Fail, Message: err.Error()})
	} else if !st.IsDir() {
		add(Check{Name: "data_root", Status: Fail, Message: "data root is not a directory"})
	} else if st.Mode().Perm()&0o077 != 0 {
		add(Check{Name: "data_root", Status: Warn, Message: fmt.Sprintf("permissions are %04o; 0700 is recommended", st.Mode().Perm())})
	} else {
		add(Check{Name: "data_root", Status: Pass, Message: "private data root", Details: fmt.Sprintf("%04o", st.Mode().Perm())})
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		add(Check{Name: "git", Status: Fail, Message: err.Error()})
	} else {
		cmd := exec.CommandContext(ctx, gitPath, "--version")
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		out, e := cmd.CombinedOutput()
		if e != nil {
			add(Check{Name: "git", Status: Fail, Message: e.Error()})
		} else {
			add(Check{Name: "git", Status: Pass, Message: strings.TrimSpace(string(out)), Details: gitPath})
		}
	}
	add(Check{Name: "sqlite", Status: Pass, Message: "system SQLite library available", Details: ledger.SQLiteVersion()})
	ledgerPath := filepath.Join(opts.DataRoot, "ledger.db")
	repo, err := ledger.OpenRepository(ledgerPath)
	if err != nil {
		add(Check{Name: "ledger", Status: Fail, Message: err.Error()})
	} else {
		audit, e := repo.Audit()
		_ = repo.Close()
		if e != nil {
			add(Check{Name: "ledger", Status: Fail, Message: e.Error()})
		} else if !audit.Healthy {
			add(Check{Name: "ledger", Status: Fail, Message: "ledger invariant audit failed", Details: audit})
		} else if audit.WarningCount > 0 {
			add(Check{Name: "ledger", Status: Warn, Message: "ledger audit passed with warnings", Details: audit})
		} else {
			add(Check{Name: "ledger", Status: Pass, Message: "ledger integrity and invariants passed", Details: audit})
		}
	}
	if opts.Socket == "" {
		add(Check{Name: "daemon", Status: Skip, Message: "socket path not configured"})
	} else if st, e := os.Stat(opts.Socket); e != nil {
		add(Check{Name: "daemon", Status: Warn, Message: "daemon socket is not available"})
	} else if st.Mode().Perm() != 0o600 {
		add(Check{Name: "daemon", Status: Fail, Message: fmt.Sprintf("socket permissions are %04o; expected 0600", st.Mode().Perm())})
	} else {
		client := api.NewClient(opts.Socket)
		raw, e := client.Do("GET", "/v1/health", nil)
		if e != nil {
			add(Check{Name: "daemon", Status: Fail, Message: e.Error()})
		} else {
			var health map[string]any
			_ = json.Unmarshal(raw, &health)
			add(Check{Name: "daemon", Status: Pass, Message: "private daemon is reachable", Details: health})
		}
	}
	if opts.CredentialConfig == "" {
		add(Check{Name: "credential_config", Status: Skip, Message: "credential configuration not supplied"})
	} else if st, e := os.Stat(opts.CredentialConfig); e != nil {
		add(Check{Name: "credential_config", Status: Fail, Message: e.Error()})
	} else if st.Mode().Perm() != 0o600 {
		add(Check{Name: "credential_config", Status: Fail, Message: fmt.Sprintf("permissions are %04o; expected 0600", st.Mode().Perm())})
	} else {
		add(Check{Name: "credential_config", Status: Pass, Message: "credential metadata file has private permissions"})
	}
	if opts.Runtime == "" {
		add(Check{Name: "rootless_runtime", Status: Skip, Message: "runtime not requested"})
	} else {
		kind := runtimeoci.RuntimeKind(opts.Runtime)
		backend, e := runtimeoci.ProbeContext(ctx, kind, opts.Runtime)
		if e != nil {
			add(Check{Name: "rootless_runtime", Status: Warn, Message: e.Error()})
		} else if !backend.Rootless {
			add(Check{Name: "rootless_runtime", Status: Fail, Message: "runtime is not rootless", Details: backend})
		} else {
			add(Check{Name: "rootless_runtime", Status: Pass, Message: "rootless runtime is ready", Details: backend})
		}
	}
	if opts.Socket != "" {
		if conn, e := net.DialTimeout("unix", opts.Socket, 500*time.Millisecond); e == nil {
			_ = conn.Close()
		}
	}
	return report
}
