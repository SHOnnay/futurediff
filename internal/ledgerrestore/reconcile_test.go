package ledgerrestore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

// reconcileEffect creates one verified (never dispatched) external effect on
// an active transaction via the public ledger API.
func reconcileEffect(t *testing.T, r *ledger.Repository, txID, effectID string, inputJSON string) {
	t.Helper()
	_, err := r.CreateExternalEffect(ledger.PrepareExternalEffectInput{Effect: domain.ExternalEffect{
		EffectID: effectID, TransactionID: txID, ToolIdentity: "reconcile-tool", AdapterIdentity: "reconcile-adapter",
		CredentialID: "reconcile-cred", Operation: "create_artifact", Destination: "https://api.example.invalid/artifacts",
		InputJSON: inputJSON, InputDigest: "input-digest-" + effectID, PreparedJSON: inputJSON, PreparedDigest: "prepared-digest-" + effectID,
		PreviewJSON: inputJSON, PreviewDigest: "preview-digest-" + effectID,
		IdempotencyKey: "reconcile-adapter:" + effectID, Status: domain.EffectVerified, Reversibility: "compensatable", CommitRank: 100, SupportLevel: "test",
	}})
	if err != nil {
		t.Fatal(err)
	}
}

// reconcileHome builds a fresh home whose live ledger is populated by build
// and then recorded as a backup. Post-backup mutations are made by reopening
// the live ledger, so the backup stays older than the live state.
func reconcileHome(t *testing.T, build func(t *testing.T, r *ledger.Repository, root string)) (root, live, backup string) {
	t.Helper()
	root = t.TempDir()
	live = filepath.Join(root, "live.db")
	l, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	build(t, l, root)
	backup = filepath.Join(root, "backup.db")
	if _, err := l.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return root, live, backup
}

func applyReconcile(t *testing.T, root, live, backup string) Report {
	t.Helper()
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	report, err := RunWithInjector(applyOptions(root, live, backup, dry.BackupSHA256), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatal("expected applied restore")
	}
	if report.EffectReconciliation == nil {
		t.Fatal("applied restore must carry an effect reconciliation")
	}
	return report
}

func quarantineDirOf(t *testing.T, report Report) string {
	t.Helper()
	if report.Preserved == nil {
		t.Fatal("expected preserved original")
	}
	if report.Preserved.QuarantineDir == "" {
		t.Fatal("expected quarantine directory")
	}
	return report.Preserved.QuarantineDir
}

// ---------------------------------------------------------------------------
// Classification unit tests (pure function over durable evidence).
// ---------------------------------------------------------------------------

func effectForClassify(txID, effectID string, status domain.EffectState) domain.ExternalEffect {
	return domain.ExternalEffect{EffectID: effectID, TransactionID: txID, Status: status}
}

func testReceipt(effectID string) *domain.EffectReceipt {
	return &domain.EffectReceipt{ReceiptID: "receipt-" + effectID, EffectID: effectID, RequestDigest: "req"}
}

func attempt(outcome string) domain.EffectAttempt {
	return domain.EffectAttempt{AttemptID: "attempt-1", EffectID: "eff", TransactionID: "tx", Phase: "commit", Outcome: outcome, FencingToken: 1, StartedAt: time.Now().UTC()}
}

func TestClassify_NoExternalEffect_NeverDispatched(t *testing.T) {
	for _, status := range []domain.EffectState{domain.EffectDiscovered, domain.EffectPreparing, domain.EffectPrepared, domain.EffectVerified, domain.EffectAborted} {
		s := classifyEffect(effectForClassify("tx", "eff", status), domain.StateActive, nil, ledger.ErrNotFound, nil)
		if s.State != StateNoExternalEffect || s.Reason != ReasonNeverDispatched {
			t.Fatalf("%s: got %s/%s, want no_external_effect/never_dispatched", status, s.State, s.Reason)
		}
	}
}

