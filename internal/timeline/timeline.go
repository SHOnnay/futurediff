package timeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/ledger"
)

const Version = "0.1"

type Entry struct {
	Sequence  int64     `json:"sequence"`
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`
	EffectID  string    `json:"effect_id,omitempty"`
	Summary   string    `json:"summary"`
	EventHash string    `json:"event_hash"`
}

type Report struct {
	Version       string  `json:"version"`
	TransactionID string  `json:"transaction_id"`
	FinalStatus   string  `json:"final_status"`
	Entries       []Entry `json:"entries"`
	Digest        string  `json:"digest"`
}

func Build(repo *ledger.Repository, transactionID string) (Report, error) {
	if repo == nil || strings.TrimSpace(transactionID) == "" {
		return Report{}, errors.New("repository and transaction id are required")
	}
	snap, err := repo.Snapshot(transactionID)
	if err != nil {
		return Report{}, err
	}
	rows := snap.Rows["events"]
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		seq, err := intValue(row["sequence"])
		if err != nil {
			return Report{}, fmt.Errorf("event sequence: %w", err)
		}
		ts, err := time.Parse(time.RFC3339Nano, stringValue(row["created_at"]))
		if err != nil {
			return Report{}, fmt.Errorf("event %d timestamp: %w", seq, err)
		}
		eventType := stringValue(row["event_type"])
		entries = append(entries, Entry{Sequence: seq, Timestamp: ts, EventType: eventType, EffectID: stringValue(row["effect_id"]), Summary: summarize(eventType), EventHash: stringValue(row["event_hash"])})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
	report := Report{Version: Version, TransactionID: transactionID, FinalStatus: string(snap.Transaction.Status), Entries: entries}
	report.Digest = digest(report)
	return report, nil
}

func Markdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# FutureDiff transaction timeline\n\n- Transaction: `%s`\n- Final status: `%s`\n- Timeline digest: `%s`\n\n", r.TransactionID, r.FinalStatus, r.Digest)
	b.WriteString("| Seq | Time (UTC) | Event | Effect | Summary |\n|---:|---|---|---|---|\n")
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "| %d | %s | `%s` | `%s` | %s |\n", e.Sequence, e.Timestamp.UTC().Format(time.RFC3339), escape(e.EventType), escape(e.EffectID), escape(e.Summary))
	}
	return b.String()
}

func Mermaid(r Report) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	for i, e := range r.Entries {
		id := fmt.Sprintf("E%d", e.Sequence)
		fmt.Fprintf(&b, "  %s[\"%d. %s\\n%s\"]\n", id, e.Sequence, mermaidEscape(e.EventType), mermaidEscape(e.Summary))
		if i > 0 {
			fmt.Fprintf(&b, "  E%d --> %s\n", r.Entries[i-1].Sequence, id)
		}
	}
	return b.String()
}

func summarize(kind string) string {
	switch kind {
	case "transaction.created":
		return "Transaction created and durable workspace registered"
	case "transaction.active":
		return "Transaction entered active staging"
	case "transaction.sealed":
		return "Staged changes were sealed"
	case "transaction.verifying":
		return "Deterministic verification started"
	case "transaction.ready":
		return "All required verification checks passed"
	case "transaction.committing":
		return "Commit intent was recorded before external release"
	case "transaction.committed":
		return "All required repository and external effects were finalized"
	case "transaction.needs_reconciliation":
		return "Outcome is ambiguous and requires status reconciliation"
	case "transaction.stale":
		return "Approved material became stale"
	case "transaction.aborted":
		return "Uncommitted transaction was aborted"
	case "runtime.execution":
		return "Command execution evidence was recorded"
	case "verification.recorded":
		return "Verification evidence and outcome were stored"
	case "approval.recorded":
		return "Approval was bound to the exact transaction digest"
	case "effect.prepared":
		return "External effect was prepared but not released"
	case "effect.committing":
		return "External effect write-ahead intent was recorded"
	case "effect.committed":
		return "External provider receipt was recorded"
	default:
		clean := strings.ReplaceAll(kind, ".", " ")
		if clean == "" {
			return "Event recorded"
		}
		return strings.ToUpper(clean[:1]) + clean[1:]
	}
}

func digest(r Report) string {
	r.Digest = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
func intValue(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case string:
		return strconv.ParseInt(x, 10, 64)
	case []byte:
		return strconv.ParseInt(string(x), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported value %T", v)
	}
}
func escape(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ") }
func mermaidEscape(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(s, "\"", "'"), "[", "("), "]", ")")
}
