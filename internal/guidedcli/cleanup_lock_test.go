package guidedcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/operatoraudit"
)

func newCleanupLockApp(t *testing.T) (*App, string) {
	t.Helper()
	// Unix socket paths are limited to ~104 bytes on macOS; t.TempDir() paths
	// embed the full test name and can exceed that, so build a short home.
	name := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	if len(name) > 10 {
		name = name[:10]
	}
	home := filepath.Join(os.TempDir(), fmt.Sprintf("fdcl-%d-%s", os.Getpid(), name))
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	paths, err := resolvePathConfig(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{
		Out: out, Err: errOut, Paths: paths, Socket: paths.Socket.Path,
		Store:    StateStore{Path: paths.State.Path},
		Renderer: Renderer{Out: out, Err: errOut, Color: false, Unicode: false},
	}
	return app, home
}

// writeStaleLock writes a lock file whose owner PID is dead, so Inspect
// classifies it as a stale lock candidate eligible for automatic cleanup.
func writeStaleLock(t *testing.T, home string) {
	t.Helper()
	meta := daemonlock.Metadata{
		Version:       daemonlock.Version,
		PID:           999999,
		UID:           os.Geteuid(),
		StartedAt:     time.Now().Add(-time.Hour).UTC(),
		Root:          home,
		Hostname:      "test-host",
		StartedAtNs:   time.Now().Add(-time.Hour).UnixNano(),
		BootID:        "test-boot",
		DaemonVersion: "test",
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, "daemon.lock")
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runCleanupLock(t *testing.T, app *App, args ...string) int {
	t.Helper()
	return app.Run(context.Background(), append([]string{"cleanup-lock"}, args...))
}

func TestCleanupLock_NoLock(t *testing.T) {
	app, _ := newCleanupLockApp(t)
	app.JSON = true
	if code := runCleanupLock(t, app); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var out struct {
		Kind   string `json:"kind"`
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.Action != "none" {
		t.Fatalf("expected action=none, got %s", out.Action)
	}
}

func TestCleanupLock_JSONWithoutYesRefuses(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	writeStaleLock(t, home)
	if code := runCleanupLock(t, app); code == 0 {
		t.Fatal("expected non-zero exit without --yes")
	}
	var out struct {
		Action     string `json:"action"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.Action != "refused" || out.ReasonCode != "confirmation_required" {
		t.Fatalf("expected refused/confirmation_required, got %s/%s", out.Action, out.ReasonCode)
	}
	if _, err := os.Lstat(filepath.Join(home, "daemon.lock")); err != nil {
		t.Fatal("lock must remain in place without --yes")
	}
}

func TestCleanupLock_NonInteractiveWithoutYesRefuses(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.Interactive = false
	writeStaleLock(t, home)
	if code := runCleanupLock(t, app); code == 0 {
		t.Fatal("expected non-zero exit without --yes in non-interactive mode")
	}
	if _, err := os.Lstat(filepath.Join(home, "daemon.lock")); err != nil {
		t.Fatal("lock must remain in place")
	}
}

func TestCleanupLock_JSONWithYesCleans(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	writeStaleLock(t, home)
	socketPath := filepath.Join(home, "futurediff.sock")
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runCleanupLock(t, app, "--yes"); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var out struct {
		Action           string `json:"action"`
		AutomaticCleanup bool   `json:"automatic_cleanup"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.Action != "cleaned" || !out.AutomaticCleanup {
		t.Fatalf("expected cleaned/automatic_cleanup=true, got %+v", out)
	}
	if _, err := os.Lstat(filepath.Join(home, "daemon.lock")); !os.IsNotExist(err) {
		t.Fatal("lock file should be removed")
	}
	if _, err := os.Lstat(socketPath); !os.IsNotExist(err) {
		t.Fatal("socket file should be removed")
	}
	// Audit event must have been recorded.
	store := operatoraudit.Store{Root: home}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("audit trail invalid after cleanup: %v", report.Findings)
	}
	events, err := store.Events()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.EventType == "lock_cleanup" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected lock_cleanup audit event")
	}
}

func TestCleanupLock_RepeatedCleanupIsIdempotent(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	writeStaleLock(t, home)
	if code := runCleanupLock(t, app, "--yes"); code != 0 {
		t.Fatalf("first cleanup exit code %d", code)
	}
	// Second invocation: no lock present, must not fail.
	app.Out.(*bytes.Buffer).Reset()
	if code := runCleanupLock(t, app, "--yes"); code != 0 {
		t.Fatalf("second cleanup exit code %d", code)
	}
	var out struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.Action != "none" {
		t.Fatalf("expected action=none on repeated cleanup, got %s", out.Action)
	}
}

func TestCleanupLock_CorruptLockCleanedWithYes(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	lockPath := filepath.Join(home, "daemon.lock")
	if err := os.WriteFile(lockPath, []byte("{corrupt json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Corrupt lock is eligible for cleanup (AutomaticCleanupAllowed=true) and
	// must not be blocked by the Inspect diagnostics error.
	if code := runCleanupLock(t, app, "--yes"); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
		t.Fatal("corrupt lock file should be removed")
	}
}

func TestCleanupLock_LiveReachableDaemonRefused(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	socketPath := filepath.Join(home, "futurediff.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	lockPath := filepath.Join(home, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if code := runCleanupLock(t, app, "--yes"); code == 0 {
		t.Fatal("expected refusal for live reachable daemon")
	}
	var out struct {
		Action     string `json:"action"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.Action != "refused" {
		t.Fatalf("expected refused, got %s", out.Action)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("live daemon lock must not be removed")
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatal("live daemon socket must not be removed")
	}
}

func TestCleanupLock_AmbiguousOwnerRefused(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	// A lock held by our live process but with no reachable daemon is ambiguous
	// and must fail closed.
	lockPath := filepath.Join(home, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if code := runCleanupLock(t, app, "--yes"); code == 0 {
		t.Fatal("expected refusal for ambiguous owner")
	}
	var out struct {
		Action     string `json:"action"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.Action != "refused" {
		t.Fatalf("expected refused, got %s", out.Action)
	}
	if out.ReasonCode != "lock_owner_ambiguous" {
		t.Fatalf("expected lock_owner_ambiguous, got %s", out.ReasonCode)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("ambiguous lock must not be removed")
	}
}

func TestCleanupLock_AuditWriteFailureRefuses(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	writeStaleLock(t, home)
	// Make audit recording fail: the audit directory path is blocked by a file.
	auditDir := filepath.Join(home, "audit")
	if err := os.WriteFile(auditDir, []byte("blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runCleanupLock(t, app, "--yes"); code == 0 {
		t.Fatal("expected refusal when audit recording fails")
	}
	var out struct {
		Action     string `json:"action"`
		ReasonCode string `json:"reason_code"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.Action != "refused" || out.ReasonCode != "audit_write_failed" {
		t.Fatalf("expected refused/audit_write_failed, got %s/%s", out.Action, out.ReasonCode)
	}
	if _, err := os.Lstat(filepath.Join(home, "daemon.lock")); err != nil {
		t.Fatal("lock must remain in place when audit recording fails")
	}
}

func TestCleanupLock_LiveSocketPreserved(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	writeStaleLock(t, home)
	socketPath := filepath.Join(home, "futurediff.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if code := runCleanupLock(t, app, "--yes"); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	var out struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	t.Logf("out buffer: %s", app.Out.(*bytes.Buffer).String())
	if out.Action != "partial" {
		t.Fatalf("expected action=partial when socket has a live listener, got %s", out.Action)
	}
	if _, err := os.Lstat(filepath.Join(home, "daemon.lock")); !os.IsNotExist(err) {
		t.Fatal("stale lock should be removed")
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatal("live socket must be preserved")
	}
}

func TestCleanupLock_ReacquiredLockRefused(t *testing.T) {
	app, home := newCleanupLockApp(t)
	app.JSON = true
	// A stale-looking lock that is actually held: RemoveIfUnheld must fail
	// closed instead of deleting a live daemon's lock file.
	lockPath := filepath.Join(home, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if code := runCleanupLock(t, app, "--yes"); code == 0 {
		t.Fatal("expected refusal for held lock")
	}
	var out struct {
		ReasonCode string `json:"reason_code"`
	}
	if err := json.Unmarshal([]byte(app.Out.(*bytes.Buffer).String()), &out); err != nil {
		t.Fatal(err)
	}
	if out.ReasonCode != "lock_owner_ambiguous" && out.ReasonCode != "lock_reacquired" {
		t.Fatalf("expected fail-closed reason code, got %s", out.ReasonCode)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("held lock must not be removed")
	}
	if !strings.Contains(app.Out.(*bytes.Buffer).String(), "refused") {
		t.Fatal("expected refused action in output")
	}
}
