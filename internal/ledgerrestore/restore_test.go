package ledgerrestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

func addTx(t *testing.T, r *ledger.Repository, id, root string) {
	t.Helper()
	_, err := r.Create(ledger.CreateInput{Transaction: domain.Transaction{ID: id, Mode: "cooperative", PolicyVersion: "p", CreatedAt: time.Now().UTC()}, Workspace: domain.Workspace{TransactionID: id, RepositoryRoot: root, GitCommonDir: root, BaseOID: "abc", ObjectFormat: "sha1", WorkspacePath: filepath.Join(root, id, "w"), ArtifactsPath: filepath.Join(root, id, "a"), DirtyPolicy: "reject", SourceStatusDigest: "clean"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRestore(t *testing.T) {
	root, livePath, backupPath := restoreFixture(t)
	dry, err := Run(Options{LivePath: livePath, BackupPath: backupPath})
	if err != nil {
		t.Fatal(err)
	}
	// Applying a backup that predates the live ledger must be refused.
	_, err = Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, PreRestoreBackupPath: filepath.Join(root, "pre.db")})
	if err == nil {
		t.Fatal("expected refusal for stale backup")
	}
	if !strings.Contains(err.Error(), "older than the live ledger") {
		t.Fatalf("unexpected error: %v", err)
	}
	report, err := Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, PreRestoreBackupPath: filepath.Join(root, "pre.db"), AllowStaleBackup: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.PreRestoreBackup == nil {
		t.Fatalf("report=%+v", report)
	}
	restored, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	health, err := restored.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if health.TransactionCount != 1 {
		t.Fatalf("transactions=%d", health.TransactionCount)
	}
}

func TestRestore_CurrentBackupAllowed(t *testing.T) {
	// A backup that already contains every live chain is not stale and must be
	// applied without AllowStaleBackup.
	root := t.TempDir()
	livePath := filepath.Join(root, "live.db")
	live, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, live, "one", root)
	addTx(t, live, "two", root)
	backupPath := filepath.Join(root, "backup.db")
	if _, err = live.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	live.Close()
	dry, err := Run(Options{LivePath: livePath, BackupPath: backupPath})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, PreRestoreBackupPath: filepath.Join(root, "pre.db")})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("report=%+v", report)
	}
}

func TestRestore_LiveDaemonLockRefused(t *testing.T) {
	root := t.TempDir()
	livePath := filepath.Join(root, "live.db")
	live, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, live, "one", root)
	backupPath := filepath.Join(root, "backup.db")
	if _, err := live.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	dry, err := Run(Options{LivePath: livePath, BackupPath: backupPath})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(Options{LivePath: livePath, BackupPath: backupPath, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, LockPath: lockPath, AllowStaleBackup: true})
	if err == nil {
		t.Fatal("expected refusal when a live daemon holds the lock")
	}
	if !strings.Contains(err.Error(), "daemon lock is held") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Batch B2a: staged replacement, byte-for-byte preservation, and
// deterministic fault behavior of the verified restore path.
// ---------------------------------------------------------------------------

func mustSHA(t *testing.T, path string) string {
	t.Helper()
	sum, err := shaFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func corruptMiddleBytes(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 0xFF
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// restoreFixture returns an isolated data root containing a live ledger with
// two transactions and a verified backup of that same home recorded in its
// authoritative catalog with one transaction (an older backup that requires
// AllowStaleBackup to apply). The backup is the home's first backup: its
// snapshot predates its own catalog insert, so the candidate file itself
// records nothing — provenance comes from the home's catalog.
func restoreFixture(t *testing.T) (root, live, backup string) {
	t.Helper()
	root = t.TempDir()
	live = filepath.Join(root, "live.db")
	l, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "one", root)
	backup = filepath.Join(root, "backup.db")
	if _, err := l.Backup(backup); err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "two", root)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return root, live, backup
}

// walAtRestLedger builds a live ledger whose WAL and SHM files exist at rest
// with uncheckpointed frames, copied out while the writer connection is open,
// together with a backup recorded in that same home's authoritative catalog.
// The backup is created before the uncheckpointed frame is added, so it holds
// two transactions and cannot be polluted by the later wal-tx frame.
func walAtRestLedger(t *testing.T, root string) (live, backup string) {
	t.Helper()
	scratch := t.TempDir()
	path := filepath.Join(scratch, "ledger.db")
	r, err := ledger.OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, r, "one", scratch)
	addTx(t, r, "two", scratch)
	backup = filepath.Join(root, "backup.db")
	if _, err := r.Backup(backup); err != nil {
		t.Fatal(err)
	}
	addTx(t, r, "wal-tx", scratch)
	for _, name := range []string{"ledger.db", "ledger.db-wal", "ledger.db-shm"} {
		b, err := os.ReadFile(filepath.Join(scratch, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.Close()
	return filepath.Join(root, "ledger.db"), backup
}

func stagingArtifacts(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ledger-restore-") {
			names = append(names, e.Name())
		}
	}
	return names
}

// assertNoRestoreArtifacts asserts that neither staging files, quarantine
// evidence directories, nor auto-named pre-restore files remain in dir.
func assertNoRestoreArtifacts(t *testing.T, dir string) {
	t.Helper()
	if got := stagingArtifacts(t, dir); len(got) != 0 {
		t.Fatalf("staging artifacts remain: %v", got)
	}
	if got := evidenceDirs(t, dir); len(got) != 0 {
		t.Fatalf("evidence dirs remain: %v", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "ledger.pre-restore.") {
			t.Fatalf("auto-named pre-restore file remains: %s", e.Name())
		}
	}
}

func evidenceDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "ledger-restore-evidence-") {
			names = append(names, filepath.Join(dir, e.Name()))
		}
	}
	return names
}

func applyOptions(root, live, backup, sha string) Options {
	return Options{LivePath: live, BackupPath: backup, ExpectedSHA256: sha, Apply: true, Confirmation: Confirmation, AllowStaleBackup: true}
}

