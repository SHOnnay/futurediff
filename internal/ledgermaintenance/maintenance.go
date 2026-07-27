package ledgermaintenance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Confirmation = "MAINTAIN_FUTUREDIFF_LEDGER"

type Report struct {
	Root        string              `json:"root"`
	Applied     bool                `json:"applied"`
	LedgerPath  string              `json:"ledger_path"`
	SizeBefore  int64               `json:"size_before"`
	SizeAfter   int64               `json:"size_after"`
	Backup      ledger.BackupRecord `json:"backup,omitempty"`
	Before      ledger.AuditReport  `json:"before,omitempty"`
	After       ledger.AuditReport  `json:"after,omitempty"`
	GeneratedAt time.Time           `json:"generated_at"`
}

func Run(root string, apply bool, confirm string, now time.Time) (Report, error) {
	if !filepath.IsAbs(root) {
		return Report{}, errors.New("root must be absolute")
	}
	ledgerPath := filepath.Join(root, "ledger.db")
	st, err := os.Stat(ledgerPath)
	if err != nil {
		return Report{}, err
	}
	out := Report{Root: root, LedgerPath: ledgerPath, SizeBefore: st.Size(), SizeAfter: st.Size(), GeneratedAt: now.UTC()}
	status, err := daemonlock.Inspect(filepath.Join(root, "daemon.lock"), now)
	if err != nil {
		return out, err
	}
	if status.Held {
		return out, errors.New("daemon must be stopped before ledger maintenance")
	}
	if !apply {
		return out, nil
	}
	if confirm != Confirmation {
		return out, fmt.Errorf("apply requires --confirm %s", Confirmation)
	}
	lock, err := daemonlock.Acquire(filepath.Join(root, "daemon.lock"), root, now)
	if err != nil {
		return out, err
	}
	defer lock.Release()
	repo, err := ledger.OpenRepository(ledgerPath)
	if err != nil {
		return out, err
	}
	defer repo.Close()
	before, err := repo.Audit()
	if err != nil {
		return out, err
	}
	out.Before = before
	if !before.Healthy {
		return out, errors.New("pre-maintenance ledger audit is unhealthy")
	}
	backupPath := filepath.Join(root, "backups", fmt.Sprintf("pre-maintenance-%s.db", now.UTC().Format("20060102T150405Z")))
	backup, err := repo.Backup(backupPath)
	if err != nil {
		return out, err
	}
	out.Backup = backup
	if err := repo.OptimizeLedger(); err != nil {
		return out, err
	}
	after, err := repo.Audit()
	if err != nil {
		return out, err
	}
	out.After = after
	if !after.Healthy {
		return out, errors.New("post-maintenance ledger audit is unhealthy")
	}
	st, err = os.Stat(ledgerPath)
	if err != nil {
		return out, err
	}
	out.SizeAfter = st.Size()
	out.Applied = true
	return out, nil
}
