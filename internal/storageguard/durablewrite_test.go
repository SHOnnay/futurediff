package storageguard

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/durablewrite"
)

// The durable-write fault-injection seam is applied only at the probe
// boundary: OSProbe carries an injector and WriteDurable routes through the
// shared durable-write helper. Production probes (OSProbe{} with no injector)
// behave exactly as before.

func probeDest(t *testing.T) (OSProbe, string) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "probe.json")
	if err := os.WriteFile(dest, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	return OSProbe{}, dest
}

func probeRead(t *testing.T, dest string) string {
	t.Helper()
	b, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func probeTemps(t *testing.T, dest string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(dest), filepath.Base(dest)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestProbeDurableWriteSuccessNoInjector(t *testing.T) {
	// Production behavior: no injector supplied, write behaves as a plain
	// atomic durable write.
	_, dest := probeDest(t)
	if err := (OSProbe{}).WriteDurable(dest, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := probeRead(t, dest); got != "new" {
		t.Fatalf("content=%q", got)
	}
	if temps := probeTemps(t, dest); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestProbeEvaluateWithRealOSProbe(t *testing.T) {
	root := t.TempDir()
	s, err := Evaluate(root, Policy{Version: Version, MinimumFreeBytes: 1}, OSProbe{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !s.Healthy {
		t.Fatalf("status=%+v", s)
	}
}

func TestProbeCreateFailure(t *testing.T) {
	p, dest := probeDest(t)
	p.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpCreate: durablewrite.ErrIO})
	if err := p.WriteDurable(dest, []byte("new"), 0o600); err == nil {
		t.Fatal("expected create failure")
	}
	if got := probeRead(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
}

func TestProbeWriteFailure(t *testing.T) {
	p, dest := probeDest(t)
	p.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpWrite: durablewrite.ErrIO})
	if err := p.WriteDurable(dest, []byte("new"), 0o600); err == nil {
		t.Fatal("expected write failure")
	}
	if got := probeRead(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if temps := probeTemps(t, dest); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestProbeShortWritePartialTempNeverAuthoritative(t *testing.T) {
	p, dest := probeDest(t)
	p.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpShortWrite: durablewrite.ErrIO})
	err := p.WriteDurable(dest, []byte("a much longer payload that would be truncated"), 0o600)
	if err == nil {
		t.Fatal("expected short-write failure")
	}
	var fe *durablewrite.FaultError
	if !errors.As(err, &fe) || fe.Code() != "short_write" {
		t.Fatalf("err=%v", err)
	}
	if got := probeRead(t, dest); got != "old" {
		t.Fatalf("partial temp became authoritative: %q", got)
	}
	if temps := probeTemps(t, dest); len(temps) != 0 {
		t.Fatalf("partial temp leaked: %v", temps)
	}
}

func TestProbeFileSyncFailure(t *testing.T) {
	p, dest := probeDest(t)
	p.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO})
	err := p.WriteDurable(dest, []byte("new"), 0o600)
	if err == nil {
		t.Fatal("expected file-sync failure")
	}
	var fe *durablewrite.FaultError
	if !errors.As(err, &fe) || fe.Code() != "sync_failure" {
		t.Fatalf("err=%v", err)
	}
	if got := probeRead(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if temps := probeTemps(t, dest); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestProbeRenameFailurePreservesPrevious(t *testing.T) {
	p, dest := probeDest(t)
	p.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpRename: durablewrite.ErrIO})
	err := p.WriteDurable(dest, []byte("new"), 0o600)
	if err == nil {
		t.Fatal("expected rename failure")
	}
	var fe *durablewrite.FaultError
	if !errors.As(err, &fe) || fe.Code() != "rename_failure" {
		t.Fatalf("err=%v", err)
	}
	if got := probeRead(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if temps := probeTemps(t, dest); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestProbeDirectorySyncFailureNoFalseSuccess(t *testing.T) {
	p, dest := probeDest(t)
	p.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpDirectorySync: durablewrite.ErrIO})
	err := p.WriteDurable(dest, []byte("new"), 0o600)
	if err == nil {
		t.Fatal("expected directory-sync failure; no false success")
	}
	var fe *durablewrite.FaultError
	if !errors.As(err, &fe) || fe.Code() != "dir_sync_failure" {
		t.Fatalf("err=%v", err)
	}
	// Rename committed before the directory sync; the new content is
	// authoritative, but the fault is reported rather than hidden.
	if got := probeRead(t, dest); got != "new" {
		t.Fatalf("dest=%q", got)
	}
}

func TestProbeClassification(t *testing.T) {
	cases := []struct {
		name string
		fail error
		want string
	}{
		{"enospc", durablewrite.ErrDiskFull, "disk_full"},
		{"edquot", durablewrite.ErrQuotaExceeded, "quota_exceeded"},
		{"erofs", durablewrite.ErrReadOnlyFilesystem, "filesystem_read_only"},
		{"eio", durablewrite.ErrIO, "durable_write_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, dest := probeDest(t)
			p.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpCreate: c.fail})
			err := p.WriteDurable(dest, []byte("new"), 0o600)
			if err == nil {
				t.Fatal("expected failure")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
			if got := probeRead(t, dest); got != "old" {
				t.Fatalf("dest changed: %q", got)
			}
		})
	}
}

func TestProbeNoFalseSuccess(t *testing.T) {
	ops := []string{
		durablewrite.OpCreate,
		durablewrite.OpWrite,
		durablewrite.OpShortWrite,
		durablewrite.OpFileSync,
		durablewrite.OpRename,
		durablewrite.OpDirectorySync,
	}
	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			p, dest := probeDest(t)
			p.Injector = durablewrite.NewFaultMap(map[string]error{op: durablewrite.ErrIO})
			if err := p.WriteDurable(dest, []byte("new"), 0o600); err == nil {
				t.Fatalf("op %s returned nil error (false success)", op)
			}
		})
	}
}

func TestProbeRetrySucceedsAfterFaultRemoved(t *testing.T) {
	p, dest := probeDest(t)
	p.Injector = durablewrite.NewOneShot(map[string]error{durablewrite.OpCreate: durablewrite.ErrIO})
	if err := p.WriteDurable(dest, []byte("new"), 0o600); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	if got := probeRead(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if err := p.WriteDurable(dest, []byte("new"), 0o600); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if got := probeRead(t, dest); got != "new" {
		t.Fatalf("dest=%q", got)
	}
}

func TestProbeConcurrentWritesRaceSafe(t *testing.T) {
	dir := t.TempDir()
	// Shared injector across goroutines exercises the injector's own
	// synchronization; distinct destinations exercise concurrent durable
	// writes.
	p := OSProbe{Injector: durablewrite.NewFaultMap(map[string]error{})}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dest := filepath.Join(dir, "probe-"+string(rune('a'+i))+".json")
			if err := p.WriteDurable(dest, []byte("payload"), 0o600); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 16 {
		t.Fatalf("expected 16 files, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Type().IsRegular() == false {
			t.Fatalf("unexpected entry: %v", e.Name())
		}
	}
}
