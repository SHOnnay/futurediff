package ledgerrestore

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

// Stable post-restore comparison states. These strings are part of the
// restore report contract; scripts must match on them, so they never change
// silently.
const (
	// StateNoExternalEffect: the effect (or transaction) has no external
	// mutation recorded at all — it was never dispatched.
	StateNoExternalEffect = "no_external_effect"
	// StateKnownAbsent: durable attempts prove the provider effect is absent
	// (a status query found nothing) or was rejected (definite failure).
	StateKnownAbsent = "known_absent"
	// StateKnownPresent: the effect is committed and a durable provider
	// receipt proves it was observed.
	StateKnownPresent = "known_present"
	// StateReconciliationRequired: the transaction is mid-commit or in a
	// reconciliation state, so the canonical recovery command must run before
	// further publication.
	StateReconciliationRequired = "reconciliation_required"
	// StateExternalStateAmbiguous: the effect was dispatched but its outcome
	// is unknown from durable evidence (transport error, in-flight attempt,
	// or the effect is newer than the backup and no longer tracked).
	StateExternalStateAmbiguous = "external_state_ambiguous"
	// StateEvidenceUnavailable: the durable evidence is missing, unreadable,
	// or internally inconsistent. Absence is never assumed from this state.
	StateEvidenceUnavailable = "evidence_unavailable"
)

// Stable comparison reason codes (additive only).
const (
	ReasonNeverDispatched            = "never_dispatched"
	ReasonProviderAbsent             = "provider_absent"
	ReasonProviderRejected           = "provider_rejected"
	ReasonReceipt                    = "receipt"
	ReasonDispatchOutcomeUnknown     = "dispatch_outcome_unknown"
	ReasonInFlight                   = "in_flight"
	ReasonReceiptMissing             = "receipt_missing"
	ReasonReceiptUnreadable          = "receipt_unreadable"
	ReasonConflictingReceipt         = "conflicting_receipt"
	ReasonLedgerUnreadable           = "ledger_unreadable"
	ReasonPreRestoreLedgerUnreadable = "pre_restore_ledger_unreadable"
	ReasonNewerThanBackup            = "newer_than_backup"
	ReasonManualIntervention         = "manual_intervention"
	ReasonUnhandledState             = "unhandled_state"
	ReasonRequiresCanonicalRecovery  = "requires_canonical_recovery"
)

// Human summary strings (stable; the CLI prints them verbatim).
const (
	HumanNone           = "restore complete; no external reconciliation identified"
	HumanKnownEffects   = "restore complete; known external effects were found"
	HumanReconciliation = "restore complete; reconciliation is required before further publication"
	HumanEvidenceGap    = "restore complete; external-effect evidence is insufficient"
)

// EffectStatus is one restored effect's durable classification.
type EffectStatus struct {
	EffectID      string `json:"effect_id"`
	TransactionID string `json:"transaction_id"`
	State         string `json:"state"`
	Reason        string `json:"reason"`
}

// EffectReconciliation is the additive post-restore external-effect
// comparison section of the restore report. It is derived exclusively from
// durable ledger evidence (effects, attempts, receipts) read from private
// snapshot copies; it never contacts providers and never mutates anything.
type EffectReconciliation struct {
	EvaluatedAt              time.Time      `json:"evaluated_at"`
	AffectedTransactionIDs   []string       `json:"affected_transaction_ids"`
	AffectedEffectIDs        []string       `json:"affected_effect_ids"`
	ReconciliationRequired   bool           `json:"reconciliation_required"`
	ReconciliationCount      int            `json:"reconciliation_required_count,omitempty"`
	KnownAbsentCount         int            `json:"known_absent_count"`
	KnownPresentCount        int            `json:"known_present_count"`
	AmbiguousCount           int            `json:"ambiguous_count"`
	EvidenceUnavailableCount int            `json:"evidence_unavailable_count"`
	NoExternalEffectCount    int            `json:"no_external_effect_count"`
	NewerThanBackupCount     int            `json:"newer_than_backup_count"`
	NewerThanBackupEffectIDs []string       `json:"newer_than_backup_effect_ids,omitempty"`
	AutomaticResumeAllowed   bool           `json:"automatic_resume_allowed"`
	RecommendedAction        string         `json:"recommended_action"`
	HumanSummary             string         `json:"human_summary"`
	RecoveryCommands         []string       `json:"recovery_commands,omitempty"`
	Effects                  []EffectStatus `json:"effects,omitempty"`
}

