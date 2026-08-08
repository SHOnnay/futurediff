package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
)

// fixtureRepo opens a fresh repository. When write is true it also commits a
// transaction so the WAL holds uncheckpointed frames while the connection
// stays open (the caller must Close the returned repository).
func fixtureRepo(t *testing.T, write bool) (*Repository, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	r, err := OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if write {
		now := time.Now().UTC()
		if _, err := r.Create(CreateInput{
			Transaction: domain.Transaction{ID: "tx-diagnose", Mode: "cooperative", PolicyVersion: "p", CreatedAt: now},
			Workspace:   domain.Workspace{TransactionID: "tx-diagnose", RepositoryRoot: "/repo", GitCommonDir: "/repo/.git", BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: "/runtime/ws", ArtifactsPath: "/runtime/artifacts", DirtyPolicy: "reject", SourceStatusDigest: "d"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return r, path
}

// closeCheckpointed closes the repository after a full checkpoint and drops
// any residual sidecars, leaving a complete main file with no sidecars.
func closeCheckpointed(t *testing.T, r *Repository, path string) {
	t.Helper()
	if err := r.db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Lstat(s); err == nil {
			if err := os.Remove(s); err != nil {
				t.Fatal(err)
			}
		}
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

func dirEntries(t *testing.T, dir string) map[string]bool {
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

const diagnosePrefix = "futurediff-diagnose-"

// countDiagnoseDirs counts futurediff-diagnose-* directories inside parent
// only. It never scans the shared system temporary directory, so directories
// legitimately created by concurrently running test binaries in other
// packages can never pollute the count.
func countDiagnoseDirs(parent string) int {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return -1
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), diagnosePrefix) {
			n++
		}
	}
	return n
}

// diagnoseParent returns a test-owned parent directory and a builder that
// pins every diagnosis in the test to it, so cleanup and leak assertions are
// scoped strictly to directories this test created.
func diagnoseParent(t *testing.T) (string, func(DiagnoseOptions) DiagnoseOptions) {
	t.Helper()
	parent := t.TempDir()
	return parent, func(o DiagnoseOptions) DiagnoseOptions {
		o.SnapshotTempDir = parent
		return o
	}
}

func TestDiagnose_MissingDatabaseIsNotInitialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DatabaseNotInitialized {
		t.Fatalf("missing database must be database_not_initialized, got %s", d.State)
	}
	if d.ReasonCode != "database_not_initialized" {
		t.Fatalf("unexpected reason code %q", d.ReasonCode)
	}

	// A stray transient sidecar must not flip the classification.
	if err := os.WriteFile(path+"-wal", []byte("stray"), 0o600); err != nil {
		t.Fatal(err)
	}
	d2, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d2.State != DatabaseNotInitialized {
		t.Fatalf("missing database with stray WAL must stay database_not_initialized, got %s", d2.State)
	}
	if !d2.WALPresent {
		t.Fatal("WALPresent must reflect the stray sidecar")
	}
}

func TestDiagnose_HealthyDatabaseNoSidecars(t *testing.T) {
	r, path := fixtureRepo(t, true)
	closeCheckpointed(t, r, path)

	if _, err := os.Lstat(path + "-wal"); !os.IsNotExist(err) {
		t.Fatalf("fixture must have no WAL: %v", err)
	}
	if _, err := os.Lstat(path + "-shm"); !os.IsNotExist(err) {
		t.Fatalf("fixture must have no SHM: %v", err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("expected healthy, got %s (%s)", d.State, d.Message)
	}
	if !d.QuickCheckOK {
		t.Fatal("QuickCheckOK must be true")
	}
	if d.WALPresent || d.SHMPresent {
		t.Fatal("sidecar flags must be false")
	}
	if d.ReasonCode != "" {
		t.Fatalf("healthy must have no reason code, got %q", d.ReasonCode)
	}
}

func TestDiagnose_HealthyWALSnapshot(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	if _, err := os.Lstat(path + "-wal"); err != nil {
		t.Fatalf("fixture must have a WAL with frames: %v", err)
	}
	if _, err := os.Lstat(path + "-shm"); err != nil {
		t.Fatalf("fixture must have an SHM: %v", err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("healthy WAL snapshot must be healthy, got %s (%s)", d.State, d.Message)
	}
	if !d.WALPresent || !d.SHMPresent {
		t.Fatal("sidecar flags must reflect the snapshot")
	}
}

func TestDiagnose_WALPresentSHMAbsent(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	// Remove the transient WAL index. The WAL itself remains and must be
	// read coherently; absence of -shm is never corruption.
	if err := os.Remove(path + "-shm"); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("WAL present without SHM must not imply corruption, got %s (%s)", d.State, d.Message)
	}
	if !d.WALPresent || d.SHMPresent {
		t.Fatal("sidecar flags must reflect the snapshot")
	}
}

func TestDiagnose_SHMPresentWALAbsent(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	// Checkpoint so the main file is complete, then remove the WAL. The SHM
	// is transient WAL-index state and its presence without a WAL must not
	// be classified as corruption.
	if err := r.db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + "-wal"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path + "-shm"); err != nil {
		t.Fatalf("fixture must have an SHM: %v", err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("SHM present without WAL must not imply corruption, got %s (%s)", d.State, d.Message)
	}
	if !d.SHMPresent || d.WALPresent {
		t.Fatalf("sidecar flags must reflect the snapshot (shm=%v wal=%v)", d.SHMPresent, d.WALPresent)
	}
}

func TestDiagnose_InvalidDatabaseHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != LedgerCorrupt {
		t.Fatalf("invalid header must be ledger_corrupt, got %s (%s)", d.State, d.Message)
	}
	if d.ReasonCode != "ledger_corrupt" {
		t.Fatalf("unexpected reason code %q", d.ReasonCode)
	}
}

func TestDiagnose_TruncatedDatabase(t *testing.T) {
	r, path := fixtureRepo(t, true)
	closeCheckpointed(t, r, path)

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, fi.Size()/2); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != LedgerCorrupt {
		t.Fatalf("truncated database must be ledger_corrupt, got %s (%s)", d.State, d.Message)
	}
}

func TestDiagnose_NonRegularWALFailsClosed(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	// A directory at the WAL path is rejected during snapshot
	// establishment: the coherent-snapshot contract never lets a
	// non-regular authoritative file be copied or treated as evidence.
	// The result is diagnosis_inconclusive — never corruption, and never
	// the wal_inconsistent claim, which is deferred to the WAL
	// classification work.
	if err := os.Remove(path + "-wal"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+"-wal", 0o700); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("non-regular WAL must fail closed as diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
}

func TestDiagnose_CorruptWALContentToleratedBySQLite(t *testing.T) {
	// SQLite silently recovers from checksum-corrupt WAL frames (it treats
	// them as an empty WAL). Per the diagnosis contract, wal_inconsistent is
	// only claimed when SQLite/open/check evidence supports it — never merely
	// because a sidecar is present or looks wrong.
	r, path := fixtureRepo(t, true)
	defer r.Close()

	walPath := path + "-wal"
	b, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	garbage := make([]byte, len(b))
	for i := range garbage {
		garbage[i] = 0xFF
	}
	if err := os.WriteFile(walPath, garbage, 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State == WALInconsistent || d.State == LedgerCorrupt {
		t.Fatalf("SQLite-tolerated WAL content must not be claimed corrupt, got %s", d.State)
	}
	if d.State != Healthy {
		t.Fatalf("expected healthy per SQLite evidence, got %s (%s)", d.State, d.Message)
	}
}

func TestDiagnose_QuickCheckFailure(t *testing.T) {
	r, path := fixtureRepo(t, true)
	closeCheckpointed(t, r, path)

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

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != LedgerIntegrityFailed {
		t.Fatalf("quick_check failure must be ledger_integrity_failed, got %s (%s)", d.State, d.Message)
	}
	if d.ReasonCode != "ledger_integrity_failed" {
		t.Fatalf("unexpected reason code %q", d.ReasonCode)
	}
	if d.QuickCheckOK {
		t.Fatal("QuickCheckOK must be false")
	}
}

func TestDiagnose_PermissionFailureIsStorageIO(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; permission failures cannot be simulated")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(path, []byte("unreadable database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o600)

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if d.State != StorageIOFailure {
		t.Fatalf("permission failure must be storage_io_failure, got %s", d.State)
	}
	if err == nil {
		t.Fatal("expected a wrapped error")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("errors.Is(err, os.ErrPermission) must hold, got %v", err)
	}
	if strings.Contains(d.Message, dir) {
		t.Fatalf("message must not expose the database path: %q", d.Message)
	}
}

func TestDiagnose_OversizedIsInconclusive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(path, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Sparse file: no disk cost, still over the bounded diagnostic size.
	if err := os.Truncate(path, maxDiagnoseBytes+1); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("oversized database must be diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
	if d.ReasonCode != "diagnosis_inconclusive" {
		t.Fatalf("unexpected reason code %q", d.ReasonCode)
	}
}

func TestDiagnose_OriginalsUntouched(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	dir := filepath.Dir(path)
	beforeEntries := dirEntries(t, dir)
	dbHash := sha256File(t, path)
	walHash := sha256File(t, path+"-wal")
	shmHash := sha256File(t, path+"-shm")

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("expected healthy, got %s", d.State)
	}

	afterEntries := dirEntries(t, dir)
	if len(beforeEntries) != len(afterEntries) {
		t.Fatalf("directory entry count changed: before=%d after=%d", len(beforeEntries), len(afterEntries))
	}
	for name := range beforeEntries {
		if !afterEntries[name] {
			t.Fatalf("entry %q disappeared from the authoritative directory", name)
		}
	}
	for name := range afterEntries {
		if !beforeEntries[name] {
			t.Fatalf("diagnosis created %q beside the authoritative database", name)
		}
	}
	if got := sha256File(t, path); got != dbHash {
		t.Fatal("authoritative database bytes changed")
	}
	if got := sha256File(t, path+"-wal"); got != walHash {
		t.Fatal("authoritative WAL bytes changed")
	}
	if got := sha256File(t, path+"-shm"); got != shmHash {
		t.Fatal("authoritative SHM bytes changed")
	}
}

func TestDiagnose_RepeatedDiagnosisIsIdempotent(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	d1, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d1.State != d2.State || d1.ReasonCode != d2.ReasonCode || d1.Message != d2.Message {
		t.Fatalf("repeated diagnosis differs: %+v vs %+v", d1, d2)
	}
	if d1.State != Healthy {
		t.Fatalf("expected healthy, got %s", d1.State)
	}

	// Idempotency also holds on a corrupt fixture.
	dir := t.TempDir()
	bad := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(bad, []byte("garbage header"), 0o600); err != nil {
		t.Fatal(err)
	}
	c1, err := Diagnose(bad, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Diagnose(bad, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if c1.State != c2.State || c1.ReasonCode != c2.ReasonCode {
		t.Fatalf("repeated corrupt diagnosis differs: %+v vs %+v", c1, c2)
	}
	if c1.State != LedgerCorrupt {
		t.Fatalf("expected ledger_corrupt, got %s", c1.State)
	}
}

func TestDiagnose_TemporarySnapshotCleanup(t *testing.T) {
	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)

	r, path := fixtureRepo(t, true)
	defer r.Close()
	if _, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true})); err != nil {
		t.Fatal(err)
	}
	if _, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true})); err != nil {
		t.Fatal(err)
	}

	after := countDiagnoseDirs(parent)
	if after != before {
		t.Fatalf("diagnostic temp directories leaked: before=%d after=%d", before, after)
	}
}

