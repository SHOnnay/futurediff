package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

// newDoctorRoot returns an isolated short data root. Unix socket paths are
// length-limited on macOS (104 bytes), so the root must be short enough for
// root/futurediff.sock to fit.
func newDoctorRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "fddr-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func doctorOptions(root string) Options {
	return Options{DataRoot: root, Socket: filepath.Join(root, "futurediff.sock")}
}

func findCheck(report Report, name string) *Check {
	for i := range report.Checks {
		if report.Checks[i].Name == name {
			return &report.Checks[i]
		}
	}
	return nil
}

func ledgerDetails(t *testing.T, report Report) ledgerDiagnosisResult {
	t.Helper()
	c := findCheck(report, "ledger")
	if c == nil {
		t.Fatal("missing ledger check")
	}
	details, ok := c.Details.(ledgerDiagnosisResult)
	if !ok {
		t.Fatalf("ledger details have type %T, want ledgerDiagnosisResult", c.Details)
	}
	return details
}

// setDoctorDiagnose installs a counting seam over the offline diagnosis entry
// point. It also asserts the doctor always requests the quiescent contract.
func setDoctorDiagnose(t *testing.T, diagnose func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error)) *int {
	t.Helper()
	old := doctorDiagnose
	calls := 0
	doctorDiagnose = func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		if !opts.Quiescent {
			t.Fatalf("doctor must diagnose with Quiescent=true")
		}
		calls++
		return diagnose(path, opts)
	}
	t.Cleanup(func() { doctorDiagnose = old })
	return &calls
}