func TestRestore_StagedReplacementPreservesOriginalAndVerifies(t *testing.T) {
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Applied {
		t.Fatal("dry run must not apply")
	}
	if got := stagingArtifacts(t, root); len(got) != 0 {
		t.Fatalf("dry run created artifacts: %v", got)
	}
	before := mustSHA(t, live)
	backupSHA := mustSHA(t, backup)
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || !report.RestoredHealthy || report.RestoreVerification != "healthy" {
		t.Fatalf("report=%+v", report)
	}
	if report.BackupSHA256 != backupSHA {
		t.Fatalf("report backup sha %s, want %s", report.BackupSHA256, backupSHA)
	}
	if report.Preserved == nil || report.Preserved.LocationClass != "quarantine" {
		t.Fatalf("preserved=%+v", report.Preserved)
	}
	// The original ledger is preserved byte-for-byte in a private quarantine.
	q := report.Preserved.QuarantineDir
	if mustSHA(t, filepath.Join(q, "ledger.db")) != before {
		t.Fatal("quarantined ledger is not byte-identical to the pre-restore ledger")
	}
	fi, err := os.Stat(q)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("quarantine permissions %#o, want 0700", fi.Mode().Perm())
	}
	// The restored ledger is healthy and holds the backup's transaction count.
	// (Reopening a WAL-mode ledger recreates its sidecars, so the
	// sidecar-free assertion must come before this reopen.)
	restored, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	health, err := restored.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	if health.TransactionCount != 1 {
		t.Fatalf("transactions=%d, want 1", health.TransactionCount)
	}
	// The backup is never modified and no staging artifacts remain.
	if mustSHA(t, backup) != backupSHA {
		t.Fatal("backup bytes changed")
	}
	if got := stagingArtifacts(t, root); len(got) != 0 {
		t.Fatalf("staging artifacts remain after restore: %v", got)
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 retained after success", got)
	}
	// The quarantine carries a durable, self-describing evidence manifest
	// referencing the preserved ledger and the verified backup.
	manifest, err := os.ReadFile(filepath.Join(q, "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	var em struct {
		Version      int    `json:"version"`
		LedgerSHA256 string `json:"ledger_sha256"`
		BackupSHA256 string `json:"backup_sha256"`
	}
	if err := json.Unmarshal(manifest, &em); err != nil {
		t.Fatalf("evidence manifest: %v", err)
	}
	if em.Version != 1 || em.LedgerSHA256 != before || em.BackupSHA256 != backupSHA {
		t.Fatalf("evidence manifest=%+v", em)
	}
}

func TestRestore_CorruptOriginalPreservedByteForByte(t *testing.T) {
	root, live, backup := restoreFixture(t)
	corruptMiddleBytes(t, live)
	corruptSHA := mustSHA(t, live)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("report=%+v", report)
	}
	// The corrupt original is preserved byte-for-byte. Because the
	// authoritative file is never opened during the restore (the quarantine
	// and any semantic pre-restore backup work from private copies), the
	// quarantined bytes equal the corrupt original exactly.
	if mustSHA(t, filepath.Join(report.Preserved.QuarantineDir, "ledger.db")) != corruptSHA {
		t.Fatal("corrupt original not preserved byte-for-byte")
	}
	if got := stagingArtifacts(t, root); len(got) != 0 {
		t.Fatalf("staging artifacts remain: %v", got)
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want 1", got)
	}
	restored, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	health, err := restored.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	if health.TransactionCount != 1 {
		t.Fatalf("transactions=%d, want 1", health.TransactionCount)
	}
}

func TestRestore_WALAndSHMPreservedWhenPresent(t *testing.T) {
	root := t.TempDir()
	live, backup := walAtRestLedger(t, root)
	walSHA := mustSHA(t, live+"-wal")
	shmSHA := mustSHA(t, live+"-shm")
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("report=%+v", report)
	}
	p := report.Preserved
	if !p.WALPreserved || !p.SHMPreserved {
		t.Fatalf("WAL/SHM preservation flags: %+v", p)
	}
	if mustSHA(t, filepath.Join(p.QuarantineDir, "ledger.db-wal")) != walSHA {
		t.Fatal("WAL not preserved byte-for-byte")
	}
	if mustSHA(t, filepath.Join(p.QuarantineDir, "ledger.db-shm")) != shmSHA {
		t.Fatal("SHM not preserved byte-for-byte")
	}
	// The pre-restore sidecars were removed from the authoritative path after
	// preservation; they exist only in quarantine. (Asserted before the
	// reopen below, which recreates WAL-mode sidecars at rest.)
	if _, err := os.Lstat(live + "-wal"); !os.IsNotExist(err) {
		t.Fatal("stale live -wal remains after restore")
	}
	if _, err := os.Lstat(live + "-shm"); !os.IsNotExist(err) {
		t.Fatal("stale live -shm remains after restore")
	}
	// The restored ledger is healthy and holds exactly the backup's state:
	// the pre-restore WAL frames (the uncheckpointed wal-tx transaction) must
	// not replay into it.
	restored, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	health, err := restored.HealthCheck()
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	if health.TransactionCount != 2 {
		t.Fatalf("restored transactions=%d, want 2 (no stale WAL replay)", health.TransactionCount)
	}
}