func TestDiagnose_StableReasonCodes(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()
	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.ReasonCode != "" {
		t.Fatalf("healthy must have an empty reason code, got %q", d.ReasonCode)
	}

	cases := []struct {
		name   string
		mutate func(t *testing.T, p string)
		state  DiagnosisState
		code   string
	}{
		{name: "missing", mutate: func(t *testing.T, p string) {}, state: DatabaseNotInitialized, code: "database_not_initialized"},
		{name: "invalid header", mutate: func(t *testing.T, p string) { os.WriteFile(p, []byte("not a database"), 0o600) }, state: LedgerCorrupt, code: "ledger_corrupt"},
		{name: "truncated", mutate: func(t *testing.T, p string) { os.Truncate(p, 32) }, state: LedgerCorrupt, code: "ledger_corrupt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "ledger.db")
			if tc.name != "missing" {
				if err := os.WriteFile(p, []byte("SQLite format 3\x00"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Truncate(p, 4096); err != nil {
					t.Fatal(err)
				}
			}
			tc.mutate(t, p)
			d, err := Diagnose(p, DiagnoseOptions{Quiescent: true})
			if err != nil {
				t.Fatal(err)
			}
			if d.State != tc.state {
				t.Fatalf("expected %s, got %s", tc.state, d.State)
			}
			if d.ReasonCode != tc.code {
				t.Fatalf("expected reason code %q, got %q", tc.code, d.ReasonCode)
			}
		})
	}
}

func TestDiagnose_NoSensitiveDataInJSON(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	// Plant a credential-shaped value inside the ledger.
	secret := "SUPER-SECRET-TOKEN-987654321"
	if _, err := r.db.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES(999999, ?)", secret); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("expected healthy, got %s (%s)", d.State, d.Message)
	}

	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	j := string(raw)
	for _, forbidden := range []string{secret, path, filepath.Dir(path), os.TempDir()} {
		if strings.Contains(j, forbidden) {
			t.Fatalf("Diagnosis JSON must not expose %q: %s", forbidden, j)
		}
	}
}

