package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

// newStartupRoot returns an isolated short data root.
func newStartupRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "fdd-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func lockPathFor(root string) string {
	return filepath.Join(root, "daemon.lock")
}

// writeLedgerFixture creates a checkpointed ledger in root. Test-only: the
// repository API is how real ledgers are made; the daemon gate itself never
// opens the ledger this way.
func writeLedgerFixture(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "ledger.db")
	r, err := ledger.OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(ledger.CreateInput{
		Transaction: domain.Transaction{ID: "tx-gate", Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()},
		Workspace:   domain.Workspace{TransactionID: "tx-gate", RepositoryRoot: "/repo", GitCommonDir: "/repo/.git", BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: "/runtime/ws", ArtifactsPath: "/runtime/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "d"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Lstat(sidecar); err == nil {
			if err := os.Remove(sidecar); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}

// writeWALLedgerAtRest copies a live WAL-mode ledger (db+wal+shm) into root
// while the connection is still open: byte-identical to a crashed daemon's
// ledger with uncheckpointed frames at rest.
func writeWALLedgerAtRest(t *testing.T, root string) string {
	t.Helper()
	scratch := filepath.Join(os.TempDir(), fmt.Sprintf("fdd-src-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(scratch)
	path := filepath.Join(scratch, "ledger.db")
	r, err := ledger.OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(ledger.CreateInput{
		Transaction: domain.Transaction{ID: "tx-gate", Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()},
		Workspace:   domain.Workspace{TransactionID: "tx-gate", RepositoryRoot: "/repo", GitCommonDir: "/repo/.git", BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: "/runtime/ws", ArtifactsPath: "/runtime/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "d"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ledger.db", "ledger.db-wal", "ledger.db-shm"} {
		b, err := os.ReadFile(filepath.Join(scratch, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "ledger.db")
}

func corruptMiddlePage(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int(b[16])<<8 | int(b[17])
	if pageSize == 0 {
		pageSize = 4096
	}
	offset := pageSize * (len(b) / pageSize / 2)
	if offset+pageSize > len(b) {
		t.Fatalf("database too small to corrupt a middle page (%d bytes)", len(b))
	}
	for i := offset; i < offset+pageSize; i++ {
		b[i] = 0xA5
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func dirEntrySet(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// setDaemonDiagnose installs a counting seam over the offline diagnosis entry
// point used by the startup gate. It asserts the gate always requests the
// quiescent, full-integrity contract.
func setDaemonDiagnose(t *testing.T, diagnose func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error)) *int {
	t.Helper()
	old := daemonDiagnose
	calls := 0
	daemonDiagnose = func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		if !opts.Quiescent {
			t.Fatalf("startup gate must diagnose with Quiescent=true")
		}
		if !opts.FullIntegrity {
			t.Fatalf("startup gate must diagnose with FullIntegrity=true")
		}
		calls++
		return diagnose(path, opts)
	}
	t.Cleanup(func() { daemonDiagnose = old })
	return &calls
}

// setOpenRepository installs a counting seam over the read-write ledger open.
func setOpenRepository(t *testing.T) *int {
	t.Helper()
	old := openRepository
	calls := 0
	openRepository = func(path string) (*ledger.Repository, error) {
		calls++
		return old(path)
	}
	t.Cleanup(func() { openRepository = old })
	return &calls
}

func gateError(t *testing.T, err error) *startupIntegrityError {
	t.Helper()
	if err == nil {
		t.Fatal("expected a gate refusal error")
	}
	var g *startupIntegrityError
	if !errors.As(err, &g) {
		t.Fatalf("error is %T, want *startupIntegrityError: %v", err, err)
	}
	return g
}

func TestOpenLedgerForStartup_FlagAbsentPreservesNormalStartup(t *testing.T) {
	// Without --require-integrity the historical behavior is preserved: a
	// missing ledger is created by the read-write open, with no diagnosis.
	root := newStartupRoot(t)
	calls := setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})
	openCalls := setOpenRepository(t)

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), false)
	if err != nil {
		t.Fatal(err)
	}
	if repo == nil || lock == nil {
		t.Fatal("expected a live lock and repository")
	}
	if _, err := os.Lstat(filepath.Join(root, "ledger.db")); err != nil {
		t.Fatalf("flag-absent startup must create the missing ledger: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("diagnosis must not run without --require-integrity (%d calls)", *calls)
	}
	if *openCalls != 1 {
		t.Fatalf("expected exactly one read-write open, got %d", *openCalls)
	}
	_ = repo.Close()
	_ = lock.Release()
	// The lock must actually be released afterwards.
	if _, err := daemonlock.Acquire(lockPathFor(root), root, time.Now()); err != nil {
		t.Fatalf("lock not released after startup: %v", err)
	}
}

func TestOpenLedgerForStartup_MissingLedgerAllowsInitialization(t *testing.T) {
	// database_not_initialized is a valid first-start state: the gate passes
	// and the read-write open initializes the ledger afterwards.
	root := newStartupRoot(t)
	calls := setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.DatabaseNotInitialized, ReasonCode: "database_not_initialized"}, nil
	})
	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err != nil {
		t.Fatal(err)
	}
	if repo == nil {
		t.Fatal("missing ledger must still initialize")
	}
	if _, err := os.Lstat(filepath.Join(root, "ledger.db")); err != nil {
		t.Fatalf("ledger not initialized: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("expected one diagnosis call, got %d", *calls)
	}
	_ = repo.Close()
	_ = lock.Release()
}

func TestOpenLedgerForStartup_HealthyLedgerStarts(t *testing.T) {
	root := newStartupRoot(t)
	path := writeLedgerFixture(t, root)
	dbHash := sha256File(t, path)
	calls := setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})
	openCalls := setOpenRepository(t)

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err != nil {
		t.Fatal(err)
	}
	if repo == nil {
		t.Fatal("healthy ledger must start")
	}
	if *calls != 1 {
		t.Fatalf("expected one diagnosis call, got %d", *calls)
	}
	if *openCalls != 1 {
		t.Fatalf("expected one read-write open, got %d", *openCalls)
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("ledger bytes changed by the gate")
	}
	_ = repo.Close()
	_ = lock.Release()
}

func TestOpenLedgerForStartup_FullIntegritySelected(t *testing.T) {
	// The gate requests the full integrity contract: Quiescent and
	// FullIntegrity must both be true (asserted inside the seam itself).
	root := newStartupRoot(t)
	writeLedgerFixture(t, root)
	calls := setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})
	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatalf("expected one diagnosis call, got %d", *calls)
	}
	_ = repo.Close()
	_ = lock.Release()
}