func TestRestore_InvalidBackupRefused(t *testing.T) {
	root := t.TempDir()
	live := buildLedgerAt(t, root, "live.db", 2)
	garbage := filepath.Join(root, "garbage.db")
	if err := os.WriteFile(garbage, []byte("this is not a sqlite database"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	g := mustSHA(t, garbage)
	_, err := RunWithInjector(applyOptions(root, live, garbage, g), nil)
	if err == nil {
		t.Fatal("invalid backup must be refused")
	}
	if !strings.Contains(err.Error(), "backup validation") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	if got := stagingArtifacts(t, root); len(got) != 0 {
		t.Fatalf("artifacts after refusal: %v", got)
	}
	if got := evidenceDirs(t, root); len(got) != 0 {
		t.Fatalf("evidence dirs after refusal: %v", got)
	}
}

func TestRestore_BackupFromAnotherDataRootRefused(t *testing.T) {
	root, live, backup := restoreFixture(t)
	other := t.TempDir()
	otherBackup := filepath.Join(other, "backup.db")
	if err := copyFile(backup, otherBackup, 0o600); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	// Provenance is part of validation, so even the dry run refuses a backup
	// that resolves outside the data root.
	if _, err := Run(Options{LivePath: live, BackupPath: otherBackup}); err == nil {
		t.Fatal("dry run with a foreign backup must be refused")
	}
	_, err := RunWithInjector(applyOptions(root, live, otherBackup, mustSHA(t, otherBackup)), nil)
	if err == nil {
		t.Fatal("backup from another data root must be refused")
	}
	if !strings.Contains(err.Error(), "outside the FutureDiff data root") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func buildLedgerAt(t *testing.T, dir, name string, txCount int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	r, err := ledger.OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < txCount; i++ {
		addTx(t, r, fmt.Sprintf("tx-%d", i), dir)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestore_LiveSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	target := buildLedgerAt(t, root, "target.db", 2)
	live := filepath.Join(root, "live.db")
	if err := os.Symlink(target, live); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "source.db")
	s, err := ledger.OpenRepository(src)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, s, "one", root)
	backup := filepath.Join(root, "backup.db")
	if _, err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	targetBefore := mustSHA(t, target)
	_, err = RunWithInjector(applyOptions(root, live, backup, ""), nil)
	if err == nil {
		t.Fatal("symlinked live ledger must be refused")
	}
	if !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, target) != targetBefore {
		t.Fatal("symlink target changed")
	}
}

func TestRestore_BackupSymlinkRefused(t *testing.T) {
	root := t.TempDir()
	live := buildLedgerAt(t, root, "live.db", 2)
	target := buildLedgerAt(t, root, "backup-target.db", 1)
	backup := filepath.Join(root, "backup.db")
	if err := os.Symlink(target, backup); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	_, err := RunWithInjector(applyOptions(root, live, backup, ""), nil)
	if err == nil {
		t.Fatal("symlinked backup must be refused")
	}
	if !strings.Contains(err.Error(), "must be a regular file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
}

func TestRestore_SidecarSymlinkRefused(t *testing.T) {
	root, live, backup := restoreFixture(t)
	// The clean close leaves an empty WAL/SHM pair at rest; remove them so
	// the symlink occupies the sidecar path itself.
	os.Remove(live + "-wal")
	os.Remove(live + "-shm")
	dummy := filepath.Join(root, "dummy")
	if err := os.WriteFile(dummy, []byte("dummy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dummy, live+"-wal"); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	backupSHA := mustSHA(t, backup)
	// The authoritative catalog snapshot refuses to follow a symlinked
	// sidecar, so validation fails closed for the dry run too.
	_, err := Run(Options{LivePath: live, BackupPath: backup})
	if err == nil {
		t.Fatal("dry run with a symlinked sidecar must fail closed")
	}
	if !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = RunWithInjector(applyOptions(root, live, backup, backupSHA), nil)
	if err == nil {
		t.Fatal("symlinked sidecar must refuse the restore")
	}
	if !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	if mustSHA(t, backup) != backupSHA {
		t.Fatal("backup changed")
	}
	// Nothing is left behind.
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_NonRegularLiveRefused(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	if err := os.Mkdir(live, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := buildLedgerAt(t, root, "backup.db", 1)
	_, err := RunWithInjector(applyOptions(root, live, backup, ""), nil)
	if err == nil {
		t.Fatal("directory at the live path must be refused")
	}
	if !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestore_DirectoryBackupRefused(t *testing.T) {
	root := t.TempDir()
	live := buildLedgerAt(t, root, "live.db", 2)
	backup := filepath.Join(root, "backup.db")
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	_, err := RunWithInjector(applyOptions(root, live, backup, ""), nil)
	if err == nil {
		t.Fatal("directory at the backup path must be refused")
	}
	if !strings.Contains(err.Error(), "must be a regular file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
}

func TestRestore_ConfirmationAndDigestRequired(t *testing.T) {
	_, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	if _, err := RunWithInjector(Options{LivePath: live, BackupPath: backup, ExpectedSHA256: dry.BackupSHA256, Apply: true, AllowStaleBackup: true}, nil); err == nil {
		t.Fatal("apply without confirmation must be refused")
	}
	if _, err := RunWithInjector(Options{LivePath: live, BackupPath: backup, Apply: true, Confirmation: Confirmation, AllowStaleBackup: true}, nil); err == nil {
		t.Fatal("apply without expected sha256 must be refused")
	}
	if _, err := RunWithInjector(Options{LivePath: live, BackupPath: backup, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: "wrong phrase", AllowStaleBackup: true}, nil); err == nil {
		t.Fatal("wrong confirmation phrase must be refused")
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed after refused restores")
	}
}

func TestRestore_LiveSocketRefused(t *testing.T) {
	root, live, backup := restoreFixture(t)
	socket := filepath.Join(root, "futurediff.sock")
	if err := os.WriteFile(socket, []byte("stale socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunWithInjector(Options{LivePath: live, BackupPath: backup, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, SocketPath: socket, AllowStaleBackup: true}, nil)
	if err == nil {
		t.Fatal("existing daemon socket must refuse the restore")
	}
	if !strings.Contains(err.Error(), "daemon socket exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestore_AmbiguousLockRefused(t *testing.T) {
	// A held lock whose owner process is alive but whose daemon socket is
	// unreachable classifies as ambiguous; restore must fail closed.
	root, live, backup := restoreFixture(t)
	lockPath := filepath.Join(root, "daemon.lock")
	lock, err := daemonlock.Acquire(lockPath, root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	status, err := daemonlock.Inspect(lockPath, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status.OwnerStatus != "ambiguous" {
		t.Fatalf("fixture lock owner status %q, want ambiguous", status.OwnerStatus)
	}
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunWithInjector(Options{LivePath: live, BackupPath: backup, ExpectedSHA256: dry.BackupSHA256, Apply: true, Confirmation: Confirmation, LockPath: lockPath, AllowStaleBackup: true}, nil)
	if err == nil {
		t.Fatal("ambiguous daemon lock must refuse the restore")
	}
	if !strings.Contains(err.Error(), "daemon lock is held") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestore_StagingFaultsFailClosed(t *testing.T) {
	cases := []struct {
		op  string
		err error
	}{
		{durablewrite.OpCreate, syscall.ENOSPC},
		{durablewrite.OpWrite, durablewrite.ErrIO},
		{durablewrite.OpShortWrite, durablewrite.ErrDiskFull},
		{durablewrite.OpFileSync, syscall.EIO},
		{durablewrite.OpRename, syscall.EROFS},
		{durablewrite.OpDirectorySync, syscall.EDQUOT},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			root, live, backup := restoreFixture(t)
			before := mustSHA(t, live)
			backupSHA := mustSHA(t, backup)
			dry, err := Run(Options{LivePath: live, BackupPath: backup})
			if err != nil {
				t.Fatal(err)
			}
			inject := durablewrite.NewFaultMap(map[string]error{tc.op: tc.err})
			_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
			if err == nil {
				t.Fatalf("staging fault %s must fail the restore", tc.op)
			}
			if mustSHA(t, live) != before {
				t.Fatal("live ledger changed by a failed staging write")
			}
			if mustSHA(t, backup) != backupSHA {
				t.Fatal("backup changed by a failed staging write")
			}
			assertNoRestoreArtifacts(t, root)
			// Retry after the fault is removed is safe and succeeds.
			report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
			if err != nil || !report.Applied {
				t.Fatalf("retry failed: err=%v report=%+v", err, report)
			}
		})
	}
}

func TestRestore_QuarantineCreateFault(t *testing.T) {
	root, live, backup := restoreFixture(t)
	before := mustSHA(t, live)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpQuarantineCreate: syscall.ENOSPC})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("quarantine-create fault must fail the restore")
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_PreserveFaultsFailClosed(t *testing.T) {
	cases := []struct {
		name         string
		op           string
		withSidecars bool
	}{
		{"ledger", OpPreserveLedger, false},
		{"wal", OpPreserveWAL, true},
		{"shm", OpPreserveSHM, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root, live, backup string
			if tc.withSidecars {
				root = t.TempDir()
				live, backup = walAtRestLedger(t, root)
			} else {
				root, live, backup = restoreFixture(t)
			}
			files := []string{live, live + "-wal", live + "-shm"}
			before := make(map[string]string, len(files))
			for _, f := range files {
				if _, err := os.Lstat(f); err == nil {
					before[f] = mustSHA(t, f)
				}
			}
			backupSHA := mustSHA(t, backup)
			dry, err := Run(Options{LivePath: live, BackupPath: backup})
			if err != nil {
				t.Fatal(err)
			}
			inject := durablewrite.NewFaultMap(map[string]error{tc.op: durablewrite.ErrIO})
			_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
			if err == nil {
				t.Fatalf("preserve fault %s must fail the restore", tc.op)
			}
			// Original authoritative state is fully intact.
			for f, sum := range before {
				if mustSHA(t, f) != sum {
					t.Fatalf("%s changed by a failed preservation", f)
				}
			}
			if mustSHA(t, backup) != backupSHA {
				t.Fatal("backup changed")
			}
			// The partial quarantine is removed.
			assertNoRestoreArtifacts(t, root)
			// Retry after the fault is removed succeeds.
			report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
			if err != nil || !report.Applied {
				t.Fatalf("retry failed: err=%v report=%+v", err, report)
			}
		})
	}
}

func TestRestore_PublishRenameFaultRetainsQuarantine(t *testing.T) {
	root, live, backup := restoreFixture(t)
	before := mustSHA(t, live)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpPublishRename: syscall.EIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("publish-rename fault must fail the restore")
	}
	if !strings.Contains(err.Error(), "publish restored ledger") {
		t.Fatalf("unexpected error: %v", err)
	}
	// The old authoritative state is still in place.
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed before the rename")
	}
	// The completed quarantine is retained with the original bytes.
	dirs := evidenceDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 retained", dirs)
	}
	if mustSHA(t, filepath.Join(dirs[0], "ledger.db")) != before {
		t.Fatal("retained quarantine does not hold the original")
	}
	if got := stagingArtifacts(t, root); len(got) != 0 {
		t.Fatalf("staging artifacts remain after publish fault: %v", got)
	}
	// Retry after the fault is removed: the previous evidence is never
	// overwritten; a second, distinct quarantine is created.
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !report.Applied {
		t.Fatalf("retry failed: err=%v report=%+v", err, report)
	}
	dirs = evidenceDirs(t, root)
	if len(dirs) != 2 {
		t.Fatalf("evidence dirs=%v, want 2 (previous retained)", dirs)
	}
	if mustSHA(t, filepath.Join(dirs[0], "ledger.db")) != before {
		t.Fatal("first retained quarantine was overwritten")
	}
}

func TestRestore_PublishDirectorySyncFaultNoFalseSuccess(t *testing.T) {
	root, live, backup := restoreFixture(t)
	before := mustSHA(t, live)
	backupSHA := mustSHA(t, backup)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpPublishDirectorySync: syscall.EIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("publish directory-sync fault must be reported, not a false success")
	}
	if !strings.Contains(err.Error(), "publish restored ledger") {
		t.Fatalf("unexpected error: %v", err)
	}
	// The restored bytes are authoritative but success was not reported.
	if mustSHA(t, live) != backupSHA {
		t.Fatal("restored ledger bytes not in place after rename")
	}
	dirs := evidenceDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 retained", dirs)
	}
	if mustSHA(t, filepath.Join(dirs[0], "ledger.db")) != before {
		t.Fatal("retained quarantine does not hold the original")
	}
}

func TestRestore_PostVerifyFaultRetainsQuarantine(t *testing.T) {
	root, live, backup := restoreFixture(t)
	before := mustSHA(t, live)
	backupSHA := mustSHA(t, backup)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpPostVerify: durablewrite.ErrIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("post-verify fault must fail the restore")
	}
	if !strings.Contains(err.Error(), "post-restore verification") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != backupSHA {
		t.Fatal("restored ledger bytes not in place")
	}
	dirs := evidenceDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 retained", dirs)
	}
	if mustSHA(t, filepath.Join(dirs[0], "ledger.db")) != before {
		t.Fatal("retained quarantine does not hold the original")
	}
}

func TestRestore_ImmediateRepeatIsStableAlreadyRestored(t *testing.T) {
	// The repeated-invocation contract: an immediate repeat with the same
	// verified backup recognizes the already-restored ledger (byte-identical
	// to the backup), reports a stable AlreadyRestored result, creates no new
	// evidence, and never touches the ledger or the backup.
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !r1.Applied {
		t.Fatalf("first restore failed: err=%v", err)
	}
	liveAfter1 := mustSHA(t, live)
	backupSHA := mustSHA(t, backup)
	r2, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatalf("second restore failed: %v", err)
	}
	if !r2.AlreadyRestored || r2.Applied {
		t.Fatalf("repeat report=%+v, want AlreadyRestored with Applied=false", r2)
	}
	if !r2.RestoredHealthy || r2.RestoreVerification != "healthy" {
		t.Fatalf("repeat report=%+v, want healthy verification", r2)
	}
	if r2.Preserved != nil {
		t.Fatalf("already-restored repeat must not preserve again: %+v", r2.Preserved)
	}
	if mustSHA(t, live) != liveAfter1 {
		t.Fatal("live ledger changed by the repeat")
	}
	if mustSHA(t, backup) != backupSHA {
		t.Fatal("backup changed by the repeat")
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 (no duplicate evidence)", got)
	}
	if got := stagingArtifacts(t, root); len(got) != 0 {
		t.Fatalf("staging artifacts remain after repeat: %v", got)
	}
}

func TestRestore_RepeatAfterPublishSyncFaultRepairsDurability(t *testing.T) {
	// A failed parent-directory sync leaves the restored bytes authoritative
	// but not durably recorded. A repeat (with the fault removed) recognizes
	// the already-restored ledger, re-syncs the parent directory, re-verifies
	// the ledger, and creates no new evidence.
	root, live, backup := restoreFixture(t)
	backupSHA := mustSHA(t, backup)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpPublishDirectorySync: syscall.EIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("publish directory-sync fault must fail the first restore")
	}
	if mustSHA(t, live) != backupSHA {
		t.Fatal("restored bytes not in place after the faulted publish")
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 retained", got)
	}
	r2, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatalf("repeat after publish-sync fault failed: %v", err)
	}
	if !r2.AlreadyRestored || r2.Applied || !r2.RestoredHealthy {
		t.Fatalf("repeat report=%+v, want AlreadyRestored and healthy", r2)
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want still 1", got)
	}
	if mustSHA(t, backup) != backupSHA {
		t.Fatal("backup changed")
	}
}

