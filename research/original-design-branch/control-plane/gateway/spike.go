package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/futurediff/futurediff/control-plane/domain"
)

const (
	transactionStateAwaitingApproval = "AWAITING_APPROVAL"
)

type SpikeService struct {
	Now             func() time.Time
	AfterPatchApply func(record *TransactionRecord) error
}

type CommandExecutor interface {
	Run(ctx context.Context, worktreePath string, command []string) ([]byte, error)
}

type RunOptions struct {
	Command         []string
	VerifyCommand   []string
	CommandExecutor CommandExecutor
	VerifyExecutor  CommandExecutor
}

type TransactionRecord struct {
	ID                     string         `json:"id"`
	RepoRoot               string         `json:"repo_root"`
	BaseRev                string         `json:"base_rev"`
	State                  string         `json:"state"`
	Command                []string       `json:"command"`
	VerificationCommand    []string       `json:"verification_command,omitempty"`
	VerificationOutputPath string         `json:"verification_output_path,omitempty"`
	WorktreePath           string         `json:"worktree_path"`
	PatchPath              string         `json:"patch_path"`
	LedgerPath             string         `json:"ledger_path"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
	Effects                []EffectRecord `json:"effects"`
}

type EffectRecord struct {
	ID                  string    `json:"id"`
	AdapterName         string    `json:"adapter_name"`
	Kind                string    `json:"kind"`
	State               string    `json:"state"`
	PreparedFingerprint string    `json:"prepared_fingerprint,omitempty"`
	EvidenceRef         string    `json:"evidence_ref,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type LedgerEntry struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	TransactionID string    `json:"transaction_id"`
	EffectID      string    `json:"effect_id,omitempty"`
	StateFrom     string    `json:"state_from,omitempty"`
	StateTo       string    `json:"state_to,omitempty"`
	Message       string    `json:"message,omitempty"`
	At            time.Time `json:"at"`
}

func (s SpikeService) Run(ctx context.Context, repoPath string, command []string) (*TransactionRecord, error) {
	return s.RunWithOptions(ctx, repoPath, RunOptions{Command: command})
}