func TestClassify_KnownAbsent_ProvedAbsent(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectVerified), domain.StateActive, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("not_found")})
	if s.State != StateKnownAbsent || s.Reason != ReasonProviderAbsent {
		t.Fatalf("got %s/%s, want known_absent/provider_absent", s.State, s.Reason)
	}
}

func TestClassify_KnownAbsent_ProviderRejected(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectVerified), domain.StateActive, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("definite_failure")})
	if s.State != StateKnownAbsent || s.Reason != ReasonProviderRejected {
		t.Fatalf("got %s/%s, want known_absent/provider_rejected", s.State, s.Reason)
	}
}

func TestClassify_KnownPresent_CommittedWithReceipt(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitted), domain.StateActive, testReceipt("eff"), nil, nil)
	if s.State != StateKnownPresent || s.Reason != ReasonReceipt {
		t.Fatalf("got %s/%s, want known_present/receipt", s.State, s.Reason)
	}
}

func TestClassify_KnownPresent_RecoveredFromUnknown(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectUnknown), domain.StateActive, testReceipt("eff"), nil, []domain.EffectAttempt{attempt("success")})
	if s.State != StateKnownPresent || s.Reason != ReasonReceipt {
		t.Fatalf("got %s/%s, want known_present/receipt", s.State, s.Reason)
	}
}

func TestClassify_Ambiguous_TransportError(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectUnknown), domain.StateActive, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("unknown")})
	if s.State != StateExternalStateAmbiguous || s.Reason != ReasonDispatchOutcomeUnknown {
		t.Fatalf("got %s/%s, want external_state_ambiguous/dispatch_outcome_unknown", s.State, s.Reason)
	}
}

func TestClassify_Ambiguous_InFlightIntent(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitting), domain.StateActive, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("intent")})
	if s.State != StateExternalStateAmbiguous || s.Reason != ReasonInFlight {
		t.Fatalf("got %s/%s, want external_state_ambiguous/in_flight", s.State, s.Reason)
	}
}

func TestClassify_Ambiguous_NoAttempt(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitting), domain.StateActive, nil, ledger.ErrNotFound, nil)
	if s.State != StateExternalStateAmbiguous || s.Reason != ReasonInFlight {
		t.Fatalf("got %s/%s, want external_state_ambiguous/in_flight", s.State, s.Reason)
	}
}

func TestClassify_EvidenceUnavailable_MissingReceipt(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitted), domain.StateActive, nil, ledger.ErrNotFound, nil)
	if s.State != StateEvidenceUnavailable || s.Reason != ReasonReceiptMissing {
		t.Fatalf("got %s/%s, want evidence_unavailable/receipt_missing", s.State, s.Reason)
	}
}

func TestClassify_EvidenceUnavailable_MalformedReceipt(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitted), domain.StateActive, nil, errors.New("malformed receipt row"), nil)
	if s.State != StateEvidenceUnavailable || s.Reason != ReasonReceiptUnreadable {
		t.Fatalf("got %s/%s, want evidence_unavailable/receipt_unreadable", s.State, s.Reason)
	}
}

func TestClassify_EvidenceUnavailable_ConflictingReceipt(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectVerified), domain.StateActive, testReceipt("eff"), nil, nil)
	if s.State != StateEvidenceUnavailable || s.Reason != ReasonConflictingReceipt {
		t.Fatalf("got %s/%s, want evidence_unavailable/conflicting_receipt", s.State, s.Reason)
	}
}

func TestClassify_EvidenceUnavailable_SuccessAttemptWithoutReceipt(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectUnknown), domain.StateActive, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("success")})
	if s.State != StateEvidenceUnavailable || s.Reason != ReasonReceiptMissing {
		t.Fatalf("got %s/%s, want evidence_unavailable/receipt_missing", s.State, s.Reason)
	}
}

func TestClassify_ManualRequiresReconciliation(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectManual), domain.StateActive, nil, ledger.ErrNotFound, nil)
	if s.State != StateExternalStateAmbiguous || s.Reason != ReasonManualIntervention {
		t.Fatalf("got %s/%s, want external_state_ambiguous/manual_intervention", s.State, s.Reason)
	}
}

