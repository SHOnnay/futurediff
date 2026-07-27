package smoke

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/futurediff/futurediff/control-plane/gateway"
)

type FileChangeFailureReport struct {
	DirectDuration        time.Duration
	FutureDiffDuration    time.Duration
	DirectRepoChanged     bool
	FutureDiffRepoChanged bool
	FutureDiffState       string
	DirectError           string
}

type Runner struct {
	Gateway gateway.SpikeService
}

func (r Runner) CompareFileChangeFailure(ctx context.Context) (*FileChangeFailureReport, error) {
	directRepo, err := initGitRepo("direct-benchmark")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directRepo)

	futureRepo, err := initGitRepo("futurediff-benchmark")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(futureRepo)

	report := &FileChangeFailureReport{}

	directStart := time.Now()
	directErr := exec.CommandContext(ctx, "/bin/sh", "-c", "printf 'direct unsafe future\n' > direct.txt; printf 'verification failed\n' >&2; exit 1")
	directErr.Dir = directRepo
	_, runErr := directErr.CombinedOutput()
	report.DirectDuration = time.Since(directStart)
	if runErr != nil {
		report.DirectError = runErr.Error()
	}
	if _, err := os.Stat(filepath.Join(directRepo, "direct.txt")); err == nil {
		report.DirectRepoChanged = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("check direct repo change: %w", err)
	}

	futureStart := time.Now()
	record, err := r.Gateway.RunWithOptions(ctx, futureRepo, gateway.RunOptions{
		Command:       []string{"/bin/sh", "-c", "printf 'futurediff guarded future\n' > guarded.txt"},
		VerifyCommand: []string{"/bin/sh", "-c", "printf 'verification failed\n' >&2; exit 1"},
	})
	report.FutureDiffDuration = time.Since(futureStart)
	if err != nil {
		return nil, fmt.Errorf("run futurediff guarded scenario: %w", err)
	}
	report.FutureDiffState = record.State
	if _, err := os.Stat(filepath.Join(futureRepo, "guarded.txt")); err == nil {
		report.FutureDiffRepoChanged = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("check futurediff repo change: %w", err)
	}

	return report, nil
}

func initGitRepo(prefix string) (string, error) {
	repo, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("create temp repo: %w", err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "benchmark@example.com"},
		{"config", "user.name", "Benchmark User"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		if _, err := gitOutput(repo, args...); err != nil {
			_ = os.RemoveAll(repo)
			return "", err
		}
	}
	return repo, nil
}

func gitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}
