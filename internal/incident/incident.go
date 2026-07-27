package incident

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/diffsummary"
	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/replay"
	"github.com/SHOnnay/futurediff/internal/timeline"
)

const Version = "0.1"

type Report struct {
	Version       string                `json:"version"`
	GeneratedAt   time.Time             `json:"generated_at"`
	TransactionID string                `json:"transaction_id"`
	Severity      string                `json:"severity"`
	Status        string                `json:"status"`
	WindowStart   *time.Time            `json:"window_start,omitempty"`
	WindowEnd     *time.Time            `json:"window_end,omitempty"`
	Diff          diffsummary.Summary   `json:"diff"`
	Timeline      timeline.Report       `json:"timeline"`
	Replay        replay.Report         `json:"replay"`
	Findings      []ledger.AuditFinding `json:"findings,omitempty"`
	Actions       []string              `json:"recommended_actions"`
	Digest        string                `json:"digest"`
}

func Build(repo *ledger.Repository, transactionID string, now time.Time) (Report, error) {
	if repo == nil || strings.TrimSpace(transactionID) == "" {
		return Report{}, errors.New("repository and transaction id are required")
	}
	snap, err := repo.Snapshot(transactionID)
	if err != nil {
		return Report{}, err
	}
	diff, err := diffsummary.Build(snap)
	if err != nil {
		return Report{}, err
	}
	tl, err := timeline.Build(repo, transactionID)
	if err != nil {
		return Report{}, err
	}
	rp, err := replay.Transaction(repo, transactionID)
	if err != nil {
		return Report{}, err
	}
	audit, err := repo.Audit()
	if err != nil {
		return Report{}, err
	}
	findings := make([]ledger.AuditFinding, 0)
	for _, f := range audit.Findings {
		if f.TransactionID == "" || f.TransactionID == transactionID {
			findings = append(findings, f)
		}
	}
	r := Report{Version: Version, GeneratedAt: now.UTC().Truncate(time.Second), TransactionID: transactionID, Status: string(snap.Transaction.Status), Diff: diff, Timeline: tl, Replay: rp, Findings: findings}
	if len(tl.Entries) > 0 {
		start, end := tl.Entries[0].Timestamp, tl.Entries[len(tl.Entries)-1].Timestamp
		r.WindowStart = &start
		r.WindowEnd = &end
	}
	r.Severity = severity(snap.Transaction.Status, rp, findings, snap.Effects)
	r.Actions = actions(snap.Transaction.Status, rp, findings, snap.Effects)
	sort.Strings(r.Actions)
	input := r
	input.Digest = ""
	d, err := domain.Digest(input)
	if err != nil {
		return Report{}, err
	}
	r.Digest = d
	return r, nil
}

func Markdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# FutureDiff Incident Reconstruction\n\n- Transaction: `%s`\n- Status: **%s**\n- Severity: **%s**\n- Report digest: `%s`\n", r.TransactionID, r.Status, r.Severity, r.Digest)
	if r.WindowStart != nil {
		fmt.Fprintf(&b, "- Window: `%s` to `%s`\n", r.WindowStart.UTC().Format(time.RFC3339), r.WindowEnd.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "- Event replay valid: **%t**\n- Audit findings: **%d**\n- Effects: **%d**\n- Provider receipts: **%d**\n", r.Replay.Valid, len(r.Findings), len(r.Diff.Effects), r.Diff.ReceiptCount)
	b.WriteString("\n## Recommended actions\n")
	for _, a := range r.Actions {
		fmt.Fprintf(&b, "- %s\n", a)
	}
	if len(r.Findings) > 0 {
		b.WriteString("\n## Findings\n")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "- **%s** `%s`: %s\n", f.Severity, f.Code, f.Message)
		}
	}
	b.WriteString("\n## Transaction summary\n\n")
	b.WriteString(diffsummary.Markdown(r.Diff))
	b.WriteString("\n## Timeline\n\n")
	b.WriteString(timeline.Markdown(r.Timeline))
	return b.String()
}

func JSON(r Report) ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

func severity(status domain.TransactionState, rp replay.Report, findings []ledger.AuditFinding, effects []domain.ExternalEffect) string {
	for _, f := range findings {
		if f.Severity == ledger.AuditError {
			return "critical"
		}
	}
	if !rp.Valid {
		return "critical"
	}
	for _, e := range effects {
		if e.Status == domain.EffectUnknown {
			return "high"
		}
	}
	switch status {
	case domain.StateManualIntervention:
		return "critical"
	case domain.StateNeedsReconciliation, domain.StateCommitting, domain.StateCompensating:
		return "high"
	case domain.StateFailedVerification, domain.StateStale:
		return "medium"
	default:
		return "informational"
	}
}
func actions(status domain.TransactionState, rp replay.Report, findings []ledger.AuditFinding, effects []domain.ExternalEffect) []string {
	set := map[string]bool{}
	add := func(s string) { set[s] = true }
	if !rp.Valid {
		add("Enter maintenance mode and preserve the ledger before attempting any repair")
		add("Export a support bundle and transaction futurepack for independent review")
	}
	for _, f := range findings {
		if f.Severity == ledger.AuditError {
			add("Resolve ledger audit errors before releasing additional effects")
		}
	}
	unknown := false
	for _, e := range effects {
		if e.Status == domain.EffectUnknown {
			unknown = true
		}
	}
	if unknown || status == domain.StateNeedsReconciliation {
		add("Query provider status using the recovery workflow; do not blindly retry external mutations")
	}
	switch status {
	case domain.StateFailedVerification:
		add("Repair the existing staged workspace and rerun deterministic verification")
	case domain.StateStale:
		add("Refresh source and provider versions, then obtain a new approval digest")
	case domain.StateCommitted:
		add("Confirm repository materialization and provider receipts, then archive the incident report")
	case domain.StateManualIntervention:
		add("Assign a human incident owner and document the final provider state")
	}
	if len(set) == 0 {
		add("Review the timeline and transaction diff; no immediate integrity failure was detected")
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	return out
}