func TestClassify_UnhandledStatesFailClosed(t *testing.T) {
	for _, status := range []domain.EffectState{domain.EffectAborting, domain.EffectCompensating, domain.EffectCompensated, domain.EffectSuperseded} {
		s := classifyEffect(effectForClassify("tx", "eff", status), domain.StateActive, nil, ledger.ErrNotFound, nil)
		if s.State != StateExternalStateAmbiguous || s.Reason != ReasonUnhandledState {
			t.Fatalf("%s: got %s/%s, want external_state_ambiguous/unhandled_state", status, s.State, s.Reason)
		}
	}
}

func TestClassify_FailedWithDefiniteFailureIsAbsent(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectFailed), domain.StateActive, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("definite_failure")})
	if s.State != StateKnownAbsent || s.Reason != ReasonProviderRejected {
		t.Fatalf("got %s/%s, want known_absent/provider_rejected", s.State, s.Reason)
	}
}

func TestClassify_LatestAttemptWins(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectVerified), domain.StateActive, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("unknown"), attempt("not_found")})
	if s.State != StateKnownAbsent || s.Reason != ReasonProviderAbsent {
		t.Fatalf("got %s/%s, want known_absent/provider_absent from latest attempt", s.State, s.Reason)
	}
}

// ---------------------------------------------------------------------------
// Aggregation unit tests.
// ---------------------------------------------------------------------------

func TestReconciliationFinish_MixedStates(t *testing.T) {
	r := EffectReconciliation{Effects: []EffectStatus{
		{EffectID: "e1", TransactionID: "t1", State: StateKnownPresent, Reason: ReasonReceipt},
		{EffectID: "e2", TransactionID: "t1", State: StateKnownAbsent, Reason: ReasonProviderAbsent},
		{EffectID: "e3", TransactionID: "t2", State: StateExternalStateAmbiguous, Reason: ReasonDispatchOutcomeUnknown},
		{EffectID: "e4", TransactionID: "t2", State: StateEvidenceUnavailable, Reason: ReasonReceiptMissing},
		{EffectID: "e5", TransactionID: "t3", State: StateNoExternalEffect, Reason: ReasonNeverDispatched},
	}}
	r.finish()
	if r.KnownPresentCount != 1 || r.KnownAbsentCount != 1 || r.AmbiguousCount != 1 || r.EvidenceUnavailableCount != 1 || r.NoExternalEffectCount != 1 {
		t.Fatalf("counts wrong: %+v", r)
	}
	if len(r.AffectedTransactionIDs) != 2 || r.AffectedTransactionIDs[0] != "t1" || r.AffectedTransactionIDs[1] != "t2" {
		t.Fatalf("affected transactions wrong: %v", r.AffectedTransactionIDs)
	}
	if len(r.AffectedEffectIDs) != 4 {
		t.Fatalf("affected effects wrong: %v", r.AffectedEffectIDs)
	}
	if !r.ReconciliationRequired || r.AutomaticResumeAllowed {
		t.Fatalf("mixed states must require reconciliation and forbid auto-resume: %+v", r)
	}
	if r.HumanSummary != HumanEvidenceGap {
		t.Fatalf("evidence gap must dominate the summary, got %q", r.HumanSummary)
	}
}

func TestReconciliationFinish_NoAffectedEffects(t *testing.T) {
	r := EffectReconciliation{Effects: []EffectStatus{
		{EffectID: "e1", TransactionID: "t1", State: StateNoExternalEffect, Reason: ReasonNeverDispatched},
	}}
	r.finish()
	if r.NoExternalEffectCount != 1 || len(r.AffectedEffectIDs) != 0 || len(r.AffectedTransactionIDs) != 0 {
		t.Fatalf("no-external-effect state must not be affected: %+v", r)
	}
	if r.ReconciliationRequired || !r.AutomaticResumeAllowed {
		t.Fatalf("no affected effects must allow automatic resume: %+v", r)
	}
	if r.HumanSummary != HumanNone {
		t.Fatalf("got %q, want %q", r.HumanSummary, HumanNone)
	}
}