func TestRestore_RepeatAfterPostVerifyFaultCompletesVerification(t *testing.T) {
	// A failed post-restore verification leaves the restored bytes
	// authoritative but unverified. A repeat (with the fault removed)
	// completes the offline verification in the AlreadyRestored path.
	root, live, backup := restoreFixture(t)
	backupSHA := mustSHA(t, backup)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpPostVerify: durablewrite.ErrIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("post-verify fault must fail the first restore")
	}
	if mustSHA(t, live) != backupSHA {
		t.Fatal("restored bytes not in place after the faulted verification")
	}
	r2, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatalf("repeat after post-verify fault failed: %v", err)
	}
	if !r2.AlreadyRestored || r2.Applied || !r2.RestoredHealthy || r2.RestoreVerification != "healthy" {
		t.Fatalf("repeat report=%+v, want AlreadyRestored with healthy verification", r2)
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want still 1", got)
	}
	if mustSHA(t, backup) != backupSHA {
		t.Fatal("backup changed")
	}
}

func TestRestore_RepeatFaultPersistsNoFalseSuccess(t *testing.T) {
	// If the durability fault persists, the repeat must fail again rather
	// than report a false success, and must not create new evidence.
	root, live, backup := restoreFixture(t)
	backupSHA := mustSHA(t, backup)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpPublishDirectorySync: syscall.EIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("first restore must fail")
	}
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("repeat with the fault persisting must fail, not report a false success")
	}
	if !strings.Contains(err.Error(), "already-restored durability sync") {
		t.Fatalf("unexpected repeat error: %v", err)
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want still 1", got)
	}
	if mustSHA(t, backup) != backupSHA {
		t.Fatal("backup changed")
	}
}

