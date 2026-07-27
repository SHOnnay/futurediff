package retention

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
)

const Confirmation = "PRUNE_TERMINAL_FUTUREDIFF_ARTIFACTS"

type Candidate struct {
	TransactionID string `json:"transaction_id"`
	RuntimeRoot   string `json:"runtime_root"`
	Bytes         int64  `json:"bytes"`
	Files         int64  `json:"files"`
}

type Plan struct {
	DataRoot   string      `json:"data_root"`
	Before     time.Time   `json:"before"`
	Candidates []Candidate `json:"candidates"`
	TotalBytes int64       `json:"total_bytes"`
	Digest     string      `json:"digest"`
}

type Result struct {
	PlanDigest   string   `json:"plan_digest"`
	Applied      bool     `json:"applied"`
	Removed      int      `json:"removed"`
	BytesRemoved int64    `json:"bytes_removed"`
	Errors       []string `json:"errors,omitempty"`
}

func BuildPlan(repo *ledger.Repository, dataRoot string, before time.Time) (Plan, error) {
	absoluteRoot, err := filepath.Abs(dataRoot)
	if err != nil {
		return Plan{}, err
	}
	workspaces, err := repo.TerminalWorkspaces(before)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{DataRoot: absoluteRoot, Before: before.UTC()}
	expectedBase := filepath.Join(absoluteRoot, "runtime", "transactions")
	for _, workspace := range workspaces {
		runtimeRoot := filepath.Dir(workspace.WorkspacePath)
		if err := requireChild(expectedBase, runtimeRoot); err != nil {
			return Plan{}, fmt.Errorf("transaction %s: %w", workspace.TransactionID, err)
		}
		bytes, files, err := treeSize(runtimeRoot)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Plan{}, err
		}
		plan.Candidates = append(plan.Candidates, Candidate{TransactionID: workspace.TransactionID, RuntimeRoot: runtimeRoot, Bytes: bytes, Files: files})
		plan.TotalBytes += bytes
	}
	sort.Slice(plan.Candidates, func(i, j int) bool { return plan.Candidates[i].TransactionID < plan.Candidates[j].TransactionID })
	digest, err := domain.Digest(map[string]any{"format": "futurediff-retention-plan-v1", "data_root": plan.DataRoot, "before": plan.Before, "candidates": plan.Candidates})
	if err != nil {
		return Plan{}, err
	}
	plan.Digest = digest
	return plan, nil
}

func Apply(repo *ledger.Repository, plan Plan, confirmation string) (Result, error) {
	if confirmation != Confirmation {
		return Result{}, errors.New("exact retention confirmation is required")
	}
	result := Result{PlanDigest: plan.Digest, Applied: true}
	manager := staging.Manager{RuntimeRoot: filepath.Join(plan.DataRoot, "runtime")}
	for _, candidate := range plan.Candidates {
		workspace, err := repo.Workspace(candidate.TransactionID)
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		if filepath.Clean(filepath.Dir(workspace.WorkspacePath)) != filepath.Clean(candidate.RuntimeRoot) {
			result.Errors = append(result.Errors, "workspace changed after plan: "+candidate.TransactionID)
			continue
		}
		bytes, _, err := treeSize(candidate.RuntimeRoot)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		if err := manager.Abort(workspace); err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		if err := os.RemoveAll(candidate.RuntimeRoot); err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		if err := repo.RecordRetention(ledger.RetentionRecord{TransactionID: candidate.TransactionID, RuntimeRoot: candidate.RuntimeRoot, BytesRemoved: bytes, PlanDigest: plan.Digest}); err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.Removed++
		result.BytesRemoved += bytes
	}
	if len(result.Errors) > 0 {
		return result, errors.New("one or more retention actions failed")
	}
	return result, nil
}

func requireChild(base, candidate string) error {
	b, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	c, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(b, c)
	if err != nil {
		return err
	}
	if rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("runtime path %s is outside managed transaction root %s", c, b)
	}
	return nil
}

func treeSize(root string) (int64, int64, error) {
	var bytes, files int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			files++
			return nil
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files, err
}
