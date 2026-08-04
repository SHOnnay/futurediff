package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func countDiagnosticTempDirs() int {
	entries, err := os.ReadDir(os.TempDir())
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

func TestDiagnose_MissingDatabaseIsNotInitialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.db")

	d, err := Diagnose(path)
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
	d2, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.State != LedgerCorrupt {
		t.Fatalf("truncated database must be ledger_corrupt, got %s (%s)", d.State, d.Message)
	}
}

func TestDiagnose_WALUnusableIsWALInconsistent(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()

	// Replace the WAL file with a directory. SQLite cannot open it, and that
	// open evidence — combined with a healthy database alone — classifies the
	// WAL as inconsistent.
	if err := os.Remove(path + "-wal"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+"-wal", 0o700); err != nil {
		t.Fatal(err)
	}

	d, err := Diagnose(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.State != WALInconsistent {
		t.Fatalf("unusable WAL must be wal_inconsistent, got %s (%s)", d.State, d.Message)
	}
	if d.ReasonCode != "wal_inconsistent" {
		t.Fatalf("unexpected reason code %q", d.ReasonCode)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d, err := Diagnose(path)
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

	d1, err := Diagnose(path)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Diagnose(path)
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
	c1, err := Diagnose(bad)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := Diagnose(bad)
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
	before := countDiagnosticTempDirs()

	r, path := fixtureRepo(t, true)
	defer r.Close()
	if _, err := Diagnose(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Diagnose(path); err != nil {
		t.Fatal(err)
	}

	after := countDiagnosticTempDirs()
	if after != before {
		t.Fatalf("diagnostic temp directories leaked: before=%d after=%d", before, after)
	}
}

func TestDiagnose_StableReasonCodes(t *testing.T) {
	r, path := fixtureRepo(t, true)
	defer r.Close()
	d, err := Diagnose(path)
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
			d, err := Diagnose(p)
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

	d, err := Diagnose(path)
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
