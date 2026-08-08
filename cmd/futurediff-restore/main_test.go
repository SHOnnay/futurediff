package main

import (
	"strings"
	"testing"

	"github.com/SHOnnay/futurediff/internal/ledgerrestore"
)

func TestHumanGuidance_NoReconciliation(t *testing.T) {
	r := ledgerrestore.Report{}
	if got := humanGuidance(r); got != "" {
		t.Fatalf("report without reconciliation must print nothing, got %q", got)
	}
	r.EffectReconciliation = &ledgerrestore.EffectReconciliation{HumanSummary: ledgerrestore.HumanNone}
	got := humanGuidance(r)
	if got != ledgerrestore.HumanNone {
		t.Fatalf("got %q, want %q", got, ledgerrestore.HumanNone)
	}
	if strings.Contains(got, "run:") {
		t.Fatalf("no commands expected: %q", got)
	}
}

func TestHumanGuidance_ReconciliationRequired(t *testing.T) {
	r := ledgerrestore.Report{EffectReconciliation: &ledgerrestore.EffectReconciliation{
		HumanSummary:      ledgerrestore.HumanReconciliation,
		RecommendedAction: "run the canonical recovery command for each affected change before further publication",
		RecoveryCommands:  []string{"fdif recover tx-abc --yes", "fdif recover tx-def --yes"},
	}}
	got := humanGuidance(r)
	if !strings.HasPrefix(got, ledgerrestore.HumanReconciliation+"\n") {
		t.Fatalf("summary must be the first line: %q", got)
	}
	for _, cmd := range []string{"run: fdif recover tx-abc --yes", "run: fdif recover tx-def --yes"} {
		if !strings.Contains(got, cmd) {
			t.Fatalf("missing exact command %q in %q", cmd, got)
		}
	}
	if !strings.Contains(got, "canonical recovery") {
		t.Fatalf("missing recommended action in %q", got)
	}
}

func TestHumanGuidance_AllFourSummariesSurfaceVerbatim(t *testing.T) {
	for _, summary := range []string{
		ledgerrestore.HumanNone,
		ledgerrestore.HumanKnownEffects,
		ledgerrestore.HumanReconciliation,
		ledgerrestore.HumanEvidenceGap,
	} {
		r := ledgerrestore.Report{EffectReconciliation: &ledgerrestore.EffectReconciliation{HumanSummary: summary, RecommendedAction: summary}}
		got := humanGuidance(r)
		if got != summary {
			t.Fatalf("summary %q must surface verbatim, got %q", summary, got)
		}
	}
}

func TestHumanGuidance_ManualInterventionUsesStatusReadNotRecover(t *testing.T) {
	r := ledgerrestore.Report{EffectReconciliation: &ledgerrestore.EffectReconciliation{
		HumanSummary:     ledgerrestore.HumanReconciliation,
		RecoveryCommands: []string{"fdif status tx-manual"},
	}}
	got := humanGuidance(r)
	if strings.Contains(got, "fdif recover tx-manual") {
		t.Fatalf("manual-intervention transaction must not get an automatic recover command: %q", got)
	}
	if !strings.Contains(got, "run: fdif status tx-manual") {
		t.Fatalf("expected status-read command: %q", got)
	}
}