func TestOpenLedgerForStartup_InvalidHeaderRefusesStartup(t *testing.T) {
	root := newStartupRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256File(t, ledgerPath)
	openCalls := setOpenRepository(t)

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err == nil {
		_ = repo.Close()
		_ = lock.Release()
		t.Fatal("invalid header must refuse startup")
	}
	g := gateError(t, err)
	if g.ReasonCode != "ledger_corrupt" {
		t.Fatalf("expected reason_code=ledger_corrupt, got %s", g.ReasonCode)
	}
	if !strings.Contains(strings.ToLower(g.RecommendedAction), "restore") {
		t.Fatalf("expected restore guidance: %q", g.RecommendedAction)
	}
	if *openCalls != 0 {
		t.Fatalf("read-write open ran despite gate refusal (%d calls)", *openCalls)
	}
	if got := sha256File(t, ledgerPath); got != before {
		t.Fatal("refused startup modified the ledger")
	}
	assertNoStartupArtifacts(t, root, ledgerPath)
}

func TestOpenLedgerForStartup_IntegrityCheckFailureRefusesStartup(t *testing.T) {
	// The real offline diagnosis runs against the corrupted ledger (no
	// seam): the full-integrity gate must refuse with ledger_integrity_failed
	// before any read-write open.
	root := newStartupRoot(t)
	path := writeLedgerFixture(t, root)
	corruptMiddlePage(t, path)
	before := sha256File(t, path)
	openCalls := setOpenRepository(t)

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err == nil {
		_ = repo.Close()
		_ = lock.Release()
		t.Fatal("integrity failure must refuse startup")
	}
	g := gateError(t, err)
	if g.ReasonCode != "ledger_integrity_failed" {
		t.Fatalf("expected reason_code=ledger_integrity_failed, got %s", g.ReasonCode)
	}
	if *openCalls != 0 {
		t.Fatalf("read-write open ran despite gate refusal (%d calls)", *openCalls)
	}
	if got := sha256File(t, path); got != before {
		t.Fatal("refused startup modified the ledger")
	}
}