func (s SpikeService) RunWithOptions(ctx context.Context, repoPath string, options RunOptions) (*TransactionRecord, error) {
	if len(options.Command) == 0 {
		return nil, errors.New("command is required")
	}

	repoRoot, err := gitOutput(repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}
	repoRoot = strings.TrimSpace(repoRoot)

	if err := ensureExclude(repoRoot); err != nil {
		return nil, err
	}

	baseRev, err := gitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("resolve base rev: %w", err)
	}
	baseRev = strings.TrimSpace(baseRev)

	now := s.now()
	txID, err := newID("tx")
	if err != nil {
		return nil, err
	}
	commandEffectID, err := newID("eff")
	if err != nil {
		return nil, err
	}
	filesystemEffectID, err := newID("eff")
	if err != nil {
		return nil, err
	}

	txDir := filepath.Join(repoRoot, ".futurediff", "transactions", txID)
	worktreePath := filepath.Join(repoRoot, ".futurediff", "worktrees", txID)
	patchPath := filepath.Join(txDir, "staged.patch")
	ledgerPath := filepath.Join(txDir, "ledger.jsonl")
	metaPath := filepath.Join(txDir, "transaction.json")
	verificationOutputPath := filepath.Join(txDir, "verification.log")

	if err := os.MkdirAll(txDir, 0o755); err != nil {
		return nil, fmt.Errorf("create transaction dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		return nil, fmt.Errorf("create worktree parent: %w", err)
	}

	record := &TransactionRecord{
		ID:                  txID,
		RepoRoot:            repoRoot,
		BaseRev:             baseRev,
		State:               string(domain.TransactionStateNew),
		Command:             append([]string(nil), options.Command...),
		VerificationCommand: append([]string(nil), options.VerifyCommand...),
		WorktreePath:        worktreePath,
		PatchPath:           patchPath,
		LedgerPath:          ledgerPath,
		CreatedAt:           now,
		UpdatedAt:           now,
		Effects: []EffectRecord{
			{
				ID:          commandEffectID,
				AdapterName: "runtime.command",
				Kind:        "command.execute",
				State:       string(domain.EffectStateDeclared),
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			{
				ID:          filesystemEffectID,
				AdapterName: "filesystem.patch",
				Kind:        "filesystem.patch",
				State:       string(domain.EffectStateDeclared),
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	}
	if len(options.VerifyCommand) > 0 {
		record.VerificationOutputPath = verificationOutputPath
	}

	if err := saveTransaction(metaPath, record); err != nil {
		return nil, err
	}
	if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: txID, StateFrom: string(domain.TransactionStateNew), StateTo: string(domain.TransactionStateActive), Message: "transaction started", At: now}); err != nil {
		return nil, err
	}
	record.State = string(domain.TransactionStateActive)
	record.UpdatedAt = s.now()

	if _, err := gitOutput(repoRoot, "worktree", "add", "--detach", "--force", worktreePath, baseRev); err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}

	record.Effects[0].State = string(domain.EffectStatePrepared)
	record.Effects[0].UpdatedAt = s.now()
	if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "effect_event", TransactionID: txID, EffectID: commandEffectID, Message: "command effect prepared", At: s.now()}); err != nil {
		return nil, err
	}

	output, err := resolveCommandExecutor(options.CommandExecutor).Run(ctx, worktreePath, options.Command)
	if err != nil {
		_ = appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "effect_event", TransactionID: txID, EffectID: commandEffectID, Message: fmt.Sprintf("command failed: %s", strings.TrimSpace(string(output))), At: s.now()})
		return nil, fmt.Errorf("run staged command: %w", err)
	}
	if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "effect_event", TransactionID: txID, EffectID: commandEffectID, Message: "command executed in staged worktree", At: s.now()}); err != nil {
		return nil, err
	}
	record.Effects[0].State = string(domain.EffectStateCommitted)
	record.Effects[0].EvidenceRef = ledgerPath
	record.Effects[0].UpdatedAt = s.now()

	if _, err := gitOutput(worktreePath, "add", "-A"); err != nil {
		return nil, fmt.Errorf("stage worktree changes: %w", err)
	}

	patch, err := gitOutput(worktreePath, "diff", "--cached", "--binary", "--full-index", baseRev)
	if err != nil {
		return nil, fmt.Errorf("build staged patch: %w", err)
	}
	if err := os.WriteFile(patchPath, []byte(patch), 0o644); err != nil {
		return nil, fmt.Errorf("write patch: %w", err)
	}
	fingerprint := sha256.Sum256([]byte(patch))
	record.Effects[1].State = string(domain.EffectStatePrepared)
	record.Effects[1].PreparedFingerprint = hex.EncodeToString(fingerprint[:])
	record.Effects[1].EvidenceRef = patchPath
	record.Effects[1].UpdatedAt = s.now()
	if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "effect_event", TransactionID: txID, EffectID: filesystemEffectID, Message: "staged patch captured", At: s.now()}); err != nil {
		return nil, err
	}

	if strings.TrimSpace(patch) == "" {
		if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: txID, StateFrom: string(domain.TransactionStateActive), StateTo: string(domain.TransactionStateAborted), Message: "no staged changes detected", At: s.now()}); err != nil {
			return nil, err
		}
		record.State = string(domain.TransactionStateAborted)
		record.UpdatedAt = s.now()
		if err := saveTransaction(metaPath, record); err != nil {
			return nil, err
		}
		return record, nil
	}

	if len(options.VerifyCommand) == 0 {
		if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: txID, StateFrom: string(domain.TransactionStateActive), StateTo: transactionStateAwaitingApproval, Message: "staged patch ready for inspect/commit", At: s.now()}); err != nil {
			return nil, err
		}
		record.State = transactionStateAwaitingApproval
		record.UpdatedAt = s.now()
		if err := saveTransaction(metaPath, record); err != nil {
			return nil, err
		}
		return record, nil
	}

	if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: txID, StateFrom: string(domain.TransactionStateActive), StateTo: string(domain.TransactionStateVerifying), Message: "running staged verification command", At: s.now()}); err != nil {
		return nil, err
	}
	record.State = string(domain.TransactionStateVerifying)
	record.UpdatedAt = s.now()
	if err := saveTransaction(metaPath, record); err != nil {
		return nil, err
	}

	verificationOutput, verificationErr := resolveCommandExecutor(options.VerifyExecutor).Run(ctx, worktreePath, options.VerifyCommand)
	if err := os.WriteFile(verificationOutputPath, verificationOutput, 0o644); err != nil {
		return nil, fmt.Errorf("write verification output: %w", err)
	}
	if verificationErr != nil {
		record.Effects[1].State = string(domain.EffectStateAborted)
		record.Effects[1].EvidenceRef = verificationOutputPath
		record.Effects[1].UpdatedAt = s.now()
		if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "verification_event", TransactionID: txID, EffectID: filesystemEffectID, Message: fmt.Sprintf("verification failed: %s", strings.TrimSpace(string(verificationOutput))), At: s.now()}); err != nil {
			return nil, err
		}
		if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: txID, StateFrom: string(domain.TransactionStateVerifying), StateTo: string(domain.TransactionStateAborting), Message: "verification failed; aborting transaction", At: s.now()}); err != nil {
			return nil, err
		}
		if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: txID, StateFrom: string(domain.TransactionStateAborting), StateTo: string(domain.TransactionStateAborted), Message: "transaction aborted after failed verification", At: s.now()}); err != nil {
			return nil, err
		}
		record.State = string(domain.TransactionStateAborted)
		record.UpdatedAt = s.now()
		if err := saveTransaction(metaPath, record); err != nil {
			return nil, err
		}
		return record, nil
	}

	record.Effects[1].State = string(domain.EffectStateVerified)
	record.Effects[1].EvidenceRef = verificationOutputPath
	record.Effects[1].UpdatedAt = s.now()
	if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "verification_event", TransactionID: txID, EffectID: filesystemEffectID, Message: "verification passed for staged patch", At: s.now()}); err != nil {
		return nil, err
	}
	if err := appendLedger(ledgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: txID, StateFrom: string(domain.TransactionStateVerifying), StateTo: transactionStateAwaitingApproval, Message: "staged patch verified and ready for inspect/commit", At: s.now()}); err != nil {
		return nil, err
	}
	record.State = transactionStateAwaitingApproval
	record.UpdatedAt = s.now()
	if err := saveTransaction(metaPath, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s SpikeService) Inspect(repoPath, transactionID string) (*TransactionRecord, string, error) {
	repoRoot, err := gitOutput(repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, "", fmt.Errorf("resolve repo root: %w", err)
	}
	repoRoot = strings.TrimSpace(repoRoot)
	record, err := loadTransaction(filepath.Join(repoRoot, ".futurediff", "transactions", transactionID, "transaction.json"))
	if err != nil {
		return nil, "", err
	}
	patchBytes, err := os.ReadFile(record.PatchPath)
	if err != nil {
		return nil, "", fmt.Errorf("read patch: %w", err)
	}
	return record, string(patchBytes), nil
}