func TestRestore_RepeatedDistinctRestorePreservesEvidence(t *testing.T) {
	// A genuine re-restore with a different verified backup is not
	// already-restored: it creates a second, distinct quarantine and the
	// first preserved original is never overwritten. Both backups are
	// recorded in the home's authoritative catalog at restore time, so the
	// restores run newest-first: once an older backup is restored, later
	// backups are no longer catalogued by the restored home and are refused
	// (fail-closed provenance sequencing).
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	l, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "a", root)
	backup1 := filepath.Join(root, "backup1.db")
	if _, err := l.Backup(backup1); err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "b", root)
	backup2 := filepath.Join(root, "backup2.db")
	if _, err := l.Backup(backup2); err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "c", root)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	original := mustSHA(t, live)
	dry2, err := Run(Options{LivePath: live, BackupPath: backup2})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := RunWithInjector(applyOptions(root, live, backup2, dry2.BackupSHA256), nil)
	if err != nil || !r2.Applied {
		t.Fatalf("newer restore failed: err=%v", err)
	}
	after2 := mustSHA(t, live)
	if after2 == original {
		t.Fatal("restore did not change the ledger")
	}
	dry1, err := Run(Options{LivePath: live, BackupPath: backup1})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := RunWithInjector(applyOptions(root, live, backup1, dry1.BackupSHA256), nil)
	if err != nil || !r1.Applied {
		t.Fatalf("older restore failed: err=%v", err)
	}
	all := evidenceDirs(t, root)
	if len(all) != 2 {
		t.Fatalf("evidence dirs=%v, want 2 (first retained)", all)
	}
	shas := map[string]bool{}
	for _, d := range all {
		shas[mustSHA(t, filepath.Join(d, "ledger.db"))] = true
	}
	if !shas[original] || !shas[after2] {
		t.Fatalf("preserved evidence shas %v, want %s and %s", shas, original, after2)
	}
	if r2.Preserved.LedgerSHA256 == r1.Preserved.LedgerSHA256 {
		t.Fatal("second restore reused the first quarantine")
	}
	// After the older backup is restored, the newer backup is no longer
	// recorded in the restored home's catalog: it must be refused rather than
	// trusted from its own embedded records.
	_, err = Run(Options{LivePath: live, BackupPath: backup2})
	if err == nil {
		t.Fatal("backup no longer catalogued after an older restore must be refused")
	}
	if !strings.Contains(err.Error(), "not recorded in the authoritative backup catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRestore_QuarantineDurabilityFaultsFailClosed(t *testing.T) {
	// Every quarantine durability boundary is fail-closed: a file-sync or
	// directory-sync failure during preservation aborts before publication,
	// removes only the partial quarantine, leaves the original authoritative
	// state byte-identical, and allows a safe retry after the fault is gone.
	cases := []struct {
		name         string
		op           string
		withSidecars bool
	}{
		{"ledger-file-sync", OpPreserveLedgerSync, false},
		{"wal-file-sync", OpPreserveWALSync, true},
		{"shm-file-sync", OpPreserveSHMSync, true},
		{"evidence-file-sync", OpQuarantineEvidenceSync, false},
		{"quarantine-dir-sync", OpQuarantineDirSync, false},
		{"parent-dir-sync", OpQuarantineParentSync, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var root, live, backup string
			if tc.withSidecars {
				root = t.TempDir()
				live, backup = walAtRestLedger(t, root)
			} else {
				root, live, backup = restoreFixture(t)
			}
			files := []string{live, live + "-wal", live + "-shm"}
			before := make(map[string]string, len(files))
			for _, f := range files {
				if _, err := os.Lstat(f); err == nil {
					before[f] = mustSHA(t, f)
				}
			}
			backupSHA := mustSHA(t, backup)
			dry, err := Run(Options{LivePath: live, BackupPath: backup})
			if err != nil {
				t.Fatal(err)
			}
			inject := durablewrite.NewFaultMap(map[string]error{tc.op: durablewrite.ErrIO})
			_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
			if err == nil {
				t.Fatalf("durability fault %s must fail the restore", tc.op)
			}
			// Publication never happened and the original authoritative state
			// is byte-identical (ledger and any sidecars).
			for f, sum := range before {
				if mustSHA(t, f) != sum {
					t.Fatalf("%s changed by a failed quarantine sync", f)
				}
			}
			if mustSHA(t, backup) != backupSHA {
				t.Fatal("backup changed")
			}
			// The partial quarantine was removed; nothing is left behind.
			assertNoRestoreArtifacts(t, root)
			// Retry after the fault is removed succeeds and preserves once.
			report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
			if err != nil || !report.Applied {
				t.Fatalf("retry failed: err=%v report=%+v", err, report)
			}
			if got := evidenceDirs(t, root); len(got) != 1 {
				t.Fatalf("evidence dirs=%v, want 1 after retry", got)
			}
		})
	}
}