// ---------------------------------------------------------------------------
// Snapshot-establishment contract (batch B1a): quiescence, rejection of
// symlinks and non-regular file kinds, identity/hash verification,
// set-membership checks, total snapshot bound, and cleanup on every failure
// path. The SQLite classification surface (healthy/corrupt/integrity) is
// unchanged; all of the failures below must fail closed.
// ---------------------------------------------------------------------------

// setSnapshotHook installs the narrow snapshot-copy test hook for the
// duration of one test. Tests never rely on timing; the hook fires at a
// deterministic stage of snapshot establishment.
func setSnapshotHook(t *testing.T, hook func(stage snapshotCopyStage, snap *diagnosticSnapshot)) {
	t.Helper()
	old := testSnapshotHook
	testSnapshotHook = hook
	t.Cleanup(func() { testSnapshotHook = old })
}

// assertParentClean fails the test if any futurediff-diagnose-* directory was
// created (and not removed) inside parent since the recorded count.
func assertParentClean(t *testing.T, parent string, before int) {
	t.Helper()
	if after := countDiagnoseDirs(parent); after != before {
		t.Fatalf("diagnostic temp directories leaked in %s: before=%d after=%d", parent, before, after)
	}
}

func TestDiagnose_QuiescentFalseFailsClosed(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	// Quiescent defaults to false: the caller has not proven the ledger is
	// idle, so no authoritative file may be read or copied.
	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("Quiescent=false must be diagnosis_inconclusive, got %s", d.State)
	}
	if d.ReasonCode != "diagnosis_inconclusive" {
		t.Fatalf("unexpected reason code %q", d.ReasonCode)
	}
	if d.WALPresent || d.SHMPresent {
		t.Fatal("Quiescent=false must not even inspect the sidecars")
	}
	assertParentClean(t, parent, before)

	// Even a missing database fails closed when quiescence is not asserted;
	// database_not_initialized is only claimed under the quiescent contract.
	missing := filepath.Join(t.TempDir(), "ledger.db")
	dm, err := Diagnose(missing, with(DiagnoseOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	if dm.State != DiagnosisInconclusive {
		t.Fatalf("missing database with Quiescent=false must be diagnosis_inconclusive, got %s", dm.State)
	}
}

func TestDiagnose_UnchangedOfflineDatabaseSucceeds(t *testing.T) {
	r, path := fixtureRepo(t, true)
	closeCheckpointed(t, r, path)

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("unchanged offline database must be healthy, got %s (%s)", d.State, d.Message)
	}
	if !d.QuickCheckOK {
		t.Fatal("QuickCheckOK must be true")
	}
}

