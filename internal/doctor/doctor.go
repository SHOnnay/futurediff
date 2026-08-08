package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	// The ledger check is quiescence-gated and non-mutating: the authoritative
	// ledger is never opened through the repository API (that path creates a
	// missing ledger and runs migrations), and the offline diagnosis needs to
	// know whether an authenticated daemon answered the health probe first.
	daemonCheck, daemonReachable := probeDaemon(ctx, opts)
	add(diagnoseLedger(opts.DataRoot, opts.Socket, daemonReachable))
	add(daemonCheck)

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

	return report
}

// probeDaemon runs the daemon reachability check. The returned bool reports
// whether an authenticated daemon answered the health probe; the ledger
// diagnosis uses it to refuse raw offline diagnosis while a daemon is live.
func probeDaemon(ctx context.Context, opts Options) (Check, bool) {
	if opts.Socket == "" {
		return Check{Name: "daemon", Status: Skip, Message: "socket path not configured"}, false
	}
	st, e := os.Stat(opts.Socket)
	if e != nil {
		return Check{Name: "daemon", Status: Warn, Message: "daemon socket is not available"}, false
	}
	if st.Mode().Perm() != 0o600 {
		return Check{Name: "daemon", Status: Fail, Message: fmt.Sprintf("socket permissions are %04o; expected 0600", st.Mode().Perm())}, false
	}
	client := api.NewClient(opts.Socket)
	raw, e := client.Do("GET", "/v1/health", nil)
	if e != nil {
		return Check{Name: "daemon", Status: Fail, Message: e.Error()}, false
	}
	var health map[string]any
	_ = json.Unmarshal(raw, &health)
	return Check{Name: "daemon", Status: Pass, Message: "private daemon is reachable", Details: health}, true
}
