package guidedcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Stable recovery reason codes. These strings are part of the guided CLI
// contract (ADR-098); scripts must match on them, so they never change
// silently.
const (
	reasonNoTransactions              = "no_transactions"
	reasonMultipleTransactions        = "multiple_transactions"
	reasonSelectionTransactionMissing = "selection_transaction_missing"
	reasonStaleSelection              = "stale_selection"
	reasonTerminalSelection           = "terminal_selection"
	reasonInvalidSelectionFile        = "invalid_selection_file"
	reasonWorkspaceMissing            = "workspace_missing"
	reasonWorkspaceIdentityMismatch   = "workspace_identity_mismatch"
	reasonRecoveryRequired            = "recovery_required"
	reasonRecoveryAmbiguous           = "recovery_ambiguous"
	reasonDaemonUnavailable           = "daemon_unavailable"
	reasonNoRecoveryNeeded            = "no_recovery_needed"
	reasonRecovered                   = "recovered"
)

// RecoveryReport is the stable JSON contract for fdif recover. Fields are
// fixed; new fields are additive only.
type RecoveryReport struct {
	Kind               string `json:"kind"`
	ReasonCode         string `json:"reason_code"`
	TransactionID      string `json:"transaction_id,omitempty"`
	CurrentStatus      string `json:"current_status,omitempty"`
	RecoveryRequired   bool   `json:"recovery_required"`
	SafeToRetry        bool   `json:"safe_to_retry"`
	RecommendedAction  string `json:"recommended_action"`
	WorkspaceAvailable bool   `json:"workspace_available"`
	SelectionRepaired  bool   `json:"selection_repaired"`
}

func recoveryReport(reason, id, status, action string, required, safe, workspace bool) RecoveryReport {
	return RecoveryReport{
		Kind:               "recovery_report",
		ReasonCode:         reason,
		TransactionID:      id,
		CurrentStatus:      status,
		RecoveryRequired:   required,
		SafeToRetry:        safe,
		RecommendedAction:  action,
		WorkspaceAvailable: workspace,
	}
}

func (a *App) emitRecoveryReport(report RecoveryReport) error {
	if a.JSON {
		return writeJSON(a.Out, report)
	}
	a.Renderer.title("Recovery")
	status := report.CurrentStatus
	if status == "" {
		status = "unknown"
	}
	a.Renderer.fields(
		[2]string{"Change ID", report.TransactionID},
		[2]string{"Status", status},
		[2]string{"Reason", report.ReasonCode},
		[2]string{"Recovery required", fmt.Sprintf("%t", report.RecoveryRequired)},
		[2]string{"Safe to retry", fmt.Sprintf("%t", report.SafeToRetry)},
	)
	if report.WorkspaceAvailable {
		a.Renderer.fields([2]string{"Safe working copy", "present"})
	}
	if report.RecommendedAction != "" {
		fmt.Fprintln(a.Out)
		a.Renderer.next(report.RecommendedAction)
	}
	return nil
}

func (a *App) recoverCommand(ctx context.Context, args []string) error {
	explicit := firstPositional(args)
	yes := a.Yes || contains(args, "--yes")

	// Canonical recovery requires the daemon; report early with an
	// actionable reason instead of a raw socket error.
	if err := a.Daemon.Status(ctx); err != nil {
		return a.emitRecoveryReport(recoveryReport(
			reasonDaemonUnavailable, explicit, "",
			"fdif daemon start", false, true, false,
		))
	}

	// Resolve the transaction under recovery. An explicit ID wins; the
	// stored selection is validated against the daemon and never silently
	// replaced.
	id, report, err := a.resolveRecoveryTarget(ctx, explicit, yes)
	if err != nil {
		return err
	}
	if report != nil {
		return a.emitRecoveryReport(*report)
	}
	if id == "" {
		return errors.New("no change selected for recovery")
	}

	_, response, err := a.get(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "not_found") || strings.Contains(err.Error(), "transaction_not_found") {
			if yes && explicit == "" {
				_ = a.Store.Clear()
				return a.emitRecoveryReport(recoveryReport(
					reasonSelectionTransactionMissing, id, "",
					"fdif use <transaction-id>", false, true, false,
				))
			}
			return a.emitRecoveryReport(recoveryReport(
				reasonSelectionTransactionMissing, id, "",
				"fdif use <transaction-id>", false, true, false,
			))
		}
		return err
	}
	tx := response.Transaction
	status := ""
	if tx != nil {
		status = tx.Status
	}

	switch status {
	case "committing", "needs_reconciliation":
		return a.runCanonicalRecovery(ctx, id, response, yes)
	case "committed", "complete", "aborted", "compensated":
		return a.reportTerminalSelection(id, status, yes)
	case "manual_intervention":
		return a.emitRecoveryReport(recoveryReport(
			reasonRecoveryAmbiguous, id, status,
			"inspect the change with fdif status "+id+" and reconcile provider effects manually",
			true, false, workspaceAvailable(response),
		))
	case "aborting", "compensating":
		return a.emitRecoveryReport(recoveryReport(
			reasonRecoveryAmbiguous, id, status,
			"the change is mid-flight; wait and re-run fdif recover "+id,
			true, true, workspaceAvailable(response),
		))
	case "created", "active", "sealed", "verifying", "failed_verification", "ready", "stale":
		available, workspaceReason := a.classifyWorkspace(ctx, response.Workspace)
		if !available {
			return a.reportWorkspaceLoss(id, status, workspaceReason, yes)
		}
		action := nextAction(status)
		if action == "" {
			action = "fdif status " + id
		}
		return a.emitRecoveryReport(recoveryReport(
			reasonNoRecoveryNeeded, id, status, action, false, true, true,
		))
	default:
		return a.emitRecoveryReport(recoveryReport(
			reasonRecoveryAmbiguous, id, status,
			"inspect the change with fdif status "+id,
			true, false, workspaceAvailable(response),
		))
	}
}

