package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
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

func (r *Repository) Backup(path string) (BackupRecord, error) {
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
	tmp := absolute + ".tmp"
	_ = os.Remove(tmp)
	if err := r.db.BackupTo(tmp); err != nil {
		return BackupRecord{}, err
	}
	candidate, err := Open(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return BackupRecord{}, err
	}
	checkErr := candidate.IntegrityCheck()
	_ = candidate.Close()
	if checkErr != nil {
		_ = os.Remove(tmp)
		return BackupRecord{}, checkErr
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return BackupRecord{}, err
	}
	sum := sha256.Sum256(data)
	if err := os.Rename(tmp, absolute); err != nil {
		_ = os.Remove(tmp)
		return BackupRecord{}, err
	}
	record := BackupRecord{BackupID: domain.NewID("backup"), Path: absolute, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data)), CreatedAt: time.Now().UTC()}
	if _, err := r.db.Exec("INSERT INTO ledger_backups(backup_id,path,sha256,size_bytes,created_at) VALUES(?,?,?,?,?)", record.BackupID, record.Path, record.SHA256, record.SizeBytes, ts(record.CreatedAt)); err != nil {
		return BackupRecord{}, err
	}
	return record, nil
}