// ---------------------------------------------------------------------------
// Restore-flow integration tests (public ledger APIs only).
// ---------------------------------------------------------------------------

func TestRestore_Reconcile_NoExternalEffects(t *testing.T) {
	root, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", "")
		addTx(t, r, "tx-b", root)
		reconcileEffect(t, r, "tx-b", "eff-b1", "")
	})
	report := applyReconcile(t, root, live, backup)
	rec := report.EffectReconciliation
	if rec.NoExternalEffectCount != 2 {
		t.Fatalf("expected 2 no-external-effect classifications, got %d", rec.NoExternalEffectCount)
	}
	if len(rec.AffectedEffectIDs) != 0 || len(rec.AffectedTransactionIDs) != 0 {
		t.Fatalf("prepared-but-never-attempted effects must not be affected: %+v", rec)
	}
	if rec.ReconciliationRequired || !rec.AutomaticResumeAllowed {
		t.Fatalf("expected automatic resume allowed with no affected effects: %+v", rec)
	}
	if rec.HumanSummary != HumanNone || rec.RecommendedAction != "" {
		t.Fatalf("unexpected summary/action: %q / %q", rec.HumanSummary, rec.RecommendedAction)
	}
	if len(rec.RecoveryCommands) != 0 {
		t.Fatalf("no recovery commands expected: %v", rec.RecoveryCommands)
	}
	for _, status := range rec.Effects {
		if status.State != StateNoExternalEffect || status.Reason != ReasonNeverDispatched {
			t.Fatalf("unexpected classification: %+v", status)
		}
	}
}

func TestRestore_Reconcile_NewerThanBackupDetected(t *testing.T) {
	root, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", "")
	})
	// After the backup, a new transaction with a new effect appears in the
	// live ledger. The restore must not silently erase awareness of it.
	l, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "tx-new", root)
	reconcileEffect(t, l, "tx-new", "eff-new1", "")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	report := applyReconcile(t, root, live, backup)
	rec := report.EffectReconciliation
	if rec.AmbiguousCount != 1 || rec.NewerThanBackupCount != 1 {
		t.Fatalf("expected 1 newer-than-backup effect, got ambiguous=%d newer=%d", rec.AmbiguousCount, rec.NewerThanBackupCount)
	}
	if !reflect.DeepEqual(rec.NewerThanBackupEffectIDs, []string{"eff-new1"}) {
		t.Fatalf("newer effect ids wrong: %v", rec.NewerThanBackupEffectIDs)
	}
	if !reflect.DeepEqual(rec.AffectedEffectIDs, []string{"eff-new1"}) {
		t.Fatalf("affected effect ids wrong: %v", rec.AffectedEffectIDs)
	}
	if !reflect.DeepEqual(rec.AffectedTransactionIDs, []string{"tx-new"}) {
		t.Fatalf("affected transaction ids wrong: %v", rec.AffectedTransactionIDs)
	}
	if !rec.ReconciliationRequired || rec.AutomaticResumeAllowed {
		t.Fatalf("newer-than-backup effects must require reconciliation: %+v", rec)
	}
	if rec.HumanSummary != HumanReconciliation {
		t.Fatalf("got %q, want %q", rec.HumanSummary, HumanReconciliation)
	}
	if len(rec.RecoveryCommands) != 0 {
		t.Fatalf("tx-new is not in the restored ledger, no recovery command expected: %v", rec.RecoveryCommands)
	}
	found := false
	for _, status := range rec.Effects {
		if status.EffectID == "eff-new1" {
			found = true
			if status.State != StateExternalStateAmbiguous || status.Reason != ReasonNewerThanBackup {
				t.Fatalf("newer effect misclassified: %+v", status)
			}
		}
		if status.EffectID == "eff-a1" && (status.State != StateNoExternalEffect || status.Reason != ReasonNeverDispatched) {
			t.Fatalf("restored effect misclassified: %+v", status)
		}
	}
	if !found {
		t.Fatal("newer effect missing from detail list")
	}
}