// resolveRecoveryTarget picks the transaction for recovery without ever
// silently choosing one: explicit IDs win, a valid stored selection is used
// as-is, and a missing selection only reports what exists.
func (a *App) resolveRecoveryTarget(ctx context.Context, explicit string, yes bool) (string, *RecoveryReport, error) {
	if explicit != "" {
		return explicit, nil, nil
	}
	current, err := a.Store.Load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return a.reportEligibleTransactions(ctx)
		}
		return "", pointerRecoveryReport(recoveryReport(
			reasonInvalidSelectionFile, "", "",
			"fdif use --clear, then fdif use <transaction-id>", false, false, false,
		)), nil
	}
	// Validate the stored selection against the daemon. If it no longer
	// resolves, we report rather than silently picking another change.
	if _, _, getErr := a.get(ctx, current.TransactionID); getErr != nil {
		if !strings.Contains(getErr.Error(), "not_found") && !strings.Contains(getErr.Error(), "transaction_not_found") {
			return "", nil, getErr
		}
		report := recoveryReport(
			reasonSelectionTransactionMissing, current.TransactionID, "",
			"fdif use --clear, then fdif use <transaction-id>", false, true, false,
		)
		if yes {
			// Explicit confirmation repairs only the stale pointer; the
			// canonical transaction evidence is untouched.
			if clearErr := a.Store.Clear(); clearErr == nil {
				report.SelectionRepaired = true
			}
		}
		return "", pointerRecoveryReport(report), nil
	}
	return current.TransactionID, nil, nil
}

// pointerRecoveryReport returns a pointer to a recovery report; the returned
// value is used only to distinguish "no report" (nil) from "report present".
func pointerRecoveryReport(report RecoveryReport) *RecoveryReport {
	return &report
}

// reportEligibleTransactions lists open changes when no selection exists. It
// never auto-selects for a risky recovery; the operator must pick explicitly.
func (a *App) reportEligibleTransactions(ctx context.Context) (string, *RecoveryReport, error) {
	raw, err := a.Engine.Run(ctx, "list")
	if err != nil {
		return "", nil, err
	}
	response, err := decodeResponse(raw)
	if err != nil {
		return "", nil, err
	}
	eligible := make([]Transaction, 0)
	for _, tx := range response.Transactions {
		if tx.Status != "aborted" && tx.Status != "committed" && tx.Status != "complete" {
			eligible = append(eligible, tx)
		}
	}
	if len(eligible) == 0 {
		return "", pointerRecoveryReport(recoveryReport(
			reasonNoTransactions, "", "",
			"fdif start", false, false, false,
		)), nil
	}
	if len(eligible) == 1 {
		return "", pointerRecoveryReport(recoveryReport(
			reasonNoRecoveryNeeded, eligible[0].TransactionID, eligible[0].Status,
			"fdif use "+eligible[0].TransactionID, false, true, false,
		)), nil
	}
	return "", pointerRecoveryReport(recoveryReport(
		reasonMultipleTransactions, "", "",
		"fdif use <transaction-id> to select the change to recover", false, false, false,
	)), nil
}