func TestRestore_RemoveLiveSidecarsFaultRetainsQuarantine(t *testing.T) {
	// The sidecar removal is part of publication: a fault there aborts before
	// the rename, leaves the original ledger byte-identical, and retains the
	// completed quarantine (the removal itself is what failed, not the
	// preservation).
	root, live, backup := restoreFixture(t)
	before := mustSHA(t, live)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpRemoveLiveSidecars: syscall.EIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("remove-live-sidecars fault must fail the restore")
	}
	if !strings.Contains(err.Error(), "publish restored ledger") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed before the rename")
	}
	dirs := evidenceDirs(t, root)
	if len(dirs) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 retained", dirs)
	}
	if mustSHA(t, filepath.Join(dirs[0], "ledger.db")) != before {
		t.Fatal("retained quarantine does not hold the original")
	}
	// Retry succeeds.
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !report.Applied {
		t.Fatalf("retry failed: err=%v report=%+v", err, report)
	}
}

func TestRestore_Provenance_ValidCataloguedSameHomeBackup(t *testing.T) {
	// A later backup of the same home is recorded in the home's authoritative
	// catalog (under its exact path, with matching size and digest), so it
	// passes provenance and restores.
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	r, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, r, "one", root)
	addTx(t, r, "two", root)
	backup1 := filepath.Join(root, "backup1.db")
	if _, err := r.Backup(backup1); err != nil {
		t.Fatal(err)
	}
	backup2 := filepath.Join(root, "backup2.db")
	if _, err := r.Backup(backup2); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	dry, err := Run(Options{LivePath: live, BackupPath: backup2})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup2, dry.BackupSHA256), nil)
	if err != nil || !report.Applied {
		t.Fatalf("catalogued same-home backup must restore: err=%v report=%+v", err, report)
	}
	if mustSHA(t, live) == before {
		t.Fatal("restore did not replace the ledger")
	}
}

func TestRestore_Provenance_EmptyCandidateCatalogAccepted(t *testing.T) {
	// A home's first backup records nothing inside the backup file itself
	// (the snapshot predates its own catalog insert), yet it is recorded in
	// the live home's authoritative catalog. An empty candidate catalog is
	// accepted precisely because provenance comes from the home, not from
	// records embedded in the candidate.
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !report.Applied {
		t.Fatalf("first home backup with an empty candidate catalog must restore: err=%v", err)
	}
}