func TestOpenLedgerForStartup_WALInconsistentRefusesStartup(t *testing.T) {
	root := newStartupRoot(t)
	path := writeWALLedgerAtRest(t, root)
	dbHash := sha256File(t, path)
	walHash := sha256File(t, path+"-wal")
	shmHash := sha256File(t, path+"-shm")
	calls := setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.WALInconsistent, ReasonCode: "wal_inconsistent", Message: "WAL/SHM is inconsistent with the ledger database"}, nil
	})

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err == nil {
		_ = repo.Close()
		_ = lock.Release()
		t.Fatal("WAL inconsistency must refuse startup")
	}
	g := gateError(t, err)
	if g.ReasonCode != "wal_inconsistent" {
		t.Fatalf("expected reason_code=wal_inconsistent, got %s", g.ReasonCode)
	}
	if *calls != 1 {
		t.Fatalf("expected one diagnosis call, got %d", *calls)
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("ledger bytes changed")
	}
	if got := sha256File(t, path+"-wal"); got != walHash {
		t.Fatal("WAL bytes changed")
	}
	if got := sha256File(t, path+"-shm"); got != shmHash {
		t.Fatal("SHM bytes changed")
	}
	// The pre-existing fixture set (db+wal+shm, plus daemonlock's persistent
	// lock file) is preserved and nothing new appeared.
	want := map[string]bool{"ledger.db": true, "ledger.db-wal": true, "ledger.db-shm": true, "daemon.lock": true}
	if got := dirEntrySet(t, root); len(got) != len(want) {
		t.Fatalf("refused startup changed the file set: %v", got)
	}
	for name := range want {
		if !dirEntrySet(t, root)[name] {
			t.Fatalf("pre-existing %q disappeared", name)
		}
	}
}

func TestOpenLedgerForStartup_StorageIOFailureRefusesStartup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission failures cannot be simulated")
	}
	root := newStartupRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ledgerPath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ledgerPath, 0o600)

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err == nil {
		_ = repo.Close()
		_ = lock.Release()
		t.Fatal("storage I/O failure must refuse startup")
	}
	g := gateError(t, err)
	if g.ReasonCode != "storage_io_failure" {
		t.Fatalf("expected reason_code=storage_io_failure, got %s", g.ReasonCode)
	}
}

func TestOpenLedgerForStartup_InconclusiveRefusesStartup(t *testing.T) {
	root := newStartupRoot(t)
	writeLedgerFixture(t, root)
	calls := setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.DiagnosisInconclusive, ReasonCode: "diagnosis_inconclusive", Message: "cannot establish a coherent snapshot"}, nil
	})

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err == nil {
		_ = repo.Close()
		_ = lock.Release()
		t.Fatal("diagnosis_inconclusive must refuse startup")
	}
	g := gateError(t, err)
	if g.ReasonCode != "diagnosis_inconclusive" {
		t.Fatalf("expected reason_code=diagnosis_inconclusive, got %s", g.ReasonCode)
	}
	if *calls != 1 {
		t.Fatalf("expected one diagnosis call, got %d", *calls)
	}
}

