package smoke

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type DestructiveShellContainmentReport struct {
	DirectDuration            time.Duration
	FutureDiffDuration        time.Duration
	DirectRepoDamaged         bool
	FutureDiffRepoDamaged     bool
	FutureDiffState           string
	StagedPatchContainsDelete bool
}

func (r Runner) CompareDestructiveShellContainment(ctx context.Context) (*DestructiveShellContainmentReport, error) {
	directRepo, err := initTrackedGitRepo("direct-shell-benchmark")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directRepo)

	futureRepo, err := initTrackedGitRepo("futurediff-shell-benchmark")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(futureRepo)

	report := &DestructiveShellContainmentReport{}
	command := "rm tracked.txt; mkdir -p nested; printf 'contained scratch\n' > nested/scratch.txt"

	directStart := time.Now()
	directRun := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	directRun.Dir = directRepo
	if _, err := directRun.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("run direct destructive command: %w", err)
	}
	report.DirectDuration = time.Since(directStart)
	if _, err := os.Stat(filepath.Join(directRepo, "tracked.txt")); os.IsNotExist(err) {
		report.DirectRepoDamaged = true
	} else if err != nil {
		return nil, fmt.Errorf("check direct repo damage: %w", err)
	}

	futureStart := time.Now()
	record, err := r.Gateway.Run(ctx, futureRepo, []string{"/bin/sh", "-c", command})
	report.FutureDiffDuration = time.Since(futureStart)
	if err != nil {
		return nil, fmt.Errorf("run contained destructive command: %w", err)
	}
	report.FutureDiffState = record.State
	if _, err := os.Stat(filepath.Join(futureRepo, "tracked.txt")); os.IsNotExist(err) {
		report.FutureDiffRepoDamaged = true
	} else if err != nil {
		return nil, fmt.Errorf("check futurediff repo damage: %w", err)
	}
	_, patch, err := r.Gateway.Inspect(futureRepo, record.ID)
	if err != nil {
		return nil, fmt.Errorf("inspect contained patch: %w", err)
	}
	report.StagedPatchContainsDelete = strings.Contains(patch, "deleted file mode") || strings.Contains(patch, "--- a/tracked.txt")

	return report, nil
}

func initTrackedGitRepo(prefix string) (string, error) {
	repo, err := initGitRepo(prefix)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("safe contents\n"), 0o644); err != nil {
		_ = os.RemoveAll(repo)
		return "", fmt.Errorf("write tracked file: %w", err)
	}
	if _, err := gitOutput(repo, "add", "tracked.txt"); err != nil {
		_ = os.RemoveAll(repo)
		return "", err
	}
	if _, err := gitOutput(repo, "commit", "-m", "add tracked file"); err != nil {
		_ = os.RemoveAll(repo)
		return "", err
	}
	return repo, nil
}