func TestRestore_Reconcile_MixedPreparedEffectsAcrossTransactions(t *testing.T) {
	root, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		for i := 0; i < 3; i++ {
			txID := "tx-" + strings.Repeat(string(rune('a'+i)), 1)
			addTx(t, r, txID, root)
			reconcileEffect(t, r, txID, "eff-"+txID+"-1", "")
			reconcileEffect(t, r, txID, "eff-"+txID+"-2", "")
		}
	})
	report := applyReconcile(t, root, live, backup)
	rec := report.EffectReconciliation
	if rec.NoExternalEffectCount != 6 {
		t.Fatalf("expected 6 no-external-effect classifications, got %d", rec.NoExternalEffectCount)
	}
	if len(rec.Effects) != 6 {
		t.Fatalf("expected 6 detail entries, got %d", len(rec.Effects))
	}
	seen := map[string]bool{}
	for _, status := range rec.Effects {
		if seen[status.EffectID] {
			t.Fatalf("duplicate effect id %s in comparison output", status.EffectID)
		}
		seen[status.EffectID] = true
	}
	if !rec.AutomaticResumeAllowed || rec.ReconciliationRequired {
		t.Fatalf("prepared-only ledger must allow automatic resume: %+v", rec)
	}
}

func TestRestore_Reconcile_RepeatedComparisonIsIdempotentAndReadOnly(t *testing.T) {
	root, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", "")
	})
	l, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	addTx(t, l, "tx-new", root)
	reconcileEffect(t, l, "tx-new", "eff-new1", "")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	report := applyReconcile(t, root, live, backup)
	qDir := quarantineDirOf(t, report)

	liveBefore := mustSHA(t, live)
	qBefore := mustSHA(t, filepath.Join(qDir, "ledger.db"))

	// Repeated comparison is read-only and idempotent: identical output, no
	// mutation of the restored ledger or the preserved quarantine.
	second := evaluateExternalEffects(live, qDir)
	report.EffectReconciliation.EvaluatedAt = time.Time{}
	second.EvaluatedAt = time.Time{}
	firstJSON, _ := json.Marshal(report.EffectReconciliation)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("repeated comparison differs:\nfirst=%s\nsecond=%s", firstJSON, secondJSON)
	}
	if liveBefore != mustSHA(t, live) {
		t.Fatal("comparison mutated the restored ledger")
	}
	if qBefore != mustSHA(t, filepath.Join(qDir, "ledger.db")) {
		t.Fatal("comparison mutated the preserved quarantine")
	}
	// Each effect appears exactly once: repeated comparison cannot fabricate
	// duplicates.
	counts := map[string]int{}
	for _, status := range second.Effects {
		counts[status.EffectID]++
	}
	for effectID, n := range counts {
		if n != 1 {
			t.Fatalf("effect %s appears %d times", effectID, n)
		}
	}
}

