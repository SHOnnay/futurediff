package upgraderehearsal

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/ledger"
)

type Report struct {
	FormatVersion          string             `json:"format_version"`
	SourceLedger           string             `json:"source_ledger"`
	SourceSHA256           string             `json:"source_sha256"`
	RehearsalLedger        string             `json:"rehearsal_ledger"`
	BeforeMigrationCount   int64              `json:"before_migration_count"`
	AfterMigrationCount    int64              `json:"after_migration_count"`
	BeforeTransactionCount int64              `json:"before_transaction_count"`
	AfterTransactionCount  int64              `json:"after_transaction_count"`
	BeforeUnresolvedCount  int64              `json:"before_unresolved_count"`
	AfterUnresolvedCount   int64              `json:"after_unresolved_count"`
	Audit                  ledger.AuditReport `json:"audit"`
	SourceUnchanged        bool               `json:"source_unchanged"`
	RehearsalSucceeded     bool               `json:"rehearsal_succeeded"`
	GeneratedAt            time.Time          `json:"generated_at"`
}

func Run(root string) (Report, error) {
	if root == "" {
		return Report{}, errors.New("data root is required")
	}
	source := filepath.Join(root, "ledger.db")
	if _, err := os.Stat(filepath.Join(root, "futurediff.sock")); err == nil {
		return Report{}, errors.New("daemon socket exists; stop daemon before upgrade rehearsal")
	}
	beforeSHA, err := fileSHA(source)
	if err != nil {
		return Report{}, err
	}
	raw, err := ledger.Open(source)
	if err != nil {
		return Report{}, err
	}
	beforeM := count(raw, "SELECT COUNT(*) AS count FROM schema_migrations")
	beforeT := count(raw, "SELECT COUNT(*) AS count FROM transactions")
	beforeU := count(raw, "SELECT COUNT(*) AS count FROM transactions WHERE status IN ('committing','needs_reconciliation','compensating')")
	tempDir, err := os.MkdirTemp(root, "upgrade-rehearsal-")
	if err != nil {
		_ = raw.Close()
		return Report{}, err
	}
	candidate := filepath.Join(tempDir, "ledger.db")
	if err := raw.BackupTo(candidate); err != nil {
		_ = raw.Close()
		return Report{}, err
	}
	_ = raw.Close()
	repo, err := ledger.OpenRepository(candidate)
	if err != nil {
		return Report{}, err
	}
	defer repo.Close()
	health, err := repo.HealthCheck()
	if err != nil {
		return Report{}, err
	}
	audit, err := repo.Audit()
	if err != nil {
		return Report{}, err
	}
	afterSHA, err := fileSHA(source)
	if err != nil {
		return Report{}, err
	}
	r := Report{FormatVersion: "0.1", SourceLedger: source, SourceSHA256: beforeSHA, RehearsalLedger: candidate, BeforeMigrationCount: beforeM, AfterMigrationCount: health.MigrationCount, BeforeTransactionCount: beforeT, AfterTransactionCount: health.TransactionCount, BeforeUnresolvedCount: beforeU, AfterUnresolvedCount: health.UnresolvedCount, Audit: audit, SourceUnchanged: beforeSHA == afterSHA, GeneratedAt: time.Now().UTC()}
	r.RehearsalSucceeded = r.SourceUnchanged && audit.Healthy && beforeT == health.TransactionCount && beforeU == health.UnresolvedCount && health.MigrationCount >= beforeM
	if !r.RehearsalSucceeded {
		return r, fmt.Errorf("upgrade rehearsal did not satisfy invariants")
	}
	return r, nil
}
func count(db *ledger.DB, q string) int64 {
	rows, err := db.Query(q)
	if err != nil || len(rows) == 0 {
		return 0
	}
	return ledger.Int64(rows[0], "count")
}
func fileSHA(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}
