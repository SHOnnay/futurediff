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
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	source, err := ledger.OpenRepository(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, source, "one", root)
	backupPath := filepath.Join(root, "backup.db")
	if _, err = source.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	source.Close()
	livePath := filepath.Join(root, "live.db")
	live, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, live, "one", root)
	addTx(t, live, "two", root)
	live.Close()
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
	sourcePath := filepath.Join(root, "source.db")
	source, err := ledger.OpenRepository(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, source, "one", root)
	backupPath := filepath.Join(root, "backup.db")
	if _, err = source.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	source.Close()
	livePath := filepath.Join(root, "live.db")
	live, err := ledger.OpenRepository(livePath)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, live, "one", root)
	live.Close()
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
// two transactions and a verified backup of a sibling ledger with one
// transaction (an older backup that requires AllowStaleBackup to apply).
func restoreFixture(t *testing.T) (root, live, backup string) {
	t.Helper()
	root = t.TempDir()
	src := filepath.Join(root, "source.db")
	s, err := ledger.OpenRepository(src)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, s, "one", root)
	backup = filepath.Join(root, "backup.db")
	if _, err := s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	live = filepath.Join(root, "live.db")
	l, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "one", root)
	addTx(t, l, "two", root)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return root, live, backup
}

// walAtRestLedger builds a ledger whose WAL and SHM files exist at rest with
// uncheckpointed frames, copied out while the writer connection is open.
func walAtRestLedger(t *testing.T, root string) string {
	t.Helper()
	scratch := t.TempDir()
	path := filepath.Join(scratch, "ledger.db")
	r, err := ledger.OpenRepository(path)
	if err != nil {
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
	return filepath.Join(root, "ledger.db")
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

// assertNoRestoreArtifacts asserts that neither staging files nor quarantine
// evidence directories remain in dir.
func assertNoRestoreArtifacts(t *testing.T, dir string) {
	t.Helper()
	if got := stagingArtifacts(t, dir); len(got) != 0 {
		t.Fatalf("staging artifacts remain: %v", got)
	}
	if got := evidenceDirs(t, dir); len(got) != 0 {
		t.Fatalf("evidence dirs remain: %v", got)
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
	live := walAtRestLedger(t, root)
	walSHA := mustSHA(t, live+"-wal")
	shmSHA := mustSHA(t, live+"-shm")
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
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err == nil {
		t.Fatal("symlinked sidecar must refuse the restore")
	}
	if !strings.Contains(err.Error(), "sidecar") {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustSHA(t, live) != before {
		t.Fatal("live ledger changed")
	}
	if mustSHA(t, backup) != backupSHA {
		t.Fatal("backup changed")
	}
	// The partial quarantine is removed; nothing is left behind.
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
				live = walAtRestLedger(t, root)
				src := filepath.Join(root, "source.db")
				s, err := ledger.OpenRepository(src)
				if err != nil {
					t.Fatal(err)
				}
				addTx(t, s, "one", root)
				backup = filepath.Join(root, "backup.db")
				if _, err := s.Backup(backup); err != nil {
					t.Fatal(err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
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

func TestRestore_RepeatedRestorePreservesEvidence(t *testing.T) {
	root, live, backup := restoreFixture(t)
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !r1.Applied {
		t.Fatalf("first restore failed: err=%v", err)
	}
	after1 := mustSHA(t, live)
	r2, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil || !r2.Applied {
		t.Fatalf("second restore failed: err=%v", err)
	}
	after2 := mustSHA(t, live)
	if after1 != after2 {
		t.Fatal("repeated restore is not stable")
	}
	dirs := evidenceDirs(t, root)
	if len(dirs) != 2 {
		t.Fatalf("evidence dirs=%v, want 2", dirs)
	}
	// The first preserved original is still intact; the second holds the
	// first restored state. Neither was overwritten.
	if mustSHA(t, filepath.Join(dirs[0], "ledger.db")) != after1 && mustSHA(t, filepath.Join(dirs[0], "ledger.db")) != r1.Preserved.LedgerSHA256 {
		// The ordering of evidenceDirs is directory order; assert by content
		// instead of position.
	}
	first := r1.Preserved.LedgerSHA256
	second := r2.Preserved.LedgerSHA256
	if first == second {
		t.Fatal("second restore reused the first quarantine")
	}
	all := evidenceDirs(t, root)
	shas := map[string]bool{}
	for _, d := range all {
		shas[mustSHA(t, filepath.Join(d, "ledger.db"))] = true
	}
	if !shas[first] || !shas[second] {
		t.Fatalf("preserved evidence shas %v, want %s and %s", shas, first, second)
	}
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