// evaluateExternalEffects compares the restored ledger's external effects
// against durable receipts and attempts and detects effects newer than the
// backup from the preserved pre-restore ledger. It is read-only and
// idempotent: it snapshots both ledgers into private copies and never opens
// the authoritative paths. A state that cannot be proved is reported as
// evidence_unavailable; absence is never assumed.
func evaluateExternalEffects(live, quarantineDir string) *EffectReconciliation {
	out := &EffectReconciliation{EvaluatedAt: time.Now().UTC()}
	restored, cleanup, err := openSnapshotCopy(live)
	if err != nil {
		out.addEvidenceGap("", "", ReasonLedgerUnreadable)
		out.finish()
		return out
	}
	defer cleanup()

	restoredTx, err := restored.TransactionsWithEffects()
	if err != nil {
		_ = restored.Close()
		out.addEvidenceGap("", "", ReasonLedgerUnreadable)
		out.finish()
		return out
	}
	restoredEffects := map[string]EffectStatus{}
	recoverableTx := map[string]domain.TransactionState{}
	for _, tx := range restoredTx {
		if tx.Status == domain.StateCommitting || tx.Status == domain.StateNeedsReconciliation || tx.Status == domain.StateManualIntervention {
			recoverableTx[tx.ID] = tx.Status
		}
		effects, txErr := restored.ExternalEffects(tx.ID)
		if txErr != nil {
			out.addEvidenceGap("", tx.ID, ReasonLedgerUnreadable)
			continue
		}
		attempts, txErr := restored.EffectAttempts(tx.ID)
		if txErr != nil {
			out.addEvidenceGap("", tx.ID, ReasonLedgerUnreadable)
			continue
		}
		for _, eff := range effects {
			receipt, receiptErr := receiptFor(restored, eff.EffectID)
			status := classifyEffect(eff, tx.Status, receipt, receiptErr, attempts)
			restoredEffects[eff.EffectID] = status
		}
	}
	_ = restored.Close()

	// Effects present in the preserved pre-restore ledger but absent from the
	// restored ledger are newer than the backup. Their external state is
	// unknown and awareness of them must not be silently erased.
	if quarantineDir != "" {
		qPath := filepath.Join(quarantineDir, "ledger.db")
		if _, statErr := os.Lstat(qPath); statErr == nil {
			before, qCleanup, qErr := openSnapshotCopy(qPath)
			if qErr != nil {
				out.addEvidenceGap("", "", ReasonPreRestoreLedgerUnreadable)
			} else {
				qTx, listErr := before.TransactionsWithEffects()
				if listErr != nil {
					out.addEvidenceGap("", "", ReasonPreRestoreLedgerUnreadable)
				} else {
					for _, tx := range qTx {
						effects, fxErr := before.ExternalEffects(tx.ID)
						if fxErr != nil {
							out.addEvidenceGap("", tx.ID, ReasonPreRestoreLedgerUnreadable)
							continue
						}
						for _, eff := range effects {
							if _, seen := restoredEffects[eff.EffectID]; seen {
								continue
							}
							status := EffectStatus{EffectID: eff.EffectID, TransactionID: tx.ID, State: StateExternalStateAmbiguous, Reason: ReasonNewerThanBackup}
							restoredEffects[eff.EffectID] = status
							out.NewerThanBackupCount++
							out.NewerThanBackupEffectIDs = append(out.NewerThanBackupEffectIDs, eff.EffectID)
						}
					}
				}
				_ = before.Close()
				qCleanup()
			}
		}
	}
	sort.Strings(out.NewerThanBackupEffectIDs)

	for _, status := range restoredEffects {
		out.Effects = append(out.Effects, status)
	}
	sort.Slice(out.Effects, func(i, j int) bool {
		if out.Effects[i].TransactionID != out.Effects[j].TransactionID {
			return out.Effects[i].TransactionID < out.Effects[j].TransactionID
		}
		return out.Effects[i].EffectID < out.Effects[j].EffectID
	})
	out.finish()
	// Recovery commands for the affected transactions that the canonical
	// recovery path accepts (committing or needs_reconciliation). A
	// manual_intervention transaction needs inspection first, so its command
	// is a status read, never an automatic action.
	seenCmd := map[string]bool{}
	for _, txID := range out.AffectedTransactionIDs {
		state, ok := recoverableTx[txID]
		if !ok {
			continue
		}
		cmd := "fdif status " + txID
		if state == domain.StateCommitting || state == domain.StateNeedsReconciliation {
			cmd = "fdif recover " + txID + " --yes"
		}
		if !seenCmd[cmd] {
			seenCmd[cmd] = true
			out.RecoveryCommands = append(out.RecoveryCommands, cmd)
		}
	}
	sort.Strings(out.RecoveryCommands)
	return out
}