func TestDiagnose_DatabaseSymlinkFailsClosed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.db")
	if err := os.WriteFile(target, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ledger.db")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	targetHash := sha256File(t, target)

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("database symlink must be diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
	if got := sha256File(t, target); got != targetHash {
		t.Fatal("symlink target must not be copied or modified")
	}
}

func TestDiagnose_WALSymlinkFailsClosed(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	dummy := filepath.Join(filepath.Dir(path), "dummy-wal")
	if err := os.WriteFile(dummy, []byte("dummy wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + "-wal"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dummy, path+"-wal"); err != nil {
		t.Fatal(err)
	}

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("WAL symlink must be diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_SHMSymlinkFailsClosed(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	dummy := filepath.Join(filepath.Dir(path), "dummy-shm")
	if err := os.WriteFile(dummy, []byte("dummy shm"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + "-shm"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dummy, path+"-shm"); err != nil {
		t.Fatal(err)
	}

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("SHM symlink must be diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_DirectoryAtAuthoritativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("directory at the database path must be diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_FIFOAtAuthoritativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}

	// The Lstat-first rejection must prevent any open of the FIFO: an open
	// would block waiting for a writer.
	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("FIFO at the database path must be diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_DatabaseReplacedDuringCopy(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyDB {
			return
		}
		replacement := filepath.Join(t.TempDir(), "replacement.db")
		if err := os.WriteFile(replacement, []byte("replacement database content"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
	})

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("database replaced during copy must be diagnosis_inconclusive, never healthy or corrupt; got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_WALReplacedDuringCopy(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyWAL {
			return
		}
		replacement := filepath.Join(t.TempDir(), "replacement-wal")
		if err := os.WriteFile(replacement, []byte("replacement wal content"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path+"-wal"); err != nil {
			t.Fatal(err)
		}
	})

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("WAL replaced during copy must be diagnosis_inconclusive, never healthy or corrupt; got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_SHMAttachedDuringCopy(t *testing.T) {
	r, path := fixtureRepo(t, true)
	closeCheckpointed(t, r, path) // no sidecars at rest

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyDB {
			return
		}
		// A transient sidecar appears mid-establishment: the authoritative
		// file set must be detected as changed.
		if err := os.WriteFile(path+"-shm", []byte("shm appeared"), 0o600); err != nil {
			t.Fatal(err)
		}
	})

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("SHM appearing during copy must be diagnosis_inconclusive, never healthy or corrupt; got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_SHMRemovedDuringCopy(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopySHM {
			return
		}
		if err := os.Remove(path + "-shm"); err != nil {
			t.Fatal(err)
		}
	})

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("SHM disappearing during copy must be diagnosis_inconclusive, never healthy or corrupt; got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_SameSizeContentModification(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyDB {
			return
		}
		// In-place, same-size write: a stat-only size check would pass.
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 0xFF
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	})

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("same-size modification during copy must be diagnosis_inconclusive, never healthy or corrupt; got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_ContentModificationWithRestoredMtime(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	origFi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	origMtime := origFi.ModTime()

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyDB {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 0xFF
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
		// Restore the modification time: only the content hash can catch
		// this change.
		if err := os.Chtimes(path, origMtime, origMtime); err != nil {
			t.Fatal(err)
		}
	})

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("modification with restored mtime must be diagnosis_inconclusive, never healthy or corrupt; got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_SourceCopyHashMismatch(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyDB {
			return
		}
		// Corrupt the snapshot copy itself: the copied bytes must not
		// diverge from the source.
		b, err := os.ReadFile(snap.dbPath)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 0xFF
		if err := os.WriteFile(snap.dbPath, b, 0o600); err != nil {
			t.Fatal(err)
		}
	})

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("source/copy hash mismatch must be diagnosis_inconclusive, never healthy or corrupt; got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_TotalSnapshotSizeBound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")
	if err := os.WriteFile(path, []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Sparse files: no disk cost. Each file is under the per-file bound
	// (maxDiagnoseBytes); together they exceed maxDiagnoseTotalBytes.
	if err := os.Truncate(path, maxDiagnoseTotalBytes-(8<<20)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path+"-wal", 16<<20); err != nil {
		t.Fatal(err)
	}

	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("snapshot over the total bound must be diagnosis_inconclusive, got %s (%s)", d.State, d.Message)
	}
	if !strings.Contains(d.Message, "total") {
		t.Fatalf("message must name the total bound: %q", d.Message)
	}
	assertParentClean(t, parent, before)
}