func TestRestore_Provenance_UncataloguedBackupRefused(t *testing.T) {
	// A valid ledger produced by a sibling repository and placed inside the
	// data root is not recorded in the live home's authoritative catalog and
	// must be refused even though its path is inside the root and its digest
	// is supplied: records embedded only inside a candidate file are never
	// treated as proof of same-home provenance.
	root, live, _ := restoreFixture(t)
	src := filepath.Join(root, "sibling-source.db")
	s, err := ledger.OpenRepository(src)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, s, "sibling", root)
	uncatalogued := filepath.Join(root, "uncatalogued.db")
	if _, err := s.Backup(uncatalogued); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	backupSHA := mustSHA(t, uncatalogued)
	// Provenance is part of validation, so the dry run refuses too.
	if _, err := Run(Options{LivePath: live, BackupPath: uncatalogued}); err == nil {
		t.Fatal("dry run with an uncatalogued backup must be refused")
	}
	_, err = RunWithInjector(applyOptions(root, live, uncatalogued, backupSHA), nil)
	if err == nil {
		t.Fatal("uncatalogued backup must be refused")
	}
	if !strings.Contains(err.Error(), "not recorded in the authoritative backup catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Refusal errors never leak environment secrets.
	for _, secret := range []string{os.Getenv("HOME"), os.Getenv("USER")} {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("refusal error leaks %q: %v", secret, err)
		}
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	if mustSHA(t, uncatalogued) != backupSHA {
		t.Fatal("backup changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_Provenance_OtherHomeBackupRefused(t *testing.T) {
	// A valid ledger produced by another FutureDiff home is refused even when
	// copied inside this data root: its records live only in the other home's
	// catalog, and this home's authoritative catalog does not record it.
	otherRoot := t.TempDir()
	br, err := ledger.OpenRepository(filepath.Join(otherRoot, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, br, "x", otherRoot)
	b1 := filepath.Join(otherRoot, "backup1.db")
	if _, err := br.Backup(b1); err != nil {
		t.Fatal(err)
	}
	b2 := filepath.Join(otherRoot, "backup2.db")
	if _, err := br.Backup(b2); err != nil {
		t.Fatal(err)
	}
	if err := br.Close(); err != nil {
		t.Fatal(err)
	}
	root, live, _ := restoreFixture(t)
	foreign := filepath.Join(root, "foreign.db")
	if err := copyFile(b2, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	// Provenance is part of validation, so the dry run refuses too.
	if _, err := Run(Options{LivePath: live, BackupPath: foreign}); err == nil {
		t.Fatal("dry run with a foreign home's backup must be refused")
	}
	_, err = RunWithInjector(applyOptions(root, live, foreign, mustSHA(t, foreign)), nil)
	if err == nil {
		t.Fatal("foreign home's backup must be refused")
	}
	if !strings.Contains(err.Error(), "not recorded in the authoritative backup catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_Provenance_MismatchedAuthoritativeDigestRefused(t *testing.T) {
	// The authoritative catalog records the digest of the backup at creation
	// time; a file whose on-disk bytes no longer match that recorded digest
	// is refused even when the operator supplies the current digest.
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	r, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, r, "one", root)
	backup := filepath.Join(root, "backup.db")
	if _, err := r.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	corruptMiddleBytes(t, backup)
	before := mustSHA(t, live)
	current := mustSHA(t, backup)
	if _, err := Run(Options{LivePath: live, BackupPath: backup}); err == nil {
		t.Fatal("dry run with a tampered backup must be refused")
	}
	_, err = RunWithInjector(applyOptions(root, live, backup, current), nil)
	if err == nil {
		t.Fatal("apply with a tampered backup must be refused")
	}
	if !strings.Contains(err.Error(), "does not match the on-disk file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_Provenance_MismatchedAuthoritativeSizeRefused(t *testing.T) {
	// Appending trailing bytes changes the on-disk size, which no longer
	// matches the recorded size in the authoritative catalog: refused.
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	r, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, r, "one", root)
	backup := filepath.Join(root, "backup.db")
	if _, err := r.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(backup, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("stale-trailing-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	current := mustSHA(t, backup)
	if _, err := Run(Options{LivePath: live, BackupPath: backup}); err == nil {
		t.Fatal("dry run with a size-mismatched backup must be refused")
	}
	_, err = RunWithInjector(applyOptions(root, live, backup, current), nil)
	if err == nil {
		t.Fatal("apply with a size-mismatched backup must be refused")
	}
	if !strings.Contains(err.Error(), "catalog record has size") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_Provenance_MismatchedAuthoritativePathRefused(t *testing.T) {
	// The authoritative catalog binds the record to the exact recorded path;
	// restoring the same bytes from a different path inside the root is not a
	// catalogued backup and is refused.
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	r, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, r, "one", root)
	backup1 := filepath.Join(root, "backup1.db")
	if _, err := r.Backup(backup1); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(root, "renamed.db")
	if err := copyFile(backup1, renamed, 0o600); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	sha := mustSHA(t, renamed)
	if _, err := Run(Options{LivePath: live, BackupPath: renamed}); err == nil {
		t.Fatal("dry run with a path-mismatched backup must be refused")
	}
	_, err = RunWithInjector(applyOptions(root, live, renamed, sha), nil)
	if err == nil {
		t.Fatal("apply with a path-mismatched backup must be refused")
	}
	if !strings.Contains(err.Error(), "not recorded in the authoritative backup catalog") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_Provenance_MissingLiveCatalogFailsClosed(t *testing.T) {
	// With no live ledger there is no authoritative catalog to prove
	// same-home provenance: restore fails closed rather than trusting a file
	// merely because it sits inside the data root with a supplied digest.
	root := t.TempDir()
	live := filepath.Join(root, "live.db")
	src := filepath.Join(root, "source.db")
	s, err := ledger.OpenRepository(src)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, s, "one", root)
	backup := filepath.Join(root, "backup.db")
	if _, err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Run(Options{LivePath: live, BackupPath: backup})
	if err == nil {
		t.Fatal("dry run with a missing live ledger must fail closed")
	}
	if !strings.Contains(err.Error(), "authoritative backup catalog unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = RunWithInjector(applyOptions(root, live, backup, mustSHA(t, backup)), nil)
	if err == nil {
		t.Fatal("apply with a missing live ledger must fail closed")
	}
	if !strings.Contains(err.Error(), "authoritative backup catalog unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_Provenance_CorruptCatalogFailsClosed(t *testing.T) {
	// A catalog read that cannot complete fails the restore closed: an
	// unreadable authoritative catalog must never be silently bypassed.
	root, live, backup := restoreFixture(t)
	before := mustSHA(t, live)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpAuthoritativeCatalogRead: durablewrite.ErrIO})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("unreadable authoritative catalog must fail closed")
	}
	if !strings.Contains(err.Error(), "authoritative backup catalog unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_Provenance_ReplacedBackupRefused(t *testing.T) {
	// After a successful dry-run validation, replacing the backup file with a
	// different ledger (or making the path a symlink) must be refused at
	// apply time: the operator's digest no longer matches, and the staged
	// copy is re-bound to the verified digest.
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	// Replace the validated backup with another home's ledger.
	otherRoot := t.TempDir()
	br, err := ledger.OpenRepository(filepath.Join(otherRoot, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, br, "x", otherRoot)
	b1 := filepath.Join(otherRoot, "backup1.db")
	if _, err := br.Backup(b1); err != nil {
		t.Fatal(err)
	}
	b2 := filepath.Join(otherRoot, "backup2.db")
	if _, err := br.Backup(b2); err != nil {
		t.Fatal(err)
	}
	if err := br.Close(); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(b2, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	before := mustSHA(t, live)
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err == nil {
		t.Fatal("replaced backup must be refused")
	}
	if !strings.Contains(err.Error(), "does not match expected digest") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	// A symlink planted at the backup path is also refused.
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target.db")
	if err := copyFile(b2, target, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, backup); err != nil {
		t.Fatal(err)
	}
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err == nil {
		t.Fatal("symlinked backup must be refused")
	}
	if !strings.Contains(err.Error(), "must be a regular file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}

// swapBackupInjector replaces the backup file's contents with another file's
// bytes at the staging-create boundary, simulating a swap that lands between
// the digest computation and the staging copy.
type swapBackupInjector struct {
	swapTo string
	backup string
}

func (s *swapBackupInjector) Before(op string) error {
	if op != durablewrite.OpCreate {
		return nil
	}
	return copyFile(s.swapTo, s.backup, 0o600)
}

func TestRestore_Provenance_BackupReplacedDuringStagingRefused(t *testing.T) {
	// Even within a single apply call the staged bytes are re-bound to the
	// verified digest: a backup swapped between the digest computation and
	// the staging copy is refused and never published.
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	other := buildLedgerAt(t, t.TempDir(), "other.db", 1)
	before := mustSHA(t, live)
	swapped := mustSHA(t, other)
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), &swapBackupInjector{swapTo: other, backup: backup})
	if err == nil {
		t.Fatal("backup swapped during staging must be refused")
	}
	if !strings.Contains(err.Error(), "changed during staging") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	// The restore never wrote to the backup path; the only difference is the
	// attacker's own swap.
	if mustSHA(t, backup) != swapped {
		t.Fatal("backup bytes were further modified by the restore")
	}
	assertNoRestoreArtifacts(t, root)
}

func TestRestore_StorageClassification(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{durablewrite.ErrDiskFull, "disk_full"},
		{syscall.ENOSPC, "disk_full"},
		{durablewrite.ErrQuotaExceeded, "quota_exceeded"},
		{syscall.EDQUOT, "quota_exceeded"},
		{durablewrite.ErrReadOnlyFilesystem, "filesystem_read_only"},
		{syscall.EROFS, "filesystem_read_only"},
		{durablewrite.ErrIO, "storage_io_failure"},
		{syscall.EIO, "storage_io_failure"},
		{errors.New("unrelated"), ""},
	}
	for _, tc := range cases {
		if got := Classify(tc.err); got != tc.want {
			t.Errorf("Classify(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	// A wrapped errno through the full restore path classifies and matches
	// errors.Is.
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	inject := durablewrite.NewFaultMap(map[string]error{OpQuarantineCreate: syscall.ENOSPC})
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), inject)
	if err == nil {
		t.Fatal("expected quarantine-create fault")
	}
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("errors.Is lost through the restore path: %v", err)
	}
	if got := Classify(err); got != "disk_full" {
		t.Fatalf("Classify(%v) = %q, want disk_full", err, got)
	}
}

func TestRestore_NoSecretLeakInReport(t *testing.T) {
	t.Setenv("FUTUREDIFF_TEST_TOKEN", "super-secret-token-value")
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, secret := range []string{"super-secret-token-value", os.Getenv("HOME"), os.Getenv("USER")} {
		if secret != "" && strings.Contains(s, secret) {
			t.Fatalf("report leaks %q", secret)
		}
	}
	for _, key := range []string{`"token"`, `"credential"`, `"secret"`, `"password"`, `"private_key"`} {
		if strings.Contains(strings.ToLower(s), key) {
			t.Fatalf("report contains credential-bearing key %s", key)
		}
	}
}

func TestRestore_StaleStagingCleanedSafely(t *testing.T) {
	root, live, backup := restoreFixture(t)
	stale := filepath.Join(root, ".ledger-restore-abandoned.db")
	if err := os.WriteFile(stale, []byte("partial staging from a crashed restore"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Abandoned semantic scratch directories from a crashed semantic backup
	// are also swept.
	semanticScratch := filepath.Join(root, ".ledger-restore-semantic-abc")
	if err := os.Mkdir(semanticScratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(semanticScratch, "ledger.db"), []byte("scratch"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !report.Applied {
		t.Fatalf("restore failed: err=%v", err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatal("abandoned staging file was not cleaned")
	}
	if _, err := os.Lstat(semanticScratch); !os.IsNotExist(err) {
		t.Fatal("abandoned semantic scratch directory was not cleaned")
	}
	if _, err := os.Lstat(unrelated); err != nil {
		t.Fatal("unrelated file was removed")
	}
	if got := stagingArtifacts(t, root); len(got) != 0 {
		t.Fatalf("staging artifacts remain after restore: %v", got)
	}
	if got := evidenceDirs(t, root); len(got) != 1 {
		t.Fatalf("evidence dirs=%v, want 1 retained after success", got)
	}
}

func TestRestore_NoAutoPreRestoreFileByDefault(t *testing.T) {
	// The documented contract: with an empty PreRestoreBackupPath no
	// standalone pre-restore backup file is written — the quarantine is the
	// preservation mechanism, and repeated restores must not accumulate
	// auto-named files in the data root.
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !report.Applied {
		t.Fatalf("restore failed: err=%v", err)
	}
	if report.PreRestoreBackup != nil {
		t.Fatalf("unexpected pre-restore backup: %+v", *report.PreRestoreBackup)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "ledger.pre-restore.") {
			t.Fatalf("auto-named pre-restore file written by default: %s", e.Name())
		}
	}
	// An explicit path is still honored: it produces exactly one file at the
	// operator-chosen path and is reported.
	root2, live2, backup2 := restoreFixture(t)
	dry2, err := Run(Options{LivePath: live2, BackupPath: backup2})
	if err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(root2, "pre.db")
	report2, err := RunWithInjector(Options{LivePath: live2, BackupPath: backup2, ExpectedSHA256: dry2.BackupSHA256, Apply: true, Confirmation: Confirmation, AllowStaleBackup: true, PreRestoreBackupPath: explicit}, nil)
	if err != nil || !report2.Applied {
		t.Fatalf("explicit-path restore failed: err=%v", err)
	}
	if report2.PreRestoreBackup == nil || report2.PreRestoreBackup.Path != explicit {
		t.Fatalf("explicit pre-restore backup not reported: %+v", report2.PreRestoreBackup)
	}
	if _, err := os.Lstat(explicit); err != nil {
		t.Fatal("explicit pre-restore file not written")
	}
}

// recordingInjector observes whether the staged file lands in the live
// directory (same filesystem as the authoritative path) at the staging
// directory-sync boundary, then fails the restore with EDQUOT.
type recordingInjector struct {
	dir string
	saw bool
}

func (r *recordingInjector) Before(op string) error {
	if op != durablewrite.OpDirectorySync {
		return nil
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".ledger-restore-") && strings.HasSuffix(e.Name(), ".db") {
			r.saw = true
		}
	}
	return syscall.EDQUOT
}

func TestRestore_StagingIsSameFilesystemAsLive(t *testing.T) {
	// The staging file is created inside the live ledger's own directory, so
	// the publish rename is an atomic same-device replacement. The faulted
	// staging run observes the staged file in that directory before failing.
	root, live, backup := restoreFixture(t)
	before := mustSHA(t, live)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recordingInjector{dir: filepath.Dir(live)}
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), rec)
	if err == nil {
		t.Fatal("staging directory-sync fault must fail the restore")
	}
	if !rec.saw {
		t.Fatal("staged file was not observed in the live directory")
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	assertNoRestoreArtifacts(t, root)
}