// receiptFor reads one effect's durable receipt from the snapshot repository,
// returning (nil, err) when it is missing or unreadable. Read-only.
func receiptFor(repo *ledger.Repository, effectID string) (*domain.EffectReceipt, error) {
	receipt, err := repo.EffectReceipt(effectID)
	if err != nil {
		return nil, err
	}
	return &receipt, nil
}

// classifyEffect derives the durable state of one restored effect from its
// recorded status, its receipt, and its attempts. It is pure and
// deterministic: no provider is consulted, and any evidence that is missing,
// unreadable, or internally inconsistent fails closed to
// evidence_unavailable.
func classifyEffect(eff domain.ExternalEffect, txStatus domain.TransactionState, receipt *domain.EffectReceipt, receiptErr error, attempts []domain.EffectAttempt) EffectStatus {
	status := EffectStatus{EffectID: eff.EffectID, TransactionID: eff.TransactionID}
	var latest *domain.EffectAttempt
	if len(attempts) > 0 {
		latest = &attempts[len(attempts)-1]
	}
	switch eff.Status {
	case domain.EffectCommitted:
		switch {
		case receipt != nil:
			status.State = StateKnownPresent
			status.Reason = ReasonReceipt
		case errors.Is(receiptErr, ledger.ErrNotFound):
			status.State = StateEvidenceUnavailable
			status.Reason = ReasonReceiptMissing
		default:
			status.State = StateEvidenceUnavailable
			status.Reason = ReasonReceiptUnreadable
		}
	case domain.EffectUnknown, domain.EffectCommitting:
		classifyFromAttempt(latest, receipt, receiptErr, &status)
	case domain.EffectVerified, domain.EffectPrepared, domain.EffectDiscovered, domain.EffectPreparing, domain.EffectAborted:
		if latest == nil {
			status.State = StateNoExternalEffect
			status.Reason = ReasonNeverDispatched
		} else {
			classifyFromAttempt(latest, receipt, receiptErr, &status)
		}
	case domain.EffectFailed:
		if latest != nil && latest.Outcome == "definite_failure" {
			status.State = StateKnownAbsent
			status.Reason = ReasonProviderRejected
		} else {
			status.State = StateExternalStateAmbiguous
			status.Reason = ReasonUnhandledState
		}
	case domain.EffectManual:
		status.State = StateExternalStateAmbiguous
		status.Reason = ReasonManualIntervention
	default:
		// compensating/compensated/superseded or any future state: never
		// assume absence from an unhandled durable state.
		status.State = StateExternalStateAmbiguous
		status.Reason = ReasonUnhandledState
	}
	// A receipt present for a non-committed effect is internally inconsistent
	// durable evidence; fail closed rather than trusting either side.
	if status.State != StateKnownPresent && receipt != nil {
		status.State = StateEvidenceUnavailable
		status.Reason = ReasonConflictingReceipt
	}
	// Transaction-state semantics from durable transaction status:
	// - needs_reconciliation: the canonical recovery command is the required
	//   next step for any effect whose fate is not already determinate.
	// - committing: a never-attempted effect sits in the dispatch window; it
	//   must not be reported as having no external effect.
	switch txStatus {
	case domain.StateNeedsReconciliation:
		if status.State == StateNoExternalEffect || status.State == StateExternalStateAmbiguous {
			status.State = StateReconciliationRequired
			status.Reason = ReasonRequiresCanonicalRecovery
		}
	case domain.StateCommitting:
		if status.State == StateNoExternalEffect {
			status.State = StateExternalStateAmbiguous
			status.Reason = ReasonInFlight
		}
	}
	return status
}