func TestOpenLedgerForStartup_LiveDaemonPreventsDiagnosis(t *testing.T) {
	// A foreign live daemon holds the lock: startup fails with the existing
	// daemon-owner error and no offline diagnosis ever runs.
	root := newStartupRoot(t)
	writeLedgerFixture(t, root)
	foreign, err := daemonlock.Acquire(lockPathFor(root), root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Release()
	lockBytes, err := os.ReadFile(lockPathFor(root))
	if err != nil {
		t.Fatal(err)
	}
	calls := setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
	if err == nil {
		_ = repo.Close()
		if lock != nil {
			_ = lock.Release()
		}
		t.Fatal("a live daemon must prevent startup")
	}
	if !strings.Contains(err.Error(), "another FutureDiff daemon holds") {
		t.Fatalf("daemon-owner error not preserved: %v", err)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran behind a live daemon (%d calls)", *calls)
	}
	// The foreign lock and its metadata are untouched.
	after, err := os.ReadFile(lockPathFor(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(lockBytes) {
		t.Fatal("failed startup attempt modified the foreign lock")
	}
	// The foreign owner still holds the lock.
	if _, err := daemonlock.Acquire(lockPathFor(root), root, time.Now()); err == nil {
		t.Fatal("foreign lock was released by the failed attempt")
	}
}

func TestOpenLedgerForStartup_GateRunsBeforeSocketAcceptance(t *testing.T) {
	// Refusal happens inside the startup helper, before the API server is
	// ever constructed: no listening socket and no pid file appear.
	root := newStartupRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openLedgerForStartup(root, lockPathFor(root), true); err == nil {
		t.Fatal("corrupt ledger must refuse startup")
	}
	assertNoStartupArtifacts(t, root, ledgerPath)
}

func TestOpenLedgerForStartup_LockReleasedAfterRefusal(t *testing.T) {
	root := newStartupRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openLedgerForStartup(root, lockPathFor(root), true); err == nil {
		t.Fatal("corrupt ledger must refuse startup")
	}
	// The lock acquired by the failed attempt must be released: a fresh
	// acquisition succeeds, and the lock file is the only leftover artifact.
	reacquired, err := daemonlock.Acquire(lockPathFor(root), root, time.Now())
	if err != nil {
		t.Fatalf("startup-owned lock not released after refusal: %v", err)
	}
	_ = reacquired.Release()
}

func TestOpenLedgerForStartup_RepeatedFailuresAreIdempotent(t *testing.T) {
	root := newStartupRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256File(t, ledgerPath)
	entriesBefore := dirEntrySet(t, root)

	var firstErr error
	for i := 0; i < 3; i++ {
		if _, _, err := openLedgerForStartup(root, lockPathFor(root), true); err != nil {
			if i == 0 {
				firstErr = err
			} else if err.Error() != firstErr.Error() {
				t.Fatalf("attempt %d error drifted: %v vs %v", i+1, err, firstErr)
			}
		} else {
			t.Fatalf("attempt %d unexpectedly succeeded", i+1)
		}
	}
	if got := sha256File(t, ledgerPath); got != before {
		t.Fatal("repeated refusals modified the ledger")
	}
	for name := range dirEntrySet(t, root) {
		if name == "daemon.lock" {
			// daemonlock's persistent lock file; the flock itself is
			// released (verified by TestOpenLedgerForStartup_LockReleasedAfterRefusal).
			continue
		}
		if !entriesBefore[name] {
			t.Fatalf("repeated refusals created %q", name)
		}
	}
}

func TestOpenLedgerForStartup_ReasonCodeAndActionInError(t *testing.T) {
	root := newStartupRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openLedgerForStartup(root, lockPathFor(root), true); err == nil {
		t.Fatal("corrupt ledger must refuse startup")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "reason_code=ledger_corrupt") {
			t.Fatalf("error lacks stable reason code: %s", msg)
		}
		if !strings.Contains(msg, "recommended action:") {
			t.Fatalf("error lacks recommended action: %s", msg)
		}
	}
}

func TestOpenLedgerForStartup_NoSecretOrPathLeakage(t *testing.T) {
	root := newStartupRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openLedgerForStartup(root, lockPathFor(root), true); err == nil {
		t.Fatal("corrupt ledger must refuse startup")
	} else {
		msg := err.Error()
		for _, forbidden := range []string{root, ledgerPath, "GITHUB_TOKEN", "SUPER-SECRET", "FDIF_HOME"} {
			if strings.Contains(msg, forbidden) {
				t.Fatalf("startup error leaks %q: %s", forbidden, msg)
			}
		}
	}
}

func TestCheckStartupIntegrityStateMapping(t *testing.T) {
	cases := []struct {
		name       string
		state      ledger.DiagnosisState
		allowStart bool
		reasonCode string
	}{
		{name: "healthy", state: ledger.Healthy, allowStart: true},
		{name: "database not initialized", state: ledger.DatabaseNotInitialized, allowStart: true},
		{name: "ledger corrupt", state: ledger.LedgerCorrupt, reasonCode: "ledger_corrupt"},
		{name: "integrity failed", state: ledger.LedgerIntegrityFailed, reasonCode: "ledger_integrity_failed"},
		{name: "wal inconsistent", state: ledger.WALInconsistent, reasonCode: "wal_inconsistent"},
		{name: "storage io failure", state: ledger.StorageIOFailure, reasonCode: "storage_io_failure"},
		{name: "diagnosis inconclusive", state: ledger.DiagnosisInconclusive, reasonCode: "diagnosis_inconclusive"},
		{name: "unknown state fails closed", state: ledger.DiagnosisState("something_else"), reasonCode: "diagnosis_inconclusive"},
		{name: "empty state fails closed", state: "", reasonCode: "diagnosis_inconclusive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := newStartupRoot(t)
			writeLedgerFixture(t, root)
			setDaemonDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
				code := tc.reasonCode
				if tc.allowStart {
					code = string(tc.state)
				}
				return ledger.Diagnosis{State: tc.state, ReasonCode: code, Message: "state detail"}, nil
			})
			lock, repo, err := openLedgerForStartup(root, lockPathFor(root), true)
			if tc.allowStart {
				if err != nil {
					t.Fatalf("state %q must allow startup: %v", tc.state, err)
				}
				_ = repo.Close()
				_ = lock.Release()
				return
			}
			if err == nil {
				_ = repo.Close()
				_ = lock.Release()
				t.Fatalf("state %q must refuse startup", tc.state)
			}
			g := gateError(t, err)
			if g.ReasonCode != tc.reasonCode {
				t.Fatalf("expected reason_code=%s, got %s", tc.reasonCode, g.ReasonCode)
			}
			if g.RecommendedAction == "" {
				t.Fatal("refusal must carry a recommended action")
			}
		})
	}
}

// assertNoStartupArtifacts verifies a refused startup left no listening
// socket, pid file, or ledger sidecar behind, and left the pre-existing
// ledger.db intact. daemonlock's persistent daemon.lock file may exist (the
// flock itself is released; see TestOpenLedgerForStartup_LockReleasedAfterRefusal).
func assertNoStartupArtifacts(t *testing.T, root, ledgerPath string) {
	t.Helper()
	entries := dirEntrySet(t, root)
	for _, forbidden := range []string{"futurediff.sock", "futurediff.pid", "ledger.db-wal", "ledger.db-shm"} {
		if entries[forbidden] {
			t.Fatalf("refused startup created %q", forbidden)
		}
	}
	if entries["ledger.db"] == false && ledgerPath != "" {
		if _, err := os.Lstat(ledgerPath); err == nil {
			t.Fatal("pre-existing ledger disappeared")
		}
	}
}