func TestDiagnose_TemporarySnapshotCleanupAfterFailures(t *testing.T) {
	// Failure inside the copy path (source replaced mid-copy) must remove
	// the temporary snapshot.
	r, path := fixtureRepo(t, true)
	defer r.Close()

	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyDB {
			return
		}
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		b[0] ^= 0xFF
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	})
	parent, with := diagnoseParent(t)
	before := countDiagnoseDirs(parent)
	if _, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true})); err != nil {
		t.Fatal(err)
	}
	assertParentClean(t, parent, before)

	// A rejection before the copy path (directory at the database path)
	// must not create a snapshot either.
	dir := t.TempDir()
	bad := filepath.Join(dir, "ledger.db")
	if err := os.Mkdir(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Diagnose(bad, with(DiagnoseOptions{Quiescent: true})); err != nil {
		t.Fatal(err)
	}
	assertParentClean(t, parent, before)
}

// buildIndexInconsistentDB creates a database whose unique-index content no
// longer matches its table: a single byte flip inside an indexed column makes
// PRAGMA quick_check pass while the exhaustive PRAGMA integrity_check fails.
// The craft is self-verifying: candidates are tried until the observed
// quick_check == ok and integrity_check != ok pair holds, so it stays valid
// across SQLite versions.
func buildIndexInconsistentDB(t *testing.T, path string) {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ExecScript("CREATE TABLE t(a INTEGER PRIMARY KEY, b TEXT); CREATE UNIQUE INDEX ux ON t(b);"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("INSERT INTO t(b) VALUES ")
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "('%s'),", fmt.Sprintf("value-%06d", i))
	}
	stmt := strings.TrimSuffix(sb.String(), ",")
	if _, err := db.Exec(stmt); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Checkpoint(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Lstat(sidecar); err == nil {
			if err := os.Remove(sidecar); err != nil {
				t.Fatal(err)
			}
		}
	}

	base, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pageSize := int(base[16])<<8 | int(base[17])
	if pageSize == 0 {
		pageSize = 4096
	}
	// Collect every candidate position: the digit following each "value-"
	// string inside any page of the file.
	var candidates []int
	for i := 0; i+6 < len(base); {
		idx := bytes.Index(base[i:], []byte("value-"))
		if idx < 0 {
			break
		}
		pos := i + idx
		candidates = append(candidates, pos+6)
		i = pos + 6
	}
	if len(candidates) == 0 {
		t.Fatal("no index/table value bytes found to corrupt")
	}
	for _, pos := range candidates {
		orig := base[pos]
		corrupted := make([]byte, len(base))
		copy(corrupted, base)
		corrupted[pos] = orig ^ 0xFF
		probe := filepath.Join(t.TempDir(), "probe.db")
		if err := os.WriteFile(probe, corrupted, 0o600); err != nil {
			t.Fatal(err)
		}
		quickOK, integrityOK := checkPragmas(t, probe)
		_ = os.Remove(probe)
		if quickOK && !integrityOK {
			if err := os.WriteFile(path, corrupted, 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("could not craft an index inconsistency (quick_check ok, integrity_check failing)")
}

// checkPragmas reports whether quick_check and integrity_check both report ok
// against a standalone database file.
func checkPragmas(t *testing.T, path string) (quickOK, integrityOK bool) {
	t.Helper()
	db, _, err := openDiagnostic(path)
	if err != nil {
		return false, false
	}
	defer db.Close()
	qc, _, err := db.QueryRC("PRAGMA quick_check")
	if err != nil {
		return false, false
	}
	quickOK = true
	for _, row := range qc {
		for _, v := range row {
			if fmt.Sprint(v) != "ok" {
				quickOK = false
			}
		}
	}
	ic, _, err := db.QueryRC("PRAGMA integrity_check")
	if err != nil {
		return quickOK, false
	}
	integrityOK = true
	for _, row := range ic {
		for _, v := range row {
			if fmt.Sprint(v) != "ok" {
				integrityOK = false
			}
		}
	}
	return quickOK, integrityOK
}

func TestDiagnose_FullIntegrityDetectsIndexInconsistency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.db")
	buildIndexInconsistentDB(t, path)
	before := sha256File(t, path)

	// The routine quick check alone sees this fixture as healthy: the
	// inconsistency only surfaces under the exhaustive integrity_check.
	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("quick check must see the fixture as healthy, got %s (%s)", d.State, d.Message)
	}
	if !d.QuickCheckOK {
		t.Fatal("QuickCheckOK must be true for the quick check")
	}
	if d.FullIntegrityOK {
		t.Fatal("FullIntegrityOK must be false when full integrity was not requested")
	}

	full, err := Diagnose(path, DiagnoseOptions{Quiescent: true, FullIntegrity: true})
	if err != nil {
		t.Fatal(err)
	}
	if full.State != LedgerIntegrityFailed {
		t.Fatalf("full integrity must classify the index inconsistency as ledger_integrity_failed, got %s (%s)", full.State, full.Message)
	}
	if full.ReasonCode != "ledger_integrity_failed" {
		t.Fatalf("reason code = %s, want ledger_integrity_failed", full.ReasonCode)
	}
	if !full.QuickCheckOK {
		t.Fatal("QuickCheckOK must remain true: quick_check passed, integrity_check failed")
	}
	if full.FullIntegrityOK {
		t.Fatal("FullIntegrityOK must be false: integrity_check reported errors")
	}
	if len(full.IntegrityErrors) == 0 || len(full.IntegrityErrors) > maxIntegrityErrors {
		t.Fatalf("integrity errors must be bounded to (0, %d], got %d", maxIntegrityErrors, len(full.IntegrityErrors))
	}
	if !strings.Contains(full.Message, "integrity_check") {
		t.Fatalf("message must name the failing check, got %q", full.Message)
	}

	// The authoritative files were never touched and no sidecars appeared.
	if got := sha256File(t, path); got != before {
		t.Fatal("diagnosis modified the authoritative database")
	}
	for _, sidecar := range []string{path + "-wal", path + "-shm"} {
		if _, err := os.Lstat(sidecar); err == nil {
			t.Fatalf("diagnosis created %s", sidecar)
		}
	}
}