func classifyFromAttempt(latest *domain.EffectAttempt, receipt *domain.EffectReceipt, receiptErr error, status *EffectStatus) {
	if latest == nil {
		status.State = StateExternalStateAmbiguous
		status.Reason = ReasonInFlight
		return
	}
	switch latest.Outcome {
	case "success":
		if receipt != nil {
			status.State = StateKnownPresent
			status.Reason = ReasonReceipt
		} else if errors.Is(receiptErr, ledger.ErrNotFound) {
			status.State = StateEvidenceUnavailable
			status.Reason = ReasonReceiptMissing
		} else {
			status.State = StateEvidenceUnavailable
			status.Reason = ReasonReceiptUnreadable
		}
	case "not_found":
		status.State = StateKnownAbsent
		status.Reason = ReasonProviderAbsent
	case "definite_failure":
		status.State = StateKnownAbsent
		status.Reason = ReasonProviderRejected
	case "unknown":
		status.State = StateExternalStateAmbiguous
		status.Reason = ReasonDispatchOutcomeUnknown
	default:
		status.State = StateExternalStateAmbiguous
		status.Reason = ReasonInFlight
	}
}

// addEvidenceGap records a synthetic evidence-unavailable entry (an empty
// effect ID marks a ledger-level failure).
func (r *EffectReconciliation) addEvidenceGap(effectID, txID, reason string) {
	r.Effects = append(r.Effects, EffectStatus{EffectID: effectID, TransactionID: txID, State: StateEvidenceUnavailable, Reason: reason})
}

// finish aggregates counts, affected lists, the human summary, and the
// recommended action from the classified effects. It is called once per
// evaluation after all effects (restored and newer-than-backup) are known.
func (r *EffectReconciliation) finish() {
	affected := 0
	hasEvidenceGap := false
	hasAmbiguous := false
	hasRecovery := false
	hasNewer := false
	hasKnownPresent := false
	hasKnownAbsent := false
	txSeen := map[string]bool{}
	effectSeen := map[string]bool{}
	for _, status := range r.Effects {
		switch status.State {
		case StateNoExternalEffect:
			r.NoExternalEffectCount++
			continue
		case StateReconciliationRequired:
			r.ReconciliationCount++
			hasRecovery = true
		case StateKnownAbsent:
			r.KnownAbsentCount++
			hasKnownAbsent = true
		case StateKnownPresent:
			r.KnownPresentCount++
			hasKnownPresent = true
		case StateExternalStateAmbiguous:
			r.AmbiguousCount++
			if status.Reason == ReasonNewerThanBackup {
				hasNewer = true
			} else {
				hasAmbiguous = true
			}
		default:
			r.EvidenceUnavailableCount++
			hasEvidenceGap = true
		}
		affected++
		if status.TransactionID != "" {
			txSeen[status.TransactionID] = true
		}
		if status.EffectID != "" {
			effectSeen[status.EffectID] = true
		}
	}
	for txID := range txSeen {
		r.AffectedTransactionIDs = append(r.AffectedTransactionIDs, txID)
	}
	for effectID := range effectSeen {
		r.AffectedEffectIDs = append(r.AffectedEffectIDs, effectID)
	}
	sort.Strings(r.AffectedTransactionIDs)
	sort.Strings(r.AffectedEffectIDs)

	r.AutomaticResumeAllowed = affected == 0
	r.ReconciliationRequired = affected > 0

	switch {
	case hasEvidenceGap:
		r.HumanSummary = HumanEvidenceGap
		r.RecommendedAction = "manual review required: durable external-effect evidence is insufficient; verify provider state before any retry"
	case hasAmbiguous || hasNewer || hasRecovery:
		r.HumanSummary = HumanReconciliation
		r.RecommendedAction = "run the canonical recovery command for each affected change before further publication"
	case hasKnownPresent:
		r.HumanSummary = HumanKnownEffects
		r.RecommendedAction = "known external effects were found; do not re-run publication automatically; inspect each affected change before further publication"
	case hasKnownAbsent:
		r.HumanSummary = HumanKnownEffects
		r.RecommendedAction = "affected effects were proved absent; retry only through the canonical recovery path"
	default:
		r.HumanSummary = HumanNone
		r.RecommendedAction = ""
	}
}
