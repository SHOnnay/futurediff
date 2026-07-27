package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/staging"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type Report struct {
	FormatVersion    string `json:"format_version"`
	TransactionID    string `json:"transaction_id"`
	FinalStatus      string `json:"final_status"`
	LiveValue        string `json:"live_value"`
	FutureValue      string `json:"future_value"`
	FutureRef        string `json:"future_ref"`
	LiveCheckoutSafe bool   `json:"live_checkout_safe"`
	ReportPath       string `json:"report_path,omitempty"`
}

func Run(root string) (Report, error) {
	if root == "" {
		var err error
		root, err = os.MkdirTemp("", "futurediff-demo-")
		if err != nil {
			return Report{}, err
		}
	} else if err := os.MkdirAll(root, 0o700); err != nil {
		return Report{}, err
	}
	repoPath := filepath.Join(root, "repository")
	dataRoot := filepath.Join(root, "state")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		return Report{}, err
	}
	if err := os.MkdirAll(repoPath, 0o700); err != nil {
		return Report{}, err
	}
	if err := runGit(repoPath, "init", "-b", "main"); err != nil {
		return Report{}, err
	}
	if err := runGit(repoPath, "config", "user.name", "FutureDiff Demo"); err != nil {
		return Report{}, err
	}
	if err := runGit(repoPath, "config", "user.email", "demo@futurediff.local"); err != nil {
		return Report{}, err
	}
	if err := os.WriteFile(filepath.Join(repoPath, "message.txt"), []byte("current reality\n"), 0o600); err != nil {
		return Report{}, err
	}
	if err := runGit(repoPath, "add", "message.txt"); err != nil {
		return Report{}, err
	}
	if err := runGit(repoPath, "commit", "-m", "initial reality"); err != nil {
		return Report{}, err
	}

	ledgerRepo, err := ledger.OpenRepository(filepath.Join(dataRoot, "ledger.db"))
	if err != nil {
		return Report{}, err
	}
	defer ledgerRepo.Close()
	svc := &app.Service{Ledger: ledgerRepo, Staging: staging.Manager{RuntimeRoot: filepath.Join(dataRoot, "runtime")}, Verifier: verification.Engine{AllowLocalCommands: false}, CoordinatorID: "demo"}
	view, err := svc.Create(app.CreateRequest{Repository: repoPath, Mode: "cooperative", PolicyVersion: "demo-policy-0.1", AgentAdapter: "futurediff-demo"})
	if err != nil {
		return Report{}, err
	}
	if err := os.WriteFile(filepath.Join(view.Workspace.WorkspacePath, "message.txt"), []byte("approved future\n"), 0o600); err != nil {
		return Report{}, err
	}
	if _, err := svc.Seal(view.Transaction.ID); err != nil {
		return Report{}, err
	}
	contract := verification.Contract{FormatVersion: "0.1", ContractID: "demo-contract", PolicyVersion: "demo-policy-0.1", Checks: []verification.Check{{CheckID: "message-exists", Required: true, Executor: "workspace_assertion", Type: "file_exists", Path: "message.txt"}}}
	verified, err := svc.Verify(view.Transaction.ID, contract)
	if err != nil {
		return Report{}, err
	}
	if string(verified.Transaction.Status) != "ready" {
		return Report{}, fmt.Errorf("verification did not produce ready state: %s", verified.Transaction.Status)
	}
	material, err := svc.ApprovalMaterial(view.Transaction.ID)
	if err != nil {
		return Report{}, err
	}
	digest := material["transaction_digest"]
	if _, err := svc.Approve(view.Transaction.ID, digest, "demo-user"); err != nil {
		return Report{}, err
	}
	committed, err := svc.CommitContext(context.Background(), view.Transaction.ID, digest)
	if err != nil {
		return Report{}, err
	}
	futureRef := "refs/heads/futurediff/" + view.Transaction.ID
	live, err := os.ReadFile(filepath.Join(repoPath, "message.txt"))
	if err != nil {
		return Report{}, err
	}
	future, err := gitOutput(repoPath, "show", futureRef+":message.txt")
	if err != nil {
		return Report{}, err
	}
	report := Report{FormatVersion: "0.1", TransactionID: view.Transaction.ID, FinalStatus: string(committed.Transaction.Status), LiveValue: string(live), FutureValue: future, FutureRef: futureRef, LiveCheckoutSafe: string(live) == "current reality\n" && future == "approved future\n"}
	reportPath := filepath.Join(root, "demo-report.json")
	encoded, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(reportPath, append(encoded, '\n'), 0o600); err != nil {
		return Report{}, err
	}
	report.ReportPath = reportPath
	return report, nil
}

func runGit(dir string, args ...string) error {
	_, err := gitOutput(dir, args...)
	return err
}
func gitOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %v: %w: %s", args, err, string(out))
	}
	return string(out), nil
}