func TestDiagnose_FullIntegrityHealthyLedgerSucceeds(t *testing.T) {
	r, path := fixtureRepo(t, true)
	closeCheckpointed(t, r, path)

	d, err := Diagnose(path, DiagnoseOptions{Quiescent: true, FullIntegrity: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("healthy ledger under full integrity must be healthy, got %s (%s)", d.State, d.Message)
	}
	if !d.QuickCheckOK {
		t.Fatal("QuickCheckOK must be true")
	}
	if !d.FullIntegrityOK {
		t.Fatal("FullIntegrityOK must be true when the full check passes")
	}
}

func TestDiagnose_FullIntegrityStillRequiresQuiescence(t *testing.T) {
	r, path := fixtureRepo(t, true)
	closeCheckpointed(t, r, path)

	d, err := Diagnose(path, DiagnoseOptions{FullIntegrity: true})
	if err != nil {
		t.Fatal(err)
	}
	if d.State != DiagnosisInconclusive {
		t.Fatalf("FullIntegrity without Quiescent must fail closed, got %s", d.State)
	}
}

// TestDiagnose_LeavesTestParentEmpty pins the snapshot to a test-owned parent
// and asserts the parent is completely empty after a normal diagnosis: every
// snapshot directory created by the diagnosis is removed on success.
func TestDiagnose_LeavesTestParentEmpty(t *testing.T) {
	parent, with := diagnoseParent(t)
	r, path := fixtureRepo(t, true)
	defer r.Close()

	if _, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true})); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("test-owned parent not empty after diagnosis: %v", names)
	}
}