func TestRestore_Reconcile_EvidenceGapFailsClosed(t *testing.T) {
	root, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", "")
	})
	report := applyReconcile(t, root, live, backup)
	qDir := quarantineDirOf(t, report)
	if rec := report.EffectReconciliation; rec.EvidenceUnavailableCount != 0 {
		t.Fatalf("healthy quarantine must not produce evidence gaps: %+v", rec)
	}
	// Corrupt the preserved pre-restore ledger: the comparison cannot prove
	// there are no newer effects, so it must fail closed.
	if err := os.WriteFile(filepath.Join(qDir, "ledger.db"), []byte("not a sqlite database at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := evaluateExternalEffects(live, qDir)
	if rec.EvidenceUnavailableCount < 1 {
		t.Fatalf("unreadable pre-restore ledger must fail closed: %+v", rec)
	}
	if rec.HumanSummary != HumanEvidenceGap {
		t.Fatalf("got %q, want %q", rec.HumanSummary, HumanEvidenceGap)
	}
	if !rec.ReconciliationRequired || rec.AutomaticResumeAllowed {
		t.Fatalf("evidence gap must require reconciliation and forbid auto-resume: %+v", rec)
	}
	if !strings.Contains(rec.RecommendedAction, "manual review") {
		t.Fatalf("unexpected recommended action: %q", rec.RecommendedAction)
	}
}

func TestRestore_Reconcile_ChangedMaterialCannotReuseApproval(t *testing.T) {
	root, live, backup1 := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", "")
	})
	first := applyReconcile(t, root, live, backup1)
	if first.EffectReconciliation.NoExternalEffectCount != 1 {
		t.Fatalf("first comparison wrong: %+v", first.EffectReconciliation)
	}
	// The restored ledger is now backup1's content. Create a second backup
	// with changed material and restore it: the comparison must be re-derived
	// from the new restored state, never carried over from the first run.
	l, err := ledger.OpenRepository(live)
	if err != nil {
		t.Fatal(err)
	}
	reconcileEffect(t, l, "tx-a", "eff-a2", "")
	backup2 := filepath.Join(root, "backup2.db")
	if _, err := l.Backup(backup2); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	second := applyReconcile(t, root, live, backup2)
	if second.EffectReconciliation.NoExternalEffectCount != 2 {
		t.Fatalf("second comparison must reflect the changed restored material: %+v", second.EffectReconciliation)
	}
	if reflect.DeepEqual(first.EffectReconciliation, second.EffectReconciliation) {
		t.Fatal("stale comparison material was reused across restores")
	}
}

func TestRestore_Reconcile_JSONFieldsAndSecretRedaction(t *testing.T) {
	const secret = "hunter2-supersecret-value"
	root, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", `{"payload":"`+secret+`"}`)
	})
	report := applyReconcile(t, root, live, backup)
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, key := range []string{
		`"reconciliation_required"`, `"affected_transaction_ids"`, `"affected_effect_ids"`,
		`"known_absent_count"`, `"known_present_count"`, `"ambiguous_count"`,
		`"evidence_unavailable_count"`, `"automatic_resume_allowed"`, `"recommended_action"`,
	} {
		if !strings.Contains(s, key) {
			t.Fatalf("report JSON missing required field %s", key)
		}
	}
	if strings.Contains(s, secret) {
		t.Fatal("report leaks effect input payload")
	}
	for _, key := range []string{`"token"`, `"credential"`, `"secret"`, `"password"`, `"private_key"`, `"input_json"`, `"destination"`} {
		if strings.Contains(strings.ToLower(s), key) {
			t.Fatalf("report contains credential/payload-bearing key %s", key)
		}
	}
}

func TestRestore_Reconcile_AlreadyRestoredRepeatsComparison(t *testing.T) {
	root, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", "")
	})
	first := applyReconcile(t, root, live, backup)
	second, err := RunWithInjector(applyOptions(root, live, backup, first.BackupSHA256), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !second.AlreadyRestored || second.Applied {
		t.Fatalf("expected stable already-restored repeat: %+v", second)
	}
	if second.EffectReconciliation == nil {
		t.Fatal("already-restored repeat must still compare the restored ledger")
	}
	if second.EffectReconciliation.HumanSummary != first.EffectReconciliation.HumanSummary {
		t.Fatalf("repeat comparison diverged: %q vs %q", second.EffectReconciliation.HumanSummary, first.EffectReconciliation.HumanSummary)
	}
	if second.EffectReconciliation.NoExternalEffectCount != 1 {
		t.Fatalf("repeat comparison wrong: %+v", second.EffectReconciliation)
	}
}

func TestRestore_Reconcile_DryRunCarriesNoComparison(t *testing.T) {
	_, live, backup := reconcileHome(t, func(t *testing.T, r *ledger.Repository, root string) {
		addTx(t, r, "tx-a", root)
		reconcileEffect(t, r, "tx-a", "eff-a1", "")
	})
	dry, err := Run(Options{LivePath: live, BackupPath: backup})
	if err != nil {
		t.Fatal(err)
	}
	if dry.EffectReconciliation != nil {
		t.Fatal("dry run must not carry an effect reconciliation")
	}
}