func (s SpikeService) Commit(ctx context.Context, repoPath, transactionID string) (*TransactionRecord, error) {
	repoRoot, err := gitOutput(repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}
	repoRoot = strings.TrimSpace(repoRoot)
	metaPath := filepath.Join(repoRoot, ".futurediff", "transactions", transactionID, "transaction.json")
	record, err := loadTransaction(metaPath)
	if err != nil {
		return nil, err
	}
	if record.State != transactionStateAwaitingApproval {
		return nil, fmt.Errorf("transaction %s is not ready to commit: %s", record.ID, record.State)
	}

	record.State = string(domain.TransactionStateCommitting)
	record.UpdatedAt = s.now()
	if err := saveTransaction(metaPath, record); err != nil {
		return nil, err
	}
	if err := appendLedger(record.LedgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: record.ID, StateFrom: transactionStateAwaitingApproval, StateTo: string(domain.TransactionStateCommitting), Message: "commit started without rerunning staged command", At: s.now()}); err != nil {
		return nil, err
	}

	patchBytes, err := os.ReadFile(record.PatchPath)
	if err != nil {
		return nil, fmt.Errorf("read patch: %w", err)
	}
	if strings.TrimSpace(string(patchBytes)) != "" {
		cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "apply", "--whitespace=nowarn", record.PatchPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("apply staged patch: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if s.AfterPatchApply != nil {
		if err := s.AfterPatchApply(record); err != nil {
			return nil, err
		}
	}

	for i := range record.Effects {
		if record.Effects[i].Kind == "filesystem.patch" {
			record.Effects[i].State = string(domain.EffectStateCommitted)
			record.Effects[i].UpdatedAt = s.now()
		}
	}
	if err := appendLedger(record.LedgerPath, LedgerEntry{ID: mustID("evt"), Kind: "effect_event", TransactionID: record.ID, EffectID: filesystemEffectID(record), Message: "exact staged patch applied to source repo", At: s.now()}); err != nil {
		return nil, err
	}
	if err := appendLedger(record.LedgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: record.ID, StateFrom: string(domain.TransactionStateCommitting), StateTo: string(domain.TransactionStateCommitted), Message: "transaction committed from stored patch", At: s.now()}); err != nil {
		return nil, err
	}
	record.State = string(domain.TransactionStateCommitted)
	record.UpdatedAt = s.now()

	if err := saveTransaction(metaPath, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s SpikeService) Recover(repoPath, transactionID string) (*TransactionRecord, error) {
	repoRoot, err := gitOutput(repoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("resolve repo root: %w", err)
	}
	repoRoot = strings.TrimSpace(repoRoot)
	metaPath := filepath.Join(repoRoot, ".futurediff", "transactions", transactionID, "transaction.json")
	record, err := loadTransaction(metaPath)
	if err != nil {
		return nil, err
	}
	if record.State != string(domain.TransactionStateCommitting) {
		return nil, fmt.Errorf("transaction %s is not in committing state: %s", record.ID, record.State)
	}

	applied, err := isPatchApplied(repoRoot, record.PatchPath)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, fmt.Errorf("transaction %s cannot be recovered deterministically because the staged patch is not applied", record.ID)
	}

	for i := range record.Effects {
		if record.Effects[i].Kind == "filesystem.patch" {
			record.Effects[i].State = string(domain.EffectStateCommitted)
			record.Effects[i].UpdatedAt = s.now()
		}
	}
	record.State = string(domain.TransactionStateCommitted)
	record.UpdatedAt = s.now()
	if err := appendLedger(record.LedgerPath, LedgerEntry{ID: mustID("evt"), Kind: "recovery_event", TransactionID: record.ID, EffectID: filesystemEffectID(record), Message: "recovery confirmed staged patch was already applied", At: s.now()}); err != nil {
		return nil, err
	}
	if err := appendLedger(record.LedgerPath, LedgerEntry{ID: mustID("evt"), Kind: "transaction_transition", TransactionID: record.ID, StateFrom: string(domain.TransactionStateCommitting), StateTo: string(domain.TransactionStateCommitted), Message: "recovery finalized commit from stored patch evidence", At: s.now()}); err != nil {
		return nil, err
	}
	if err := saveTransaction(metaPath, record); err != nil {
		return nil, err
	}
	return record, nil
}

