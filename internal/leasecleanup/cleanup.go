package leasecleanup

import (
	"errors"
	"fmt"
	"github.com/SHOnnay/futurediff/internal/daemonlock"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"path/filepath"
	"time"
)

const Confirmation = "DELETE_EXPIRED_FUTUREDIFF_LEASES"

type Report struct {
	Root         string               `json:"root"`
	Leases       []ledger.LeaseRecord `json:"leases"`
	ExpiredCount int                  `json:"expired_count"`
	Deleted      int64                `json:"deleted"`
	Applied      bool                 `json:"applied"`
	GeneratedAt  time.Time            `json:"generated_at"`
}

func Run(root string, apply bool, confirm string, now time.Time) (Report, error) {
	if !filepath.IsAbs(root) {
		return Report{}, errors.New("root must be absolute")
	}
	status, err := daemonlock.Inspect(filepath.Join(root, "daemon.lock"), now)
	if err != nil {
		return Report{}, err
	}
	if status.Held {
		return Report{}, errors.New("daemon must be stopped before lease cleanup")
	}
	lock, err := daemonlock.Acquire(filepath.Join(root, "daemon.lock"), root, now)
	if err != nil {
		return Report{}, err
	}
	defer lock.Release()
	repo, err := ledger.OpenRepository(filepath.Join(root, "ledger.db"))
	if err != nil {
		return Report{}, err
	}
	defer repo.Close()
	ls, err := repo.Leases(now)
	if err != nil {
		return Report{}, err
	}
	out := Report{Root: root, Leases: ls, GeneratedAt: now.UTC()}
	for _, l := range ls {
		if l.Expired {
			out.ExpiredCount++
		}
	}
	if !apply {
		return out, nil
	}
	if confirm != Confirmation {
		return out, fmt.Errorf("apply requires --confirm %s", Confirmation)
	}
	n, err := repo.DeleteExpiredLeases(now)
	if err != nil {
		return out, err
	}
	out.Deleted = n
	out.Applied = true
	return out, nil
}