func TestClassify_ReconciliationRequired_NeedsReconciliationTransaction(t *testing.T) {
	// A transaction parked for reconciliation requires the canonical recovery
	// command for effects whose fate is not already determinate.
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectVerified), domain.StateNeedsReconciliation, nil, ledger.ErrNotFound, nil)
	if s.State != StateReconciliationRequired || s.Reason != ReasonRequiresCanonicalRecovery {
		t.Fatalf("got %s/%s, want reconciliation_required/requires_canonical_recovery", s.State, s.Reason)
	}
	s = classifyEffect(effectForClassify("tx", "eff", domain.EffectUnknown), domain.StateNeedsReconciliation, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("unknown")})
	if s.State != StateReconciliationRequired || s.Reason != ReasonRequiresCanonicalRecovery {
		t.Fatalf("got %s/%s, want reconciliation_required/requires_canonical_recovery", s.State, s.Reason)
	}
	// Determinate fates stay determinate even under needs_reconciliation.
	s = classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitted), domain.StateNeedsReconciliation, testReceipt("eff"), nil, nil)
	if s.State != StateKnownPresent {
		t.Fatalf("committed effect must stay known_present, got %s", s.State)
	}
	s = classifyEffect(effectForClassify("tx", "eff", domain.EffectVerified), domain.StateNeedsReconciliation, nil, ledger.ErrNotFound, []domain.EffectAttempt{attempt("not_found")})
	if s.State != StateKnownAbsent {
		t.Fatalf("proved-absent effect must stay known_absent, got %s", s.State)
	}
	s = classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitted), domain.StateNeedsReconciliation, nil, ledger.ErrNotFound, nil)
	if s.State != StateEvidenceUnavailable {
		t.Fatalf("committed-without-receipt must stay evidence_unavailable, got %s", s.State)
	}
}

func TestClassify_CommittingTransactionNeverAttemptedIsInFlight(t *testing.T) {
	s := classifyEffect(effectForClassify("tx", "eff", domain.EffectVerified), domain.StateCommitting, nil, ledger.ErrNotFound, nil)
	if s.State != StateExternalStateAmbiguous || s.Reason != ReasonInFlight {
		t.Fatalf("committing tx never-attempted effect must be in-flight, got %s/%s", s.State, s.Reason)
	}
	// A committing transaction's committed effect stays determinate.
	s = classifyEffect(effectForClassify("tx", "eff", domain.EffectCommitted), domain.StateCommitting, testReceipt("eff"), nil, nil)
	if s.State != StateKnownPresent {
		t.Fatalf("committed effect must stay known_present, got %s", s.State)
	}
}

func TestReconciliationFinish_CountsReconciliationRequired(t *testing.T) {
	r := EffectReconciliation{Effects: []EffectStatus{
		{EffectID: "e1", TransactionID: "t1", State: StateReconciliationRequired, Reason: ReasonRequiresCanonicalRecovery},
		{EffectID: "e2", TransactionID: "t1", State: StateKnownPresent, Reason: ReasonReceipt},
	}}
	r.finish()
	if r.ReconciliationCount != 1 || r.KnownPresentCount != 1 {
		t.Fatalf("counts wrong: %+v", r)
	}
	if !r.ReconciliationRequired || r.AutomaticResumeAllowed {
		t.Fatalf("reconciliation-required state must forbid auto-resume: %+v", r)
	}
	if r.HumanSummary != HumanReconciliation {
		t.Fatalf("got %q, want %q", r.HumanSummary, HumanReconciliation)
	}
	if len(r.AffectedEffectIDs) != 2 || len(r.AffectedTransactionIDs) != 1 {
		t.Fatalf("affected lists wrong: %+v", r)
	}
}
