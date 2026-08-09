package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
)

type Health struct {
	IntegrityOK      bool  `json:"integrity_ok"`
	MigrationCount   int64 `json:"migration_count"`
	TransactionCount int64 `json:"transaction_count"`
	UnresolvedCount  int64 `json:"unresolved_transaction_count"`
}

type BackupRecord struct {
	BackupID  string    `json:"backup_id"`
	Path      string    `json:"path"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *Repository) HealthCheck() (Health, error) {
	if err := r.db.IntegrityCheck(); err != nil {
		return Health{}, err
	}
	migrationRows, err := r.db.Query("SELECT COUNT(*) AS count FROM schema_migrations")
	if err != nil {
		return Health{}, err
	}
	transactionRows, err := r.db.Query("SELECT COUNT(*) AS count FROM transactions")
	if err != nil {
		return Health{}, err
	}
	unresolvedRows, err := r.db.Query("SELECT COUNT(*) AS count FROM transactions WHERE status IN ('committing','needs_reconciliation','compensating')")
	if err != nil {
		return Health{}, err
	}
	return Health{IntegrityOK: true, MigrationCount: Int64(migrationRows[0], "count"), TransactionCount: Int64(transactionRows[0], "count"), UnresolvedCount: Int64(unresolvedRows[0], "count")}, nil
}

// Backup creates a verified, durable backup of the ledger at path. The backup
// file is produced through the shared durable-write helper: the temporary file
// is created, written (via the SQLite online backup API), fsynced, verified,
// and renamed over the destination, and the parent directory is fsynced so the
// rename is crash-durable. The previous authoritative file at path is
// preserved unless the full sequence succeeds.
func (r *Repository) Backup(path string) (BackupRecord, error) {
	return r.BackupWithInjector(path, nil)
}

// BackupWithInjector is Backup with a test-only durable-write fault injector
// (ADR-099). Production callers use Backup; nothing outside tests constructs
// an injector. The injector is consulted at the create, write, short_write,
// file_sync, rename, and directory_sync boundaries of the backup file write.
func (r *Repository) BackupWithInjector(path string, inject durablewrite.Injector) (BackupRecord, error) {
	if path == "" {
		return BackupRecord{}, errors.New("backup path required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return BackupRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return BackupRecord{}, err
	}
	var record BackupRecord
	produce := func(tmp string) error {
		// Opening the backup for verification leaves SQLite -wal/-shm sidecars
		// next to the temporary file (the backup inherits WAL journal mode from
		// the source). Remove them so only the backup file itself remains; the
		// previous code leaked these permanently next to the backup.
		defer func() { _ = os.Remove(tmp + "-wal"); _ = os.Remove(tmp + "-shm") }()
		if err := r.db.BackupTo(tmp); err != nil {
			return err
		}
		candidate, err := Open(tmp)
		if err != nil {
			return err
		}
		checkErr := candidate.IntegrityCheck()
		_ = candidate.Close()
		if checkErr != nil {
			return checkErr
		}
		data, err := os.ReadFile(tmp)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		record = BackupRecord{BackupID: domain.NewID("backup"), Path: absolute, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data)), CreatedAt: time.Now().UTC()}
		return nil
	}
	if err := durablewrite.ReplaceFileVia(absolute, 0o600, inject, produce); err != nil {
		return BackupRecord{}, err
	}
	if _, err := r.db.Exec("INSERT INTO ledger_backups(backup_id,path,sha256,size_bytes,created_at) VALUES(?,?,?,?,?)", record.BackupID, record.Path, record.SHA256, record.SizeBytes, ts(record.CreatedAt)); err != nil {
		return BackupRecord{}, err
	}
	return record, nil
}
