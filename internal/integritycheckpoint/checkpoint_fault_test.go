package integritycheckpoint

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/durablewrite"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
)

func checkpointFixture(t *testing.T) (root, priv, ring string) {
	t.Helper()
	root = t.TempDir()
	r, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	priv = filepath.Join(root, "private.json")
	ring = filepath.Join(root, "ring.json")
	pk, pub, err := operatorapproval.Generate("op", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := operatorapproval.WritePrivate(priv, pk); err != nil {
		t.Fatal(err)
	}
	if err := operatorapproval.WriteKeyring(ring, operatorapproval.Keyring{Version: operatorapproval.Version, Keys: []operatorapproval.PublicKey{pub}}); err != nil {
		t.Fatal(err)
	}
	return root, priv, ring
}

// countInjector fails the ath occurrence of op. The first durable write in
// Create is the ledger backup and the second is the checkpoint JSON, so
// occurrence 1 targets the backup boundaries and occurrence 2 targets the
// checkpoint-JSON boundaries.
type countInjector struct {
	mu   sync.Mutex
	op   string
	at   int
	seen int
	err  error
}

func (c *countInjector) Before(operation string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if operation == c.op {
		c.seen++
		if c.seen == c.at {
			return c.err
		}
	}
	return nil
}

func checkpointTemps(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "checkpoint.json.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCheckpointDurableSuccess(t *testing.T) {
	root, priv, ring := checkpointFixture(t)
	out := filepath.Join(root, "checkpoint.json")
	if _, err := CreateWithInjector(root, out, priv, ring, "", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "checkpoint.ledger.db")); err != nil {
		t.Fatalf("ledger backup missing: %v", err)
	}
	if temps := checkpointTemps(t, root); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
	if _, err := Verify(out, ring, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointBackupBoundaryFaults(t *testing.T) {
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
			root, priv, ring := checkpointFixture(t)
			out := filepath.Join(root, "checkpoint.json")
			inject := &countInjector{op: op, at: 1, err: durablewrite.ErrIO}
			if _, err := CreateWithInjector(root, out, priv, ring, "", time.Now(), inject); err == nil {
				t.Fatalf("op %s returned success", op)
			}
		})
	}
}

func TestCheckpointPreservesPreviousJSONBeforeRename(t *testing.T) {
	preRenameOps := []string{
		durablewrite.OpCreate,
		durablewrite.OpWrite,
		durablewrite.OpShortWrite,
		durablewrite.OpFileSync,
		durablewrite.OpRename,
	}
	for _, op := range preRenameOps {
		t.Run(op, func(t *testing.T) {
			root, priv, ring := checkpointFixture(t)
			out := filepath.Join(root, "checkpoint.json")
			if _, err := Create(root, out, priv, ring, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			// Occurrence 2 faults the checkpoint-JSON write, after the ledger
			// backup already succeeded.
			inject := &countInjector{op: op, at: 2, err: durablewrite.ErrIO}
			if _, err := CreateWithInjector(root, out, priv, ring, "", time.Now(), inject); err == nil {
				t.Fatalf("expected %s failure", op)
			}
			after, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("previous checkpoint changed after %s fault", op)
			}
			if _, err := Load(out); err != nil {
				t.Fatalf("previous checkpoint unreadable: %v", err)
			}
			if temps := checkpointTemps(t, root); len(temps) != 0 {
				t.Fatalf("leftover temps: %v", temps)
			}
		})
	}
}

func TestCheckpointJSONDirectorySyncFailureReported(t *testing.T) {
	root, priv, ring := checkpointFixture(t)
	out := filepath.Join(root, "checkpoint.json")
	// Occurrence 2 = the checkpoint-JSON directory sync, after its rename
	// committed. The new JSON is visible and self-consistent (its ledger
	// backup was written by the same run), but the fault must be reported.
	inject := &countInjector{op: durablewrite.OpDirectorySync, at: 2, err: durablewrite.ErrIO}
	if _, err := CreateWithInjector(root, out, priv, ring, "", time.Now(), inject); err == nil {
		t.Fatal("expected directory-sync failure; no false success")
	}
	if _, err := Load(out); err != nil {
		t.Fatalf("new checkpoint unreadable: %v", err)
	}
	if temps := checkpointTemps(t, root); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestCheckpointClassification(t *testing.T) {
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
			root, priv, ring := checkpointFixture(t)
			out := filepath.Join(root, "checkpoint.json")
			inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpCreate: c.fail})
			_, err := CreateWithInjector(root, out, priv, ring, "", time.Now(), inject)
			if err == nil {
				t.Fatal("expected failure")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
		})
	}
}

func TestCheckpointRetryAfterFaultRemoved(t *testing.T) {
	root, priv, ring := checkpointFixture(t)
	out := filepath.Join(root, "checkpoint.json")
	inject := durablewrite.NewOneShot(map[string]error{durablewrite.OpCreate: durablewrite.ErrIO})
	if _, err := CreateWithInjector(root, out, priv, ring, "", time.Now(), inject); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("checkpoint appeared despite first-attempt fault")
	}
	if _, err := CreateWithInjector(root, out, priv, ring, "", time.Now(), inject); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if _, err := Verify(out, ring, "", "", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointConcurrentDistinctRootsRaceSafe(t *testing.T) {
	// Separate roots avoid daemon-lock contention (Acquire is non-blocking),
	// so all writers genuinely run concurrently against the shared injector.
	inject := durablewrite.NewFaultMap(map[string]error{})
	var wg sync.WaitGroup
	errs := make([]error, 8)
	outs := make([]string, 8)
	rings := make([]string, 8)
	for i := 0; i < 8; i++ {
		root, priv, ring := checkpointFixture(t)
		outs[i] = filepath.Join(root, "checkpoint.json")
		rings[i] = ring
		wg.Add(1)
		go func(i int, root, priv string) {
			defer wg.Done()
			_, errs[i] = CreateWithInjector(root, outs[i], priv, rings[i], "", time.Now(), inject)
		}(i, root, priv)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("checkpoint %d: %v", i, err)
		}
		if _, err := Verify(outs[i], rings[i], "", "", time.Now()); err != nil {
			t.Fatalf("checkpoint %d verify: %v", i, err)
		}
		if temps := checkpointTemps(t, filepath.Dir(outs[i])); len(temps) != 0 {
			t.Fatalf("checkpoint %d leftover temps: %v", i, temps)
		}
	}
}