// TestDiagnose_DefaultSnapshotParentIsSystemTemp verifies that leaving
// SnapshotTempDir empty keeps the production default: the private snapshot
// directory is created directly under the system temporary directory with
// 0700 permissions, and it is removed when the diagnosis finishes.
func TestDiagnose_DefaultSnapshotParentIsSystemTemp(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	var captured string
	var capturedMode os.FileMode
	setSnapshotHook(t, func(stage snapshotCopyStage, snap *diagnosticSnapshot) {
		if stage != stageAfterCopyDB {
			return
		}
		captured = snap.dir
		fi, err := os.Lstat(snap.dir)
		if err != nil {
			t.Fatal(err)
		}
		capturedMode = fi.Mode().Perm()
	})

	if _, err := Diagnose(path, DiagnoseOptions{Quiescent: true}); err != nil {
		t.Fatal(err)
	}
	if captured == "" {
		t.Fatal("snapshot hook never fired")
	}
	if filepath.Dir(captured) != filepath.Clean(os.TempDir()) {
		t.Fatalf("default snapshot parent %s, want system temp %s", filepath.Dir(captured), filepath.Clean(os.TempDir()))
	}
	if capturedMode != 0o700 {
		t.Fatalf("snapshot directory permissions %#o, want 0700", capturedMode)
	}
	if _, err := os.Lstat(captured); !os.IsNotExist(err) {
		t.Fatalf("default snapshot directory not cleaned up after diagnosis: %v", err)
	}
}

