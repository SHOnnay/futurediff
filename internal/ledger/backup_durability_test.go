package ledger

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/SHOnnay/futurediff/internal/durablewrite"
)

func backupRepo(t *testing.T) (*Repository, string) {
	t.Helper()
	root := t.TempDir()
	repo, err := OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo, root
}

func backupValid(t *testing.T, path string) {
	t.Helper()
	r, err := OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.HealthCheck(); err != nil {
		t.Fatal(err)
	}
}

func backupTemps(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "ledger.db.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func backupCount(t *testing.T, repo *Repository) int {
	t.Helper()
	recs, err := repo.Backups()
	if err != nil {
		t.Fatal(err)
	}
	return len(recs)
}

func TestBackupDurableSuccess(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	record, err := repo.BackupWithInjector(dest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if record.SHA256 == "" || record.SizeBytes == 0 {
		t.Fatalf("bad record: %+v", record)
	}
	backupValid(t, dest)
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
	if got := backupCount(t, repo); got != 1 {
		t.Fatalf("records=%d", got)
	}
}

func TestBackupCreateFailure(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpCreate: durablewrite.ErrIO})
	if _, err := repo.BackupWithInjector(dest, inject); err == nil {
		t.Fatal("expected create failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("backup appeared despite create fault")
	}
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
	if got := backupCount(t, repo); got != 0 {
		t.Fatalf("record inserted despite failure: %d", got)
	}
}

func TestBackupWriteFailure(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpWrite: durablewrite.ErrIO})
	if _, err := repo.BackupWithInjector(dest, inject); err == nil {
		t.Fatal("expected write failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("backup appeared despite write fault")
	}
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
	if got := backupCount(t, repo); got != 0 {
		t.Fatalf("record inserted despite failure: %d", got)
	}
}

func TestBackupShortWriteNoAuthoritativePartial(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpShortWrite: durablewrite.ErrIO})
	if _, err := repo.BackupWithInjector(dest, inject); err == nil {
		t.Fatal("expected short-write failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("partial backup became authoritative")
	}
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("partial temp leaked: %v", temps)
	}
	if got := backupCount(t, repo); got != 0 {
		t.Fatalf("record inserted despite failure: %d", got)
	}
}

func TestBackupFileSyncFailure(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO})
	if _, err := repo.BackupWithInjector(dest, inject); err == nil {
		t.Fatal("expected file-sync failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("backup appeared despite sync fault")
	}
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestBackupRenameFailure(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpRename: durablewrite.ErrIO})
	if _, err := repo.BackupWithInjector(dest, inject); err == nil {
		t.Fatal("expected rename failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("backup appeared despite rename fault")
	}
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
	if got := backupCount(t, repo); got != 0 {
		t.Fatalf("record inserted despite failure: %d", got)
	}
}

func TestBackupDirectorySyncFailureReported(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpDirectorySync: durablewrite.ErrIO})
	if _, err := repo.BackupWithInjector(dest, inject); err == nil {
		t.Fatal("expected directory-sync failure; no false success")
	}
	// Rename committed before the directory sync: the new backup file is
	// visible and valid, but crash durability is not confirmed and the error
	// is reported. No record is inserted because Backup reported failure.
	backupValid(t, dest)
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
	if got := backupCount(t, repo); got != 0 {
		t.Fatalf("record inserted despite failure: %d", got)
	}
}

func TestBackupPreservesPreviousBeforeRename(t *testing.T) {
	ops := []string{
		durablewrite.OpCreate,
		durablewrite.OpWrite,
		durablewrite.OpShortWrite,
		durablewrite.OpFileSync,
		durablewrite.OpRename,
	}
	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			repo, root := backupRepo(t)
			dest := filepath.Join(root, "backups", "ledger.db")
			if _, err := repo.Backup(dest); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			inject := durablewrite.NewFaultMap(map[string]error{op: durablewrite.ErrIO})
			if _, err := repo.BackupWithInjector(dest, inject); err == nil {
				t.Fatalf("expected %s failure", op)
			}
			after, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("previous backup changed after %s fault", op)
			}
			backupValid(t, dest)
		})
	}
}

func TestBackupClassification(t *testing.T) {
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
			repo, root := backupRepo(t)
			dest := filepath.Join(root, "backups", "ledger.db")
			inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpCreate: c.fail})
			_, err := repo.BackupWithInjector(dest, inject)
			if err == nil {
				t.Fatal("expected failure")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
		})
	}
}

func TestBackupNoFalseSuccess(t *testing.T) {
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
			repo, root := backupRepo(t)
			dest := filepath.Join(root, "backups", "ledger.db")
			inject := durablewrite.NewFaultMap(map[string]error{op: durablewrite.ErrIO})
			if _, err := repo.BackupWithInjector(dest, inject); err == nil {
				t.Fatalf("op %s returned success", op)
			}
		})
	}
}

func TestBackupRetryAfterFaultRemoved(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewOneShot(map[string]error{durablewrite.OpCreate: durablewrite.ErrIO})
	if _, err := repo.BackupWithInjector(dest, inject); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("backup appeared despite first-attempt fault")
	}
	record, err := repo.BackupWithInjector(dest, inject)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if record.SHA256 == "" {
		t.Fatalf("bad record: %+v", record)
	}
	backupValid(t, dest)
	if got := backupCount(t, repo); got != 1 {
		t.Fatalf("records=%d", got)
	}
}

func TestBackupConcurrentRaceSafe(t *testing.T) {
	repo, root := backupRepo(t)
	dest := filepath.Join(root, "backups", "ledger.db")
	inject := durablewrite.NewFaultMap(map[string]error{})
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = repo.BackupWithInjector(dest, inject)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
	}
	backupValid(t, dest)
	if temps := backupTemps(t, filepath.Dir(dest)); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
	if got := backupCount(t, repo); got != 8 {
		t.Fatalf("records=%d", got)
	}
}
