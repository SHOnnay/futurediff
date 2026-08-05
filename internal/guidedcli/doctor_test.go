package guidedcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

// newDoctorHome returns an isolated short home directory. Unix socket paths
// are length-limited on macOS (104 bytes), so the home must be short enough
// for home/futurediff.sock to fit.
func newDoctorHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("", "fdd-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	return home
}

// newDoctorApp builds a fdif doctor app bound to home. healthErr makes the
// daemon health probe fail (a stopped daemon) when non-nil.
func newDoctorApp(t *testing.T, home string, healthErr error) (*App, *bytes.Buffer, *bytes.Buffer) {
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
		Daemon:   DaemonManager{Engine: &fakeEngine{healthErr: healthErr}, Socket: paths.Socket.Path},
	}
	return app, out, errOut
}

func runDoctor(t *testing.T, app *App) map[string]any {
	t.Helper()
	if code := app.Run(context.Background(), []string{"doctor"}); code != 0 {
		t.Fatalf("doctor exit code %d: %s", code, app.Err.(*bytes.Buffer).String())
	}
	var raw map[string]any
	if err := json.Unmarshal(app.Out.(*bytes.Buffer).Bytes(), &raw); err != nil {
		t.Fatalf("doctor output is not JSON: %s", app.Out.(*bytes.Buffer).String())
	}
	return raw
}