// writeLedgerFixture creates a checkpointed ledger in root. Test-only: the
// repository API is how real ledgers are made; the doctor itself never opens
// the ledger this way.
func writeLedgerFixture(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "ledger.db")
	r, err := ledger.OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(ledger.CreateInput{
		Transaction: domain.Transaction{ID: "tx-doctor", Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()},
		Workspace:   domain.Workspace{TransactionID: "tx-doctor", RepositoryRoot: "/repo", GitCommonDir: "/repo/.git", BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: "/runtime/ws", ArtifactsPath: "/runtime/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "d"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	// The close checkpoint leaves a complete main file; drop residual empty
	// sidecars so the fixture is sidecar-free at rest.
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
// while the connection is still open: the copies are byte-identical to a
// crashed daemon's ledger with uncheckpointed frames at rest.
func writeWALLedgerAtRest(t *testing.T, root string) string {
	t.Helper()
	scratch := filepath.Join(os.TempDir(), fmt.Sprintf("fddr-src-%d-%d", os.Getpid(), time.Now().UnixNano()))
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
		Transaction: domain.Transaction{ID: "tx-doctor", Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()},
		Workspace:   domain.Workspace{TransactionID: "tx-doctor", RepositoryRoot: "/repo", GitCommonDir: "/repo/.git", BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: "/runtime/ws", ArtifactsPath: "/runtime/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "d"},
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

// corruptMiddlePage flips an entire middle page so SQLite quick_check fails.
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

// serveHealthSocket serves a daemon health endpoint over a Unix socket so the
// authenticated health probe succeeds.
func serveHealthSocket(t *testing.T, socketPath string) {
	t.Helper()
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})}
	t.Cleanup(func() { _ = srv.Close() })
	t.Cleanup(func() { _ = ln.Close() })
	go func() { _ = srv.Serve(ln) }()
}

// serveRefusingSocket listens on a Unix socket and immediately closes every
// accepted connection: connects succeed (a live process is present) but the
// HTTP health probe fails fast.
func serveRefusingSocket(t *testing.T, socketPath string) {
	t.Helper()
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
}

func TestDoctorFreshRootIsNotInitializedAndCreatesNothing(t *testing.T) {
	root := newDoctorRoot(t)
	before := dirEntrySet(t, root)
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	report := Run(context.Background(), doctorOptions(root))

	// The result is database_not_initialized, never corruption, and never
	// reported as healthy.
	c := findCheck(report, "ledger")
	if c == nil {
		t.Fatal("missing ledger check")
	}
	if c.Status != Warn {
		t.Fatalf("expected warn for a fresh root, got %s (%s)", c.Status, c.Message)
	}
	details := ledgerDetails(t, report)
	if details.ReasonCode != "database_not_initialized" || details.State != "database_not_initialized" {
		t.Fatalf("unexpected classification: %v / %v", details.ReasonCode, details.State)
	}
	if details.DiagnosisPerformed || details.QuiescenceProved {
		t.Fatalf("fresh root must not perform a diagnosis: %+v", details)
	}
	// A warn-only fresh root keeps the report healthy (not corruption).
	if !report.Healthy {
		t.Fatalf("fresh root must not flip the report to unhealthy: %+v", report)
	}

	// No ledger, sidecar, or daemon runtime file may appear.
	after := dirEntrySet(t, root)
	for name := range after {
		if !before[name] {
			t.Fatalf("doctor created %q", name)
		}
	}
	for _, forbidden := range []string{"ledger.db", "ledger.db-wal", "ledger.db-shm", "daemon.lock", "futurediff.sock"} {
		if after[forbidden] {
			t.Fatalf("doctor created %q", forbidden)
		}
	}
	if *calls != 0 {
		t.Fatalf("diagnosis ran on a fresh root (%d calls)", *calls)
	}

	// Repeated execution is non-mutating and idempotent.
	report2 := Run(context.Background(), doctorOptions(root))
	details2 := ledgerDetails(t, report2)
	if details2.ReasonCode != "database_not_initialized" {
		t.Fatalf("second run changed classification: %v", details2.ReasonCode)
	}
	if after2 := dirEntrySet(t, root); len(after2) != len(before) {
		t.Fatalf("second run created files: %v", after2)
	}
}

func TestDoctorHealthyOfflineLedger(t *testing.T) {
	root := newDoctorRoot(t)
	path := writeLedgerFixture(t, root)
	dbHash := sha256File(t, path)
	report := Run(context.Background(), doctorOptions(root))

	c := findCheck(report, "ledger")
	if c == nil {
		t.Fatal("missing ledger check")
	}
	if c.Status != Pass {
		t.Fatalf("expected pass, got %s (%s)", c.Status, c.Message)
	}
	details := ledgerDetails(t, report)
	if details.ReasonCode != "healthy" || details.State != "healthy" {
		t.Fatalf("unexpected classification: %v / %v", details.ReasonCode, details.State)
	}
	if details.IntegrityStatus != "pass" {
		t.Fatalf("unexpected integrity_status: %v", details.IntegrityStatus)
	}
	if !details.DiagnosisPerformed || !details.QuiescenceProved {
		t.Fatalf("expected a performed, quiescence-proved diagnosis: %+v", details)
	}
	if details.DaemonStatus != "stopped" {
		t.Fatalf("expected daemon_status stopped: %v", details.DaemonStatus)
	}
	if details.RecommendedAction != "none" {
		t.Fatalf("expected recommended_action none: %v", details.RecommendedAction)
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("ledger bytes changed")
	}
	assertNoSidecars(t, root)
}

func TestDoctorWALLedgerAtRestIsHealthyAndUnchanged(t *testing.T) {
	root := newDoctorRoot(t)
	path := writeWALLedgerAtRest(t, root)
	dbHash := sha256File(t, path)
	walHash := sha256File(t, path+"-wal")
	shmHash := sha256File(t, path+"-shm")
	report := Run(context.Background(), doctorOptions(root))

	details := ledgerDetails(t, report)
	if details.State != "healthy" {
		t.Fatalf("expected healthy with a live WAL at rest, got %v (%v)", details.State, details.Message)
	}
	if !details.WALPresent || !details.SHMPresent {
		t.Fatalf("sidecar flags must be reported: %+v", details)
	}
	if !details.DiagnosisPerformed || !details.QuiescenceProved {
		t.Fatalf("expected a performed diagnosis: %+v", details)
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
}

func TestDoctorInvalidHeaderIsCorrupt(t *testing.T) {
	root := newDoctorRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256File(t, ledgerPath)
	report := Run(context.Background(), doctorOptions(root))

	c := findCheck(report, "ledger")
	if c == nil || c.Status != Fail {
		t.Fatalf("expected fail for an invalid header, got %+v", c)
	}
	if report.Healthy {
		t.Fatal("a corrupt ledger must make the report unhealthy")
	}
	details := ledgerDetails(t, report)
	if details.ReasonCode != "ledger_corrupt" || details.State != "ledger_corrupt" {
		t.Fatalf("unexpected classification: %v / %v", details.ReasonCode, details.State)
	}
	if !details.DiagnosisPerformed || !details.QuiescenceProved {
		t.Fatalf("corrupt ledger must still be diagnosed under quiescence: %+v", details)
	}
	if details.IntegrityStatus != "fail" {
		t.Fatalf("expected fail integrity_status: %v", details.IntegrityStatus)
	}
	if got := sha256File(t, ledgerPath); got != before {
		t.Fatal("corrupt ledger bytes were modified")
	}
	assertNoSidecars(t, root)
}

func TestDoctorIntegrityCheckFailure(t *testing.T) {
	root := newDoctorRoot(t)
	path := writeLedgerFixture(t, root)
	corruptMiddlePage(t, path)
	report := Run(context.Background(), doctorOptions(root))

	c := findCheck(report, "ledger")
	if c == nil || c.Status != Fail {
		t.Fatalf("expected fail for integrity failure, got %+v", c)
	}
	details := ledgerDetails(t, report)
	if details.ReasonCode != "ledger_integrity_failed" || details.State != "ledger_integrity_failed" {
		t.Fatalf("unexpected classification: %v / %v", details.ReasonCode, details.State)
	}
	if details.QuickCheckOK {
		t.Fatalf("quick_check_ok must be false: %v", details.QuickCheckOK)
	}
	if len(details.IntegrityErrors) == 0 {
		t.Fatalf("integrity errors must be reported: %+v", details)
	}
}

func TestDoctorWALInconsistentMapping(t *testing.T) {
	// wal_inconsistent cannot be produced from a real file set with the
	// current SQLite (it tolerates WAL corruption and self-heals), so the
	// narrow diagnosis seam supplies the state while the full quiescence
	// gate and output mapping run for real.
	root := newDoctorRoot(t)
	writeLedgerFixture(t, root)
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.WALInconsistent, ReasonCode: "wal_inconsistent", Message: "WAL/SHM is inconsistent with the ledger database"}, nil
	})

	report := Run(context.Background(), doctorOptions(root))

	c := findCheck(report, "ledger")
	if c == nil || c.Status != Fail {
		t.Fatalf("expected fail for wal_inconsistent, got %+v", c)
	}
	details := ledgerDetails(t, report)
	if details.ReasonCode != "wal_inconsistent" || details.State != "wal_inconsistent" {
		t.Fatalf("unexpected classification: %v / %v", details.ReasonCode, details.State)
	}
	if !details.DiagnosisPerformed || !details.QuiescenceProved {
		t.Fatalf("WAL-inconsistent diagnosis must be performed under quiescence: %+v", details)
	}
	if strings.Contains(strings.ToLower(details.RecommendedAction), "restore") == false {
		t.Fatalf("expected restore guidance: %v", details.RecommendedAction)
	}
	if *calls != 1 {
		t.Fatalf("expected exactly one diagnosis call, got %d", *calls)
	}
}

func TestDoctorStorageIOFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission failures cannot be simulated")
	}
	root := newDoctorRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ledgerPath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ledgerPath, 0o600)
	report := Run(context.Background(), doctorOptions(root))

	details := ledgerDetails(t, report)
	if details.ReasonCode != "storage_io_failure" || details.State != "storage_io_failure" {
		t.Fatalf("expected storage_io_failure, got %v / %v", details.ReasonCode, details.State)
	}
	if details.IntegrityStatus != "fail" {
		t.Fatalf("unexpected integrity_status: %v", details.IntegrityStatus)
	}
	if !details.DiagnosisPerformed || !details.QuiescenceProved {
		t.Fatalf("storage I/O failure must still be diagnosed under quiescence: %+v", details)
	}
}

func TestDoctorLiveDaemonDefersDiagnosis(t *testing.T) {
	root := newDoctorRoot(t)
	path := writeLedgerFixture(t, root)
	dbHash := sha256File(t, path)
	serveHealthSocket(t, filepath.Join(root, "futurediff.sock"))
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	report := Run(context.Background(), doctorOptions(root))

	dc := findCheck(report, "daemon")
	if dc == nil || dc.Status != Pass {
		t.Fatalf("daemon check must pass with a live health socket: %+v", dc)
	}
	c := findCheck(report, "ledger")
	if c == nil || c.Status != Warn {
		t.Fatalf("live daemon must defer diagnosis (warn), got %+v", c)
	}
	details := ledgerDetails(t, report)
	if details.ReasonCode != "diagnosis_inconclusive" || details.State != "diagnosis_inconclusive" {
		t.Fatalf("live daemon must be inconclusive: %v / %v", details.ReasonCode, details.State)
	}
	if details.DiagnosisPerformed || details.QuiescenceProved {
		t.Fatalf("live daemon must not diagnose: %+v", details)
	}
	if details.DaemonStatus != "reachable" {
		t.Fatalf("expected daemon_status reachable: %v", details.DaemonStatus)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran behind a live daemon (%d calls)", *calls)
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("ledger bytes changed behind a live daemon")
	}
}

