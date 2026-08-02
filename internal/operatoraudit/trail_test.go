package operatoraudit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Root: t.TempDir(), Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
}

func TestVerifyMissingTrailIsValid(t *testing.T) {
	store := fixedStore(t)
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Count != 0 || report.Schema != Version || report.ExportFormat != "jsonl" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRecordRejectsRelativeRoot(t *testing.T) {
	store := &Store{Root: filepath.Join("relative", "root"), Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected relative root rejection")
	}
}

func sampleInput() Input {
	return Input{
		OperationID:    "req_123",
		TransactionID:  "tx_123",
		Actor:          Actor{PrincipalID: "uid:1000", PeerUID: 1000, Source: "unix-peer"},
		Context:        ExecutionContext{Component: "api", RequestID: "req_123", Method: "POST", Path: "/v1/transactions/tx_123/commit"},
		EventType:      "transaction.commit.request",
		Target:         Target{ResourceType: "transaction", ResourceID: "tx_123"},
		Result:         ResultRequested,
		PolicyDecision: PolicyAllow,
		Metadata:       map[string]string{"effect_count": "1"},
	}
}

func TestRecordVerifyRoundTrip(t *testing.T) {
	store := fixedStore(t)
	first, err := store.Record(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	secondInput := sampleInput()
	secondInput.EventType = "transaction.commit.result"
	secondInput.Result = ResultSucceeded
	secondInput.Metadata = map[string]string{"published_ref": "refs/heads/futurediff/tx_123"}
	second, err := store.Record(secondInput)
	if err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Count != 2 || report.HeadHash != second.EventHash {
		t.Fatalf("unexpected report: %+v", report)
	}
	events, err := store.Events()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].EventHash != first.EventHash || events[1].PreviousHash != first.EventHash {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestRecordRedactsSensitiveMetadataDeterministically(t *testing.T) {
	store := fixedStore(t)
	input := sampleInput()
	input.Metadata = map[string]string{
		"credential_id": "github-main",
		"token":         "ghp_super_secret_token",
		"message":       "Bearer top-secret-value",
	}
	event, err := store.Record(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := event.Metadata["credential_id"]; got != "github-main" {
		t.Fatalf("credential id redacted unexpectedly: %q", got)
	}
	for _, key := range []string{"token", "message"} {
		if !strings.HasPrefix(event.Metadata[key], "[redacted sha256:") {
			t.Fatalf("%s not redacted: %q", key, event.Metadata[key])
		}
	}
	if len(event.Redacted) != 2 {
		t.Fatalf("redacted=%v", event.Redacted)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super_secret") || strings.Contains(string(raw), "top-secret-value") {
		t.Fatal("sensitive value leaked into operator audit trail")
	}
}

func TestConcurrentRecordSerializesChain(t *testing.T) {
	store := fixedStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input := sampleInput()
			input.OperationID = input.OperationID + string(rune('a'+i))
			input.Context.RequestID = input.OperationID
			input.EventType = "transaction.verify.request"
			input.Result = ResultRequested
			input.Metadata = map[string]string{"attempt": string(rune('a' + i))}
			if _, err := store.Record(input); err != nil {
				t.Errorf("record %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Count != 16 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestVerifyDetectsModifiedRecord(t *testing.T) {
	store := fixedStore(t)
	if _, err := store.Record(sampleInput()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &event); err != nil {
		t.Fatal(err)
	}
	event.Metadata["effect_count"] = "99"
	line, _ := json.Marshal(event)
	if err := os.WriteFile(store.Path(), append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !containsFinding(report.Findings, "event hash mismatch") {
		t.Fatalf("expected tamper detection: %+v", report)
	}
}

func TestVerifyDetectsPartialWrite(t *testing.T) {
	store := fixedStore(t)
	if _, err := store.Record(sampleInput()); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(store.Path(), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"broken\""); err != nil {
		t.Fatal(err)
	}
	f.Close()
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !containsFinding(report.Findings, "trailing partial write detected") {
		t.Fatalf("expected partial-write detection: %+v", report)
	}
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected append to fail while trail is invalid")
	}
}

func TestVerifyDetectsRemovedOrReorderedRecords(t *testing.T) {
	store := fixedStore(t)
	for _, eventType := range []string{"transaction.create.request", "transaction.create.result", "transaction.commit.request"} {
		input := sampleInput()
		input.OperationID = eventType
		input.Context.RequestID = eventType
		input.EventType = eventType
		if _, err := store.Record(input); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	reordered := strings.Join([]string{lines[1], lines[0], lines[2]}, "\n") + "\n"
	if err := os.WriteFile(store.Path(), []byte(reordered), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !containsFinding(report.Findings, "sequence mismatch") {
		t.Fatalf("expected reorder detection: %+v", report)
	}
}

func TestVerifyRejectsMalformedSchemaVersion(t *testing.T) {
	store := fixedStore(t)
	if _, err := store.Record(sampleInput()); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events()
	if err != nil {
		t.Fatal(err)
	}
	events[0].SchemaVersion = "999"
	line, _ := json.Marshal(events[0])
	if err := os.WriteFile(store.Path(), append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !containsFinding(report.Findings, "unsupported schema version") {
		t.Fatalf("expected schema rejection: %+v", report)
	}
}

func TestRecordUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows permission bits are not authoritative in CI")
	}
	store := fixedStore(t)
	if _, err := store.Record(sampleInput()); err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file perms=%03o", fileInfo.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(store.Path()))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir perms=%03o", dirInfo.Mode().Perm())
	}
}

func containsFinding(findings []string, needle string) bool {
	for _, finding := range findings {
		if strings.Contains(finding, needle) {
			return true
		}
	}
	return false
}