func ledgerFrom(raw map[string]any) map[string]any {
	ledgerObj, _ := raw["ledger"].(map[string]any)
	return ledgerObj
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

func writeDoctorLedger(t *testing.T, home string) string {
	t.Helper()
	path := filepath.Join(home, "ledger.db")
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

// writeWALLedgerAtRest copies a live WAL-mode ledger (db+wal+shm) into home
// while the connection is still open: the copies are byte-identical to a
// crashed daemon's ledger with uncheckpointed frames at rest.
func writeWALLedgerAtRest(t *testing.T, home string) string {
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
		if err := os.WriteFile(filepath.Join(home, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, "ledger.db")
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

// assertNoSidecarsCreated fails if doctor left any ledger/daemon artifact
// behind in home.
func assertNoSidecarsCreated(t *testing.T, home string) {
	t.Helper()
	entries := dirEntrySet(t, home)
	for _, forbidden := range []string{"ledger.db-wal", "ledger.db-shm", "daemon.lock", "futurediff.sock"} {
		if entries[forbidden] {
			t.Fatalf("doctor created %q", forbidden)
		}
	}
}

func TestDoctor_MissingLedgerIsNotInitialized(t *testing.T) {
	home := newDoctorHome(t)
	app, _, _ := newDoctorApp(t, home, nil)
	raw := runDoctor(t, app)

	status, detail, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatalf("missing ledger_integrity check; checks=%v", raw["checks"])
	}
	if status != "warn" {
		t.Fatalf("expected warn for missing ledger, got %s (%s)", status, detail)
	}
	obj := ledgerFrom(raw)
	if obj["reason_code"] != "database_not_initialized" || obj["state"] != "database_not_initialized" {
		t.Fatalf("unexpected classification: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["diagnosis_performed"] != false || obj["quiescence_proved"] != false {
		t.Fatalf("missing ledger must not perform a diagnosis: %v", obj)
	}
	// The doctor must never create the ledger or any sidecar for a missing
	// ledger, even while the daemon probe is unanswered.
	assertNoSidecarsCreated(t, home)
	entries := dirEntrySet(t, home)
	if entries["ledger.db"] {
		t.Fatal("doctor created ledger.db for a missing ledger")
	}
}

func TestDoctor_StoppedDaemonHealthyLedger(t *testing.T) {
	home := newDoctorHome(t)
	path := writeDoctorLedger(t, home)
	before := sha256File(t, path)
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	status, detail, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatal("missing ledger_integrity check")
	}
	if status != "pass" {
		t.Fatalf("expected pass, got %s (%s)", status, detail)
	}
	obj := ledgerFrom(raw)
	if obj["reason_code"] != "healthy" || obj["state"] != "healthy" {
		t.Fatalf("unexpected classification: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["integrity_status"] != "pass" {
		t.Fatalf("unexpected integrity_status: %v", obj["integrity_status"])
	}
	if obj["diagnosis_performed"] != true || obj["quiescence_proved"] != true {
		t.Fatalf("expected a performed, quiescence-proved diagnosis: %v", obj)
	}
	if obj["daemon_status"] != "stopped" {
		t.Fatalf("expected daemon_status stopped: %v", obj["daemon_status"])
	}
	if got := sha256File(t, path); got != before {
		t.Fatal("ledger bytes changed")
	}
	assertNoSidecarsCreated(t, home)
}

func TestDoctor_StoppedDaemonWALLedgerAtRest(t *testing.T) {
	home := newDoctorHome(t)
	path := writeWALLedgerAtRest(t, home)
	dbHash := sha256File(t, path)
	walHash := sha256File(t, path+"-wal")
	shmHash := sha256File(t, path+"-shm")
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["state"] != "healthy" {
		t.Fatalf("expected healthy with a live WAL at rest, got %v (%v)", obj["state"], obj["message"])
	}
	if obj["wal_present"] != true || obj["shm_present"] != true {
		t.Fatalf("sidecar flags must be reported: %v", obj)
	}
	if obj["diagnosis_performed"] != true || obj["quiescence_proved"] != true {
		t.Fatalf("expected a performed diagnosis: %v", obj)
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

func TestDoctor_StoppedDaemonCorruptLedgerIsFailure(t *testing.T) {
	home := newDoctorHome(t)
	ledgerPath := filepath.Join(home, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256File(t, ledgerPath)
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	status, detail, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatal("missing ledger_integrity check")
	}
	if status != "fail" {
		t.Fatalf("expected fail for corrupt ledger, got %s (%s)", status, detail)
	}
	obj := ledgerFrom(raw)
	if obj["reason_code"] != "ledger_corrupt" || obj["state"] != "ledger_corrupt" {
		t.Fatalf("unexpected classification: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["diagnosis_performed"] != true || obj["quiescence_proved"] != true {
		t.Fatalf("corrupt ledger must still be diagnosed under quiescence: %v", obj)
	}
	if obj["integrity_status"] != "fail" {
		t.Fatalf("expected fail integrity_status: %v", obj["integrity_status"])
	}
	if got := sha256File(t, ledgerPath); got != before {
		t.Fatal("corrupt ledger bytes were modified")
	}
	assertNoSidecarsCreated(t, home)
}

func TestDoctor_StoppedDaemonIntegrityFailureIsFailure(t *testing.T) {
	home := newDoctorHome(t)
	path := writeDoctorLedger(t, home)
	corruptMiddlePage(t, path)
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	status, _, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatal("missing ledger_integrity check")
	}
	if status != "fail" {
		t.Fatalf("expected fail for integrity failure, got %s", status)
	}
	obj := ledgerFrom(raw)
	if obj["reason_code"] != "ledger_integrity_failed" || obj["state"] != "ledger_integrity_failed" {
		t.Fatalf("unexpected classification: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["quick_check_ok"] != false && obj["quick_check_ok"] != nil {
		t.Fatalf("quick_check_ok must be false or omitted: %v", obj["quick_check_ok"])
	}
}

func TestDoctor_StoppedDaemonWALInconsistentIsFailure(t *testing.T) {
	// wal_inconsistent cannot be produced from a real file set with the
	// current SQLite (it tolerates WAL corruption and self-heals), so the
	// narrow diagnosis seam supplies the state while the full quiescence
	// gate and output mapping run for real.
	home := newDoctorHome(t)
	writeDoctorLedger(t, home)
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.WALInconsistent, ReasonCode: "wal_inconsistent", Message: "WAL/SHM is inconsistent with the ledger database"}, nil
	})

	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	status, _, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatal("missing ledger_integrity check")
	}
	if status != "fail" {
		t.Fatalf("expected fail for wal_inconsistent, got %s", status)
	}
	obj := ledgerFrom(raw)
	if obj["reason_code"] != "wal_inconsistent" || obj["state"] != "wal_inconsistent" {
		t.Fatalf("unexpected classification: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["diagnosis_performed"] != true || obj["quiescence_proved"] != true {
		t.Fatalf("WAL-inconsistent diagnosis must be performed under quiescence: %v", obj)
	}
	if *calls != 1 {
		t.Fatalf("expected exactly one diagnosis call, got %d", *calls)
	}
}

func TestDoctor_StoppedDaemonStorageIOFailureIsFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission failures cannot be simulated")
	}
	home := newDoctorHome(t)
	ledgerPath := filepath.Join(home, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ledgerPath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(ledgerPath, 0o600)
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["reason_code"] != "storage_io_failure" {
		t.Fatalf("expected storage_io_failure, got %v", obj["reason_code"])
	}
	if obj["state"] != "storage_io_failure" || obj["integrity_status"] != "fail" {
		t.Fatalf("unexpected classification: %v / %v", obj["state"], obj["integrity_status"])
	}
	if obj["diagnosis_performed"] != true || obj["quiescence_proved"] != true {
		t.Fatalf("storage I/O failure must still be diagnosed under quiescence: %v", obj)
	}
}

func TestDoctor_LiveDaemonDefersDiagnosis(t *testing.T) {
	home := newDoctorHome(t)
	path := writeDoctorLedger(t, home)
	before := sha256File(t, path)
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	// healthErr is nil: the daemon health probe succeeds.
	app, _, _ := newDoctorApp(t, home, nil)
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["reason_code"] != "diagnosis_inconclusive" || obj["state"] != "diagnosis_inconclusive" {
		t.Fatalf("live daemon must be inconclusive: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["diagnosis_performed"] != false || obj["quiescence_proved"] != false {
		t.Fatalf("live daemon must not diagnose: %v", obj)
	}
	if obj["daemon_status"] != "reachable" {
		t.Fatalf("expected daemon_status reachable: %v", obj["daemon_status"])
	}
	status, _, ok := checkStatus(raw, "ledger_integrity")
	if !ok {
		t.Fatal("missing ledger_integrity check")
	}
	if status != "warn" {
		t.Fatalf("inconclusive must be warn, got %s", status)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran behind a live daemon (%d calls)", *calls)
	}
	if got := sha256File(t, path); got != before {
		t.Fatal("ledger bytes changed behind a live daemon")
	}
}

func TestDoctor_LiveLockOwnerDefersDiagnosis(t *testing.T) {
	// A held lock whose recorded owner is alive and whose socket answers
	// must prevent offline diagnosis even when the health probe fails.
	home := newDoctorHome(t)
	writeDoctorLedger(t, home)
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

	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["diagnosis_performed"] != false || obj["quiescence_proved"] != false {
		t.Fatalf("live lock owner must not be diagnosed: %v", obj)
	}
	if obj["daemon_status"] != "reachable" {
		t.Fatalf("expected reachable per live lock owner: %v", obj["daemon_status"])
	}
	if obj["state"] != "diagnosis_inconclusive" {
		t.Fatalf("expected diagnosis_inconclusive: %v", obj["state"])
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

func TestDoctor_AmbiguousLockOwnerFailsClosed(t *testing.T) {
	// A held lock whose owner cannot be confirmed (no reachable socket)
	// must fail closed without touching the ledger.
	home := newDoctorHome(t)
	writeDoctorLedger(t, home)
	lockPath := filepath.Join(home, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["reason_code"] != "lock_owner_ambiguous" || obj["state"] != "diagnosis_inconclusive" {
		t.Fatalf("expected lock_owner_ambiguous/diagnosis_inconclusive: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["quiescence_proved"] != false || obj["diagnosis_performed"] != false {
		t.Fatalf("ambiguous ownership must fail closed: %v", obj)
	}
	if obj["daemon_status"] != "ambiguous" {
		t.Fatalf("expected daemon_status ambiguous: %v", obj["daemon_status"])
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran despite ambiguous ownership (%d calls)", *calls)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("doctor must not remove an ambiguous lock")
	}
}

func TestDoctor_CorruptLockFailsClosed(t *testing.T) {
	home := newDoctorHome(t)
	writeDoctorLedger(t, home)
	lockPath := filepath.Join(home, "daemon.lock")
	if err := os.WriteFile(lockPath, []byte("{corrupt json"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := setDoctorDiagnose(t, func(path string, opts ledger.DiagnoseOptions) (ledger.Diagnosis, error) {
		return ledger.Diagnosis{State: ledger.Healthy}, nil
	})

	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["reason_code"] != "lock_invalid_json" || obj["state"] != "diagnosis_inconclusive" {
		t.Fatalf("expected lock_invalid_json/diagnosis_inconclusive: %v / %v", obj["reason_code"], obj["state"])
	}
	if obj["diagnosis_performed"] != false || obj["quiescence_proved"] != false {
		t.Fatalf("unreadable lock must fail closed: %v", obj)
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran despite an unreadable lock (%d calls)", *calls)
	}
	// The daemon_lock check still surfaces the corruption as a failure.
	status, detail, ok := checkStatus(raw, "daemon_lock")
	if !ok || status != "fail" {
		t.Fatalf("daemon_lock must fail for a corrupt lock: %s %s", status, detail)
	}
	if _, err := os.Lstat(lockPath); err != nil {
		t.Fatal("doctor must not remove a corrupt lock")
	}
}

func TestDoctor_StaleSocketWithoutListenerIsQuiescent(t *testing.T) {
	// A stale socket file with no listener is not a liveness signal: the
	// doctor may diagnose, and must leave the socket file in place.
	home := newDoctorHome(t)
	path := writeDoctorLedger(t, home)
	socketPath := filepath.Join(home, "futurediff.sock")
	if err := os.WriteFile(socketPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := sha256File(t, path)
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["diagnosis_performed"] != true || obj["quiescence_proved"] != true {
		t.Fatalf("stale socket must not block diagnosis: %v", obj)
	}
	if obj["state"] != "healthy" {
		t.Fatalf("expected healthy, got %v (%v)", obj["state"], obj["message"])
	}
	if got := sha256File(t, path); got != before {
		t.Fatal("ledger bytes changed")
	}
	if _, err := os.Lstat(socketPath); err != nil {
		t.Fatal("doctor must not remove a socket")
	}
	if _, err := os.Lstat(filepath.Join(home, "daemon.lock")); !os.IsNotExist(err) {
		t.Fatal("doctor must not create a lock file")
	}
}

func TestDoctor_SocketReachableDuringRevalidationFailsClosed(t *testing.T) {
	// The socket becomes reachable between the first probe and the
	// revalidation: the doctor must detect it via the deterministic hook and
	// refuse to diagnose.
	home := newDoctorHome(t)
	path := writeDoctorLedger(t, home)
	before := sha256File(t, path)

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

	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	if obj["diagnosis_performed"] != false || obj["quiescence_proved"] != false {
		t.Fatalf("socket appearing during revalidation must fail closed: %v", obj)
	}
	if obj["state"] != "diagnosis_inconclusive" {
		t.Fatalf("expected diagnosis_inconclusive: %v", obj["state"])
	}
	if obj["reason_code"] != "daemon_unavailable" {
		t.Fatalf("expected daemon_unavailable: %v", obj["reason_code"])
	}
	if obj["daemon_status"] != "unavailable" {
		t.Fatalf("expected daemon_status unavailable: %v", obj["daemon_status"])
	}
	if *calls != 0 {
		t.Fatalf("offline diagnosis ran after the socket became reachable (%d calls)", *calls)
	}
	if got := sha256File(t, path); got != before {
		t.Fatal("ledger bytes changed")
	}
}

func TestDoctor_JSONStableFieldsAndSingleDocument(t *testing.T) {
	home := newDoctorHome(t)
	writeDoctorLedger(t, home)
	app, out, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	obj := ledgerFrom(raw)
	required := []string{"reason_code", "state", "integrity_status", "diagnosis_performed", "quiescence_proved", "daemon_status", "recommended_action"}
	for _, field := range required {
		if _, ok := obj[field]; !ok {
			t.Fatalf("ledger object missing %q: %v", field, obj)
		}
	}
	// Single JSON document, valid, and never a prompt.
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("doctor JSON is not a single document: %v", err)
	}
	if strings.Contains(out.String(), "\"error\"") || strings.Contains(out.String(), "Type ") {
		t.Fatalf("doctor JSON contains error/prompt artifacts:\n%s", out.String())
	}
}

func TestDoctor_HumanOutputGuidesWithoutPrompting(t *testing.T) {
	home := newDoctorHome(t)
	writeDoctorLedger(t, home)
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	app.JSON = false
	if code := app.Run(context.Background(), []string{"doctor"}); code != 0 {
		t.Fatalf("doctor exit code %d", code)
	}
	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "ledger_integrity") || !strings.Contains(out, "OK") {
		t.Fatalf("human output missing ledger line:\n%s", out)
	}
	if strings.Contains(out, "Type ") {
		t.Fatalf("human output prompts:\n%s", out)
	}

	// A corrupt ledger must surface an actionable next step.
	home2 := newDoctorHome(t)
	ledgerPath := filepath.Join(home2, "ledger.db")
	if err := os.WriteFile(ledgerPath, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	app2, _, _ := newDoctorApp(t, home2, errors.New("connection refused"))
	app2.JSON = false
	if code := app2.Run(context.Background(), []string{"doctor"}); code != 0 {
		t.Fatalf("doctor exit code %d", code)
	}
	out2 := app2.Out.(*bytes.Buffer).String()
	if !strings.Contains(out2, "Next") || !strings.Contains(out2, "restore") {
		t.Fatalf("human output missing actionable guidance:\n%s", out2)
	}
}

func TestDoctor_LedgerOutputLeaksNoSecretsOrPaths(t *testing.T) {
	home := newDoctorHome(t)
	path := writeDoctorLedger(t, home)
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)

	b, err := json.Marshal(ledgerFrom(raw))
	if err != nil {
		t.Fatal(err)
	}
	j := string(b)
	for _, forbidden := range []string{"SUPER-SECRET", home, path, "FDIF_HOME", "GITHUB_TOKEN"} {
		if strings.Contains(j, forbidden) {
			t.Fatalf("ledger output must not expose %q: %s", forbidden, j)
		}
	}
}

func TestDoctor_MappingCoversEveryDiagnosisState(t *testing.T) {
	cases := []struct {
		state       ledger.DiagnosisState
		reasonCode  string
		status      string
		performed   bool
		daemonState string
	}{
		{state: ledger.Healthy, reasonCode: "healthy", status: "pass", performed: true, daemonState: "stopped"},
		{state: ledger.DatabaseNotInitialized, reasonCode: "database_not_initialized", status: "warn", performed: true, daemonState: "stopped"},
		{state: ledger.LedgerCorrupt, reasonCode: "ledger_corrupt", status: "fail", performed: true, daemonState: "stopped"},
		{state: ledger.LedgerIntegrityFailed, reasonCode: "ledger_integrity_failed", status: "fail", performed: true, daemonState: "stopped"},
		{state: ledger.WALInconsistent, reasonCode: "wal_inconsistent", status: "fail", performed: true, daemonState: "stopped"},
		{state: ledger.StorageIOFailure, reasonCode: "storage_io_failure", status: "fail", performed: true, daemonState: "stopped"},
		{state: ledger.DiagnosisInconclusive, reasonCode: "diagnosis_inconclusive", status: "warn", performed: true, daemonState: "stopped"},
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
			if result.DiagnosisPerformed != tc.performed || result.QuiescenceProved != true {
				t.Fatalf("performed/quiescence mismatch: %+v", result)
			}
			if result.DaemonStatus != tc.daemonState {
				t.Fatalf("daemon_status mismatch: got %s want %s", result.DaemonStatus, tc.daemonState)
			}
		})
	}
}

func TestDoctor_QuiescenceAssessmentFailCloses(t *testing.T) {
	cases := []struct {
		name         string
		probe        quiescenceProbe
		quiescent    bool
		reasonCode   string
		daemonStatus string
	}{
		{name: "no lock and socket silent", probe: quiescenceProbe{lockStatus: daemonlock.Status{OwnerStatus: "dead", LockStatus: "released", ReasonCode: "no_lock"}, lockErr: nil, socketReached: false}, quiescent: true, reasonCode: "", daemonStatus: "stopped"},
		{name: "stale lock candidate and socket silent", probe: quiescenceProbe{lockStatus: daemonlock.Status{OwnerStatus: "proved_stale", LockStatus: "released", ReasonCode: "stale_lock_candidate"}, lockErr: nil, socketReached: false}, quiescent: true, reasonCode: "", daemonStatus: "stopped"},
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

func TestDoctor_InconclusiveIsNeverHealthyOrCorrupt(t *testing.T) {
	// Every non-quiescent path renders diagnosis_inconclusive (warn), never
	// healthy (pass) or corrupt (fail).
	home := newDoctorHome(t)
	writeDoctorLedger(t, home)
	lock, err := daemonlock.Acquire(filepath.Join(home, "daemon.lock"), home, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	app, _, _ := newDoctorApp(t, home, errors.New("connection refused"))
	raw := runDoctor(t, app)
	obj := ledgerFrom(raw)
	if obj["state"] != "diagnosis_inconclusive" {
		t.Fatalf("expected diagnosis_inconclusive, got %v", obj["state"])
	}
	status, _, ok := checkStatus(raw, "ledger_integrity")
	if !ok || status != "warn" {
		t.Fatalf("inconclusive must be warn, got %s", status)
	}
}