// TestDiagnose_ForeignDiagnosisDirNotCountedOrRemoved plants a
// futurediff-diagnose-* directory in the test-owned parent (as another test
// or process would legitimately create) and proves a diagnosis neither
// removes it nor trips the parent-scoped leak assertion.
func TestDiagnose_ForeignDiagnosisDirNotCountedOrRemoved(t *testing.T) {
	parent, with := diagnoseParent(t)
	foreign := filepath.Join(parent, "futurediff-diagnose-foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(foreign, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := countDiagnoseDirs(parent)
	r, path := fixtureRepo(t, true)
	defer r.Close()
	d, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true}))
	if err != nil {
		t.Fatal(err)
	}
	if d.State != Healthy {
		t.Fatalf("expected healthy, got %s (%s)", d.State, d.Message)
	}
	assertParentClean(t, parent, before)

	if _, err := os.Lstat(foreign); err != nil {
		t.Fatalf("foreign diagnosis directory was removed: %v", err)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "keep" {
		t.Fatalf("foreign diagnosis contents changed: %q %v", b, err)
	}
}

// TestDiagnose_ConcurrentSeparateParents runs concurrent diagnoses, each
// pinned to its own test-owned parent, and asserts each parent stays clean:
// a diagnosis in one parent can never interfere with another parent's leak
// assertion.
func TestDiagnose_ConcurrentSeparateParents(t *testing.T) {
	for i := 0; i < 4; i++ {
		t.Run(fmt.Sprintf("parent-%d", i), func(t *testing.T) {
			t.Parallel()
			parent, with := diagnoseParent(t)
			r, path := fixtureRepo(t, true)
			defer r.Close()

			before := countDiagnoseDirs(parent)
			if _, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true})); err != nil {
				t.Fatal(err)
			}
			assertParentClean(t, parent, before)
		})
	}
}

// TestDiagnose_ConcurrentSharedParent runs concurrent diagnoses sharing one
// test-owned parent and proves each cleans only its own children: after all
// finish, the only remaining entry is the foreign directory planted to
// represent another test's legitimate snapshot.
func TestDiagnose_ConcurrentSharedParent(t *testing.T) {
	parent := t.TempDir()
	foreign := filepath.Join(parent, "futurediff-diagnose-foreign")
	if err := os.Mkdir(foreign, 0o700); err != nil {
		t.Fatal(err)
	}
	with := func(o DiagnoseOptions) DiagnoseOptions {
		o.SnapshotTempDir = parent
		return o
	}

	for i := 0; i < 4; i++ {
		t.Run(fmt.Sprintf("child-%d", i), func(t *testing.T) {
			t.Parallel()
			r, path := fixtureRepo(t, true)
			defer r.Close()
			if _, err := Diagnose(path, with(DiagnoseOptions{Quiescent: true})); err != nil {
				t.Fatal(err)
			}
		})
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
		if strings.HasPrefix(e.Name(), diagnosePrefix) && e.Name() != "futurediff-diagnose-foreign" {
			t.Fatalf("diagnosis child directory leaked in shared parent: %s", e.Name())
		}
	}
	if len(names) != 1 {
		t.Fatalf("expected only the planted foreign directory, got %v", names)
	}
}