func TestDoctorLiveLockOwnerDefersDiagnosis(t *testing.T) {
	// A held lock whose recorded owner is alive and whose socket answers
	// must prevent offline diagnosis even when the health probe fails.
	root := newDoctorRoot(t)
	writeLedgerFixture(t, root)
	socketPath := filepath.Join(root, "futurediff.sock")
	serveRefusingSocket(t, socketPath)
	lockPath := filepath.Join(root, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	report := Run(context.Background(), doctorOptions(root))

	details := ledgerDetails(t, report)
	if details.DiagnosisPerformed || details.QuiescenceProved {
		t.Fatalf("live lock owner must not be diagnosed: %+v", details)
	}
	if details.DaemonStatus != "reachable" {
		t.Fatalf("expected reachable per live lock owner: %v", details.DaemonStatus)
	}
	if details.State != "diagnosis_inconclusive" {
		t.Fatalf("expected diagnosis_inconclusive: %v", details.State)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran despite a live lock owner (%d calls)", *calls)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("doctor must not remove a live lock")
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatal("doctor must not remove a live socket")
	}
}

func TestDoctorAmbiguousLockOwnershipFailsClosed(t *testing.T) {
	// A held lock whose owner cannot be confirmed (no reachable socket)
	// must fail closed without touching the ledger.
	root := newDoctorRoot(t)
	writeLedgerFixture(t, root)
	lockPath := filepath.Join(root, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	report := Run(context.Background(), doctorOptions(root))

	details := ledgerDetails(t, report)
	if details.ReasonCode != "lock_owner_ambiguous" || details.State != "diagnosis_inconclusive" {
		t.Fatalf("expected lock_owner_ambiguous/diagnosis_inconclusive: %v / %v", details.ReasonCode, details.State)
	}
	if details.QuiescenceProved || details.DiagnosisPerformed {
		t.Fatalf("ambiguous ownership must fail closed: %+v", details)
	}
	if details.DaemonStatus != "ambiguous" {
		t.Fatalf("expected daemon_status ambiguous: %v", details.DaemonStatus)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran despite ambiguous ownership (%d calls)", *calls)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("doctor must not remove an ambiguous lock")
	}
}

func TestDoctorMalformedLockFailsClosed(t *testing.T) {
	root := newDoctorRoot(t)
	writeLedgerFixture(t, root)
	lockPath := filepath.Join(root, "daemon.lock")
	if err := os.WriteFile(lockPath, []byte("{corrupt json"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	report := Run(context.Background(), doctorOptions(root))

	details := ledgerDetails(t, report)
	if details.ReasonCode != "lock_invalid_json" || details.State != "diagnosis_inconclusive" {
		t.Fatalf("expected lock_invalid_json/diagnosis_inconclusive: %v / %v", details.ReasonCode, details.State)
	}
	if details.DiagnosisPerformed || details.QuiescenceProved {
		t.Fatalf("unreadable lock must fail closed: %+v", details)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran despite an unreadable lock (%d calls)", *calls)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("doctor must not remove a corrupt lock")
	}
}

func TestDoctorStaleSocketWithoutListenerIsQuiescent(t *testing.T) {
	// A stale socket file with no listener is not a liveness signal: the
	// doctor may diagnose, and must leave the socket file in place.
	root := newDoctorRoot(t)
	path := writeLedgerFixture(t, root)
	socketPath := filepath.Join(root, "futurediff.sock")
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	dbHash := sha256File(t, path)
	report := Run(context.Background(), doctorOptions(root))

	details := ledgerDetails(t, report)
	if !details.DiagnosisPerformed || !details.QuiescenceProved {
		t.Fatalf("stale socket must not block diagnosis: %+v", details)
	}
	if details.State != "healthy" {
		t.Fatalf("expected healthy, got %v (%v)", details.State, details.Message)
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("ledger bytes changed")
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatal("doctor must not remove a socket")
	}
	if _, err := os.Lstat(filepath.Join(root, "daemon.lock")); !os.IsNotExist(err) {
		t.Fatal("doctor must not create a lock file")
	}
}

func TestDoctorSocketReachableDuringRevalidationFailsClosed(t *testing.T) {
	// The socket becomes reachable between the first probe and the
	// revalidation: the doctor must detect it via the deterministic hook
	// and refuse to diagnose.
	root := newDoctorRoot(t)
	path := writeLedgerFixture(t, root)
	dbHash := sha256File(t, path)

	oldHook := testRevalidationHook
	var listener net.Listener
	testRevalidationHook = func(socketPath string) {
		var err error
		listener, err = net.Listen("unix", socketPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		testRevalidationHook = oldHook
		if listener != nil {
			_ = listener.Close()
		}
	})

	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	report := Run(context.Background(), doctorOptions(root))

	details := ledgerDetails(t, report)
	if details.DiagnosisPerformed || details.QuiescenceProved {
		t.Fatalf("socket appearing during revalidation must fail closed: %+v", details)
	}
	if details.State != "diagnosis_inconclusive" {
		t.Fatalf("expected diagnosis_inconclusive: %v", details.State)
	}
	if details.ReasonCode != "daemon_unavailable" {
		t.Fatalf("expected daemon_unavailable: %v", details.ReasonCode)
	}
	if details.DaemonStatus != "unavailable" {
		t.Fatalf("expected daemon_status unavailable: %v", details.DaemonStatus)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran after the socket became reachable (%d calls)", *calls)
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("ledger bytes changed")
	}
}

func TestDoctorJSONStructuredFields(t *testing.T) {
	root := newDoctorRoot(t)
	writeLedgerFixture(t, root)
	report := Run(context.Background(), doctorOptions(root))

	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("report is not a single JSON document: %v", err)
	}
	var ledgerObj map[string]any
	for _, c := range decoded["checks"].([]any) {
		item := c.(map[string]any)
		if item["name"] == "ledger" {
			ledgerObj, _ = item["details"].(map[string]any)
		}
	}
	if ledgerObj == nil {
		t.Fatal("missing ledger details in JSON output")
	}
	for _, field := range []string{"reason_code", "state", "integrity_status", "diagnosis_performed", "quiescence_proved", "daemon_status", "recommended_action"} {
		if _, ok := ledgerObj[field]; !ok {
			t.Fatalf("ledger JSON missing %q: %v", field, ledgerObj)
		}
	}
}

func TestDoctorHumanReadableGuidance(t *testing.T) {
	// Corrupt ledger: the check message and the recommendation are
	// human-readable and actionable.
	root := newDoctorRoot(t)
	ledgerPath := filepath.Join(root, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	report := Run(context.Background(), doctorOptions(root))
	c := findCheck(report, "ledger")
	if c == nil || c.Status != Fail {
		t.Fatalf("expected fail, got %+v", c)
	}
	if c.Message == "" {
		t.Fatalf("human message must be non-empty for a corrupt ledger")
	}

	details := ledgerDetails(t, report)
	if !strings.Contains(strings.ToLower(details.RecommendedAction), "restore") {
		t.Fatalf("expected restore guidance: %q", details.RecommendedAction)
	}

	// Fresh root: clear initialization guidance.
	root2 := newDoctorRoot(t)
	report2 := Run(context.Background(), doctorOptions(root2))
	details2 := ledgerDetails(t, report2)
	if !strings.Contains(strings.ToLower(details2.RecommendedAction), "initialize") {
		t.Fatalf("expected initialize guidance: %q", details2.RecommendedAction)
	}

	// Live daemon: clear stop-and-rerun guidance.
	root3 := newDoctorRoot(t)
	writeLedgerFixture(t, root3)
	serveHealthSocket(t, filepath.Join(root3, "futurediff.sock"))
	report3 := Run(context.Background(), doctorOptions(root3))
	details3 := ledgerDetails(t, report3)
	if !strings.Contains(strings.ToLower(details3.RecommendedAction), "stop the daemon") {
		t.Fatalf("expected stop-the-daemon guidance: %q", details3.RecommendedAction)
	}
}

func TestDoctorLedgerOutputLeaksNoSecretsOrPaths(t *testing.T) {
	root := newDoctorRoot(t)
	writeLedgerFixture(t, root)
	report := Run(context.Background(), doctorOptions(root))

	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	j := string(b)
	for _, forbidden := range []string{"SUPER-SECRET", root, "ledger.db", "GITHUB_TOKEN", "FDIF_HOME"} {
		if strings.Contains(j, forbidden) {
			t.Fatalf("doctor output must not expose %q: %s", forbidden, j)
		}
	}
}

func TestDoctorIdempotentRepeatedDiagnosis(t *testing.T) {
	root := newDoctorRoot(t)
	path := writeLedgerFixture(t, root)
	dbHash := sha256File(t, path)
	entriesBefore := dirEntrySet(t, root)

	first := Run(context.Background(), doctorOptions(root))
	second := Run(context.Background(), doctorOptions(root))

	d1 := ledgerDetails(t, first)
	d2 := ledgerDetails(t, second)
	if d1.State != "healthy" || d2.State != "healthy" {
		t.Fatalf("classification drifted across runs: %v then %v", d1.State, d2.State)
	}
	if d1.ReasonCode != d2.ReasonCode || d1.IntegrityStatus != d2.IntegrityStatus {
		t.Fatalf("result drifted across runs: %+v then %+v", d1, d2)
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("ledger bytes changed across runs")
	}
	entriesAfter := dirEntrySet(t, root)
	if len(entriesAfter) != len(entriesBefore) {
		t.Fatalf("repeated diagnosis created files: %v", entriesAfter)
	}
}

func TestDoctorMappingCoversEveryDiagnosisState(t *testing.T) {
	cases := []struct {
		state       ledger.DiagnosisState
		reasonCode  string
		status      string
		daemonState string
	}{
		{state: ledger.Healthy, reasonCode: "healthy", status: "pass", daemonState: "stopped"},
		{state: ledger.DatabaseNotInitialized, reasonCode: "database_not_initialized", status: "warn", daemonState: "stopped"},
		{state: ledger.LedgerCorrupt, reasonCode: "ledger_corrupt", status: "fail", daemonState: "stopped"},
		{state: ledger.LedgerIntegrityFailed, reasonCode: "ledger_integrity_failed", status: "fail", daemonState: "stopped"},
		{state: ledger.WALInconsistent, reasonCode: "wal_inconsistent", status: "fail", daemonState: "stopped"},
		{state: ledger.StorageIOFailure, reasonCode: "storage_io_failure", status: "fail", daemonState: "stopped"},
		{state: ledger.DiagnosisInconclusive, reasonCode: "diagnosis_inconclusive", status: "warn", daemonState: "stopped"},
	}
	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			d := ledger.Diagnosis{State: tc.state, ReasonCode: tc.reasonCode, Message: "detail"}
			result := diagnosisToLedgerResult(d)
			if result.ReasonCode != tc.reasonCode || result.State != string(tc.state) {
				t.Fatalf("classification mismatch: %v / %v", result.ReasonCode, result.State)
			}
			if result.IntegrityStatus != tc.status {
				t.Fatalf("status mismatch: got %s want %s", result.IntegrityStatus, tc.status)
			}
			if !result.DiagnosisPerformed || !result.QuiescenceProved {
				t.Fatalf("performed/quiescence mismatch: %+v", result)
			}
			if result.DaemonStatus != tc.daemonState {
				t.Fatalf("daemon_status mismatch: got %s want %s", result.DaemonStatus, tc.daemonState)
			}
		})
	}
}

func TestDoctorQuiescenceAssessmentFailCloses(t *testing.T) {
	cases := []struct {
		name         string
		probe        quiescenceProbe
		quiescent    bool
		reasonCode   string
		daemonStatus string
	}{
		{name: "no lock and socket silent", probe: quiescenceProbe{lockStatus: daemonlock.Status{OwnerStatus: "dead", LockStatus: "released", ReasonCode: "no_lock"}, lockErr: nil, socketReached: false}, quiescent: true, daemonStatus: "stopped"},
		{name: "stale lock candidate and socket silent", probe: quiescenceProbe{lockStatus: daemonlock.Status{OwnerStatus: "proved_stale", LockStatus: "released", ReasonCode: "stale_lock_candidate"}, lockErr: nil, socketReached: false}, quiescent: true, daemonStatus: "stopped"},
		{name: "inspect error", probe: quiescenceProbe{lockStatus: daemonlock.Status{ReasonCode: "lock_invalid_json"}, lockErr: errors.New("bad json"), socketReached: false}, quiescent: false, reasonCode: "lock_invalid_json", daemonStatus: "unavailable"},
		{name: "owner alive", probe: quiescenceProbe{lockStatus: daemonlock.Status{Held: true, OwnerStatus: "alive", LockStatus: "held", DaemonReachable: true}, lockErr: nil, socketReached: true}, quiescent: false, reasonCode: "diagnosis_inconclusive", daemonStatus: "reachable"},
		{name: "owner ambiguous", probe: quiescenceProbe{lockStatus: daemonlock.Status{Held: true, OwnerStatus: "ambiguous", LockStatus: "held", ReasonCode: "lock_owner_ambiguous"}, lockErr: nil, socketReached: false}, quiescent: false, reasonCode: "lock_owner_ambiguous", daemonStatus: "ambiguous"},
		{name: "held by unknown process", probe: quiescenceProbe{lockStatus: daemonlock.Status{Held: true, OwnerStatus: "dead", LockStatus: "held"}, lockErr: nil, socketReached: false}, quiescent: false, reasonCode: "lock_owner_ambiguous", daemonStatus: "ambiguous"},
		{name: "socket answered without health", probe: quiescenceProbe{lockStatus: daemonlock.Status{OwnerStatus: "dead", LockStatus: "released", ReasonCode: "no_lock"}, lockErr: nil, socketReached: true}, quiescent: false, reasonCode: "daemon_unavailable", daemonStatus: "unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outcome := assessLedgerQuiescence(tc.probe)
			if outcome.quiescent != tc.quiescent {
				t.Fatalf("quiescent = %v, want %v", outcome.quiescent, tc.quiescent)
			}
			if !tc.quiescent {
				if outcome.reasonCode != tc.reasonCode || outcome.daemonStatus != tc.daemonStatus {
					t.Fatalf("outcome mismatch: %+v", outcome)
				}
			}
		})
	}
}

func assertNoSidecars(t *testing.T, root string) {
	t.Helper()
	entries := dirEntrySet(t, root)
	for _, forbidden := range []string{"ledger.db-wal", "ledger.db-shm", "daemon.lock", "futurediff.sock"} {
		if entries[forbidden] {
			t.Fatalf("doctor created %q", forbidden)
		}
	}
}
