package diffsummary

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/ledger"
)

type EffectSummary struct {
	EffectID      string `json:"effect_id"`
	Tool          string `json:"tool"`
	Adapter       string `json:"adapter"`
	Status        string `json:"status"`
	Reversibility string `json:"reversibility"`
	CommitRank    int    `json:"commit_rank"`
}

type Summary struct {
	FormatVersion         string          `json:"format_version"`
	TransactionID         string          `json:"transaction_id"`
	Status                string          `json:"status"`
	Mode                  string          `json:"mode"`
	BaseRevision          string          `json:"base_revision,omitempty"`
	StagedTreeOID         string          `json:"staged_tree_oid,omitempty"`
	PatchSHA256           string          `json:"patch_sha256,omitempty"`
	ChangedPathCount      int             `json:"changed_path_count"`
	ChangedPaths          []string        `json:"changed_paths,omitempty"`
	VerificationOutcome   string          `json:"verification_outcome,omitempty"`
	VerificationDigest    string          `json:"verification_digest,omitempty"`
	Effects               []EffectSummary `json:"effects,omitempty"`
	ApprovalCount         int             `json:"approval_count"`
	ReceiptCount          int             `json:"receipt_count"`
	RuntimeExecutionCount int             `json:"runtime_execution_count"`
	EventCount            int             `json:"event_count"`
	Warnings              []string        `json:"warnings,omitempty"`
	SummaryDigest         string          `json:"summary_digest"`
}

func Build(snapshot ledger.TransactionSnapshot) (Summary, error) {
	s := Summary{FormatVersion: "0.1", TransactionID: snapshot.Transaction.ID, Status: string(snapshot.Transaction.Status), Mode: snapshot.Transaction.Mode, BaseRevision: snapshot.Transaction.BaseRevision}
	if s.TransactionID == "" {
		return s, fmt.Errorf("transaction id is required")
	}
	if snapshot.Patch != nil {
		s.StagedTreeOID = snapshot.Patch.StagedTreeOID
		s.PatchSHA256 = snapshot.Patch.PatchSHA256
		s.ChangedPaths = append([]string(nil), snapshot.Patch.ChangedPaths...)
		sort.Strings(s.ChangedPaths)
		s.ChangedPathCount = len(s.ChangedPaths)
	}
	rows := snapshot.Rows
	if v := rows["verification_runs"]; len(v) > 0 {
		latest := v[len(v)-1]
		s.VerificationOutcome = str(latest["outcome"])
		s.VerificationDigest = str(latest["verification_digest"])
	}
	for _, e := range snapshot.Effects {
		s.Effects = append(s.Effects, EffectSummary{EffectID: e.EffectID, Tool: e.ToolIdentity, Adapter: e.AdapterIdentity, Status: string(e.Status), Reversibility: e.Reversibility, CommitRank: e.CommitRank})
	}
	sort.Slice(s.Effects, func(i, j int) bool {
		if s.Effects[i].CommitRank != s.Effects[j].CommitRank {
			return s.Effects[i].CommitRank < s.Effects[j].CommitRank
		}
		return s.Effects[i].EffectID < s.Effects[j].EffectID
	})
	s.ApprovalCount = len(rows["approvals"])
	s.ReceiptCount = len(rows["receipts"])
	s.RuntimeExecutionCount = len(rows["runtime_executions"])
	s.EventCount = len(rows["events"])
	if snapshot.Patch == nil {
		s.Warnings = append(s.Warnings, "no staged patch is recorded")
	}
	if s.VerificationOutcome == "" {
		s.Warnings = append(s.Warnings, "no verification result is recorded")
	}
	if snapshot.Transaction.ApprovalDigest != "" && s.ApprovalCount == 0 {
		s.Warnings = append(s.Warnings, "transaction has approval digest but no approval record in snapshot")
	}
	if snapshot.Transaction.Status == domain.StateCommitted && len(s.Effects) > s.ReceiptCount {
		s.Warnings = append(s.Warnings, "committed transaction has fewer provider receipts than effects")
	}
	sort.Strings(s.Warnings)
	digestInput := s
	s.SummaryDigest = ""
	d, err := domain.Digest(digestInput)
	if err != nil {
		return Summary{}, err
	}
	s.SummaryDigest = d
	return s, nil
}

func Markdown(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# FutureDiff Transaction Diff\n\n- Transaction: `%s`\n- Status: **%s**\n- Mode: `%s`\n- Base revision: `%s`\n- Staged tree: `%s`\n- Patch SHA-256: `%s`\n- Changed paths: **%d**\n- Verification: **%s**\n- External effects: **%d**\n- Approvals: **%d**\n- Receipts: **%d**\n- Summary digest: `%s`\n", s.TransactionID, s.Status, s.Mode, s.BaseRevision, s.StagedTreeOID, s.PatchSHA256, s.ChangedPathCount, s.VerificationOutcome, len(s.Effects), s.ApprovalCount, s.ReceiptCount, s.SummaryDigest)
	if len(s.ChangedPaths) > 0 {
		b.WriteString("\n## Changed paths\n")
		for _, p := range s.ChangedPaths {
			fmt.Fprintf(&b, "- `%s`\n", p)
		}
	}
	if len(s.Effects) > 0 {
		b.WriteString("\n## External effects\n")
		for _, e := range s.Effects {
			fmt.Fprintf(&b, "- `%s` — %s via %s — **%s** — reversibility: %s\n", e.EffectID, e.Tool, e.Adapter, e.Status, e.Reversibility)
		}
	}
	if len(s.Warnings) > 0 {
		b.WriteString("\n## Warnings\n")
		for _, w := range s.Warnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	return b.String()
}

func JSON(s Summary) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}