func isPatchApplied(repoRoot, patchPath string) (bool, error) {
	cmd := exec.Command("git", "-C", repoRoot, "apply", "--check", "--reverse", patchPath)
	reverseOutput, reverseErr := cmd.CombinedOutput()
	if reverseErr == nil {
		return true, nil
	}
	if strings.TrimSpace(string(reverseOutput)) == "" {
		return false, fmt.Errorf("check reverse patch: %w", reverseErr)
	}

	forwardCmd := exec.Command("git", "-C", repoRoot, "apply", "--check", patchPath)
	forwardOutput, forwardErr := forwardCmd.CombinedOutput()
	if forwardErr == nil {
		return false, nil
	}
	if strings.TrimSpace(string(forwardOutput)) == "" {
		return false, fmt.Errorf("check forward patch: %w", forwardErr)
	}

	return false, nil
}
func filesystemEffectID(record *TransactionRecord) string {
	for _, effect := range record.Effects {
		if effect.Kind == "filesystem.patch" {
			return effect.ID
		}
	}
	return ""
}

func loadTransaction(path string) (*TransactionRecord, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read transaction record: %w", err)
	}
	var record TransactionRecord
	if err := json.Unmarshal(bytes, &record); err != nil {
		return nil, fmt.Errorf("decode transaction record: %w", err)
	}
	return &record, nil
}

func saveTransaction(path string, record *TransactionRecord) error {
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode transaction record: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write transaction record: %w", err)
	}
	return nil
}

func appendLedger(path string, entry LedgerEntry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode ledger entry: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open ledger: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append ledger: %w", err)
	}
	return nil
}

func gitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func runOutput(ctx context.Context, dir string, command []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	return cmd.CombinedOutput()
}

type shellCommandExecutor struct{}

func (shellCommandExecutor) Run(ctx context.Context, worktreePath string, command []string) ([]byte, error) {
	return runOutput(ctx, worktreePath, command)
}

func resolveCommandExecutor(executor CommandExecutor) CommandExecutor {
	if executor != nil {
		return executor
	}
	return shellCommandExecutor{}
}

func ensureExclude(repoRoot string) error {
	excludePath := filepath.Join(repoRoot, ".git", "info", "exclude")
	content, err := os.ReadFile(excludePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read git exclude: %w", err)
	}
	if strings.Contains(string(content), ".futurediff/") {
		return nil
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open git exclude: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString("\n.futurediff/\n"); err != nil {
		return fmt.Errorf("write git exclude: %w", err)
	}
	return nil
}

func newID(prefix string) (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(buf)), nil
}

func mustID(prefix string) string {
	id, err := newID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

func (s SpikeService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