// runCanonicalRecovery invokes the daemon's authoritative recover endpoint
// after explicit confirmation. It never reimplements reconciliation; it only
// formats the canonical response.
func (a *App) runCanonicalRecovery(ctx context.Context, id string, response Response, yes bool) error {
	workspaceOK := workspaceAvailable(response)
	status := ""
	if response.Transaction != nil {
		status = response.Transaction.Status
	}
	if !yes {
		report := recoveryReport(
			reasonRecoveryRequired, id, status,
			"fdif recover "+id+" --yes", true, true, workspaceOK,
		)
		if a.JSON || !a.Interactive {
			return a.emitRecoveryReport(report)
		}
		ok, confirmErr := a.confirm("Run canonical recovery for change "+id+"?", "RECOVER")
		if confirmErr != nil {
			return confirmErr
		}
		if !ok {
			return errors.New("recovery declined")
		}
	}
	raw, err := a.Engine.Run(ctx, "recover", id)
	if err != nil {
		return err
	}
	final, err := decodeResponse(raw)
	if err != nil {
		return err
	}
	newStatus := ""
	if final.Transaction != nil {
		newStatus = final.Transaction.Status
	}
	// A successful recovery either completes the change or returns it to a
	// workable state; both outcomes are reported through the canonical view.
	report := recoveryReport(
		reasonRecovered, id, newStatus, nextAction(newStatus), false, false, workspaceAvailable(final),
	)
	if newStatus == "committed" || newStatus == "complete" {
		_ = a.Store.Clear()
	} else if newStatus == "ready" || newStatus == "stale" || newStatus == "active" {
		repo := ""
		if final.Workspace != nil {
			repo = final.Workspace.RepositoryRoot
		}
		_ = a.Store.Save(id, repo)
	}
	return a.emitRecoveryReport(report)
}

// reportTerminalSelection handles a selection that points at a finished
// change. The change is not incomplete; only the pointer is stale.
func (a *App) reportTerminalSelection(id, status string, yes bool) error {
	if yes {
		_ = a.Store.Clear()
	}
	report := recoveryReport(
		reasonTerminalSelection, id, status,
		"fdif use --clear, then fdif use <transaction-id>", false, true, false,
	)
	if yes {
		report.SelectionRepaired = true
	}
	return a.emitRecoveryReport(report)
}

// reportWorkspaceLoss classifies a missing or replaced safe working copy by
// transaction stage and never claims recovery of unsealed edits.
func (a *App) reportWorkspaceLoss(id, status, reason string, yes bool) error {
	action := "fdif abort " + id + " --yes"
	message := "the safe working copy is missing; unsealed edits are not recoverable"
	switch reason {
	case "identity_mismatch":
		message = "the path is present but is not the recorded safe working copy; refusing to operate on it"
	case "unreadable":
		message = "the safe working copy path cannot be inspected"
	}
	if status == "sealed" || status == "verifying" || status == "ready" || status == "stale" || status == "failed_verification" {
		message += "; sealed material is durable in the ledger but working-copy edits are not"
	}
	if reason == "identity_mismatch" && yes {
		action = "fdif abort " + id + " --yes (safe working copy replaced)"
	}
	code := reasonWorkspaceMissing
	if reason == "identity_mismatch" {
		code = reasonWorkspaceIdentityMismatch
	}
	return a.emitRecoveryReport(recoveryReport(
		code, id, status, action, false, false, false,
	).withMessage(message))
}

// classifyWorkspace reports whether the recorded safe working copy is present
// and is still the exact git worktree the daemon created.
func (a *App) classifyWorkspace(ctx context.Context, ws *Workspace) (bool, string) {
	if ws == nil || ws.WorkspacePath == "" {
		return false, "missing"
	}
	info, err := os.Lstat(ws.WorkspacePath)
	if os.IsNotExist(err) {
		return false, "missing"
	}
	if err != nil {
		return false, "unreadable"
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, "identity_mismatch"
	}
	common, err := gitOutput(ctx, a.GitBinary, ws.WorkspacePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return false, "identity_mismatch"
	}
	common = strings.TrimSpace(common)
	if !filepath.IsAbs(common) {
		common = filepath.Join(ws.WorkspacePath, common)
	}
	commonAbs, err := canonicalizeFilePath(common)
	if err != nil {
		return false, "identity_mismatch"
	}
	if ws.GitCommonDir != "" {
		recorded, recErr := canonicalizeFilePath(ws.GitCommonDir)
		if recErr != nil || recorded != commonAbs {
			return false, "identity_mismatch"
		}
	}
	return true, "available"
}

func workspaceAvailable(response Response) bool {
	if response.Workspace == nil || response.Workspace.WorkspacePath == "" {
		return false
	}
	info, err := os.Lstat(response.Workspace.WorkspacePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false
	}
	return true
}

func (r RecoveryReport) withMessage(message string) RecoveryReport {
	r.RecommendedAction = message + "; " + r.RecommendedAction
	return r
}
