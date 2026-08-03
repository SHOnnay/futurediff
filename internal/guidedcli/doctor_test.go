package guidedcli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func runDoctorJSON(t *testing.T, home string) map[string]any {
	t.Helper()
	paths, err := resolvePathConfig(Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	app := &App{
		Out: out, Err: errOut, Paths: paths, Socket: paths.Socket.Path,
		Store:    StateStore{Path: paths.State.Path},
		Renderer: Renderer{Out: out, Err: errOut, Color: false, Unicode: false},
		JSON:     true,
		Daemon:   DaemonManager{Engine: &fakeEngine{}, Socket: paths.Socket.Path},
	}
	// doctor must tolerate a missing daemon manager.
	if code := app.Run(context.Background(), []string{"doctor"}); code != 0 {
		t.Fatalf("doctor exit code %d: %s", code, errOut.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatalf("doctor output is not JSON: %s", out.String())
	}
	return raw
}

func checkStatus(raw map[string]any, id string) (string, string, bool) {
	checks, _ := raw["checks"].([]any)
	for _, c := range checks {
		item, _ := c.(map[string]any)
		if item["ID"] == id {
			status, _ := item["Status"].(string)
			detail, _ := item["Detail"].(string)
			return status, detail, true
		}
	}
	return "", "", false
}

func TestDoctor_MissingLedgerIsNotInitialized(t *testing.T) {
	home := filepath.Join(t.TempDir(), "fdif-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := runDoctorJSON(t, home)
	status, detail, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatalf("missing ledger_integrity check; checks=%v", raw["checks"])
	}
	if status != "warn" {
		t.Fatalf("expected warn for missing ledger, got %s (%s)", status, detail)
	}
}

func TestDoctor_CorruptLedgerIsFailure(t *testing.T) {
	home := filepath.Join(t.TempDir(), "fdif-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	ledgerPath := filepath.Join(home, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := runDoctorJSON(t, home)
	status, detail, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatalf("missing ledger_integrity check; checks=%v", raw["checks"])
	}
	if status != "fail" {
		t.Fatalf("expected fail for corrupt ledger, got %s (%s)", status, detail)
	}
}

func TestDoctor_CorruptLockIsFailure(t *testing.T) {
	home := filepath.Join(t.TempDir(), "fdif-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, "daemon.lock")
	if err := os.WriteFile(lockPath, []byte("{corrupt json"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := runDoctorJSON(t, home)
	status, detail, ok := checkStatus(raw, "daemon_lock")
	if !ok {
		t.Fatal("missing daemon_lock check")
	}
	if status != "fail" {
		t.Fatalf("expected fail for corrupt lock, got %s (%s)", status, detail)
	}
}
