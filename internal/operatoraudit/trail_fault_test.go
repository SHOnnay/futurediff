package operatoraudit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SHOnnay/futurediff/internal/durablewrite"
)

func faultStore(t *testing.T, inject durablewrite.Injector) *Store {
	t.Helper()
	return &Store{Root: t.TempDir(), Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }, Injector: inject}
}

func TestRecordFirstCreationIsDurable(t *testing.T) {
	store := faultStore(t, nil)
	event, err := store.Record(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 1 || event.PreviousHash != "" {
		t.Fatalf("event=%+v", event)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Count != 1 || report.HeadHash != event.EventHash {
		t.Fatalf("report=%+v", report)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(store.Path()), uncertainMarker)); !os.IsNotExist(err) {
		t.Fatalf("uncertain marker present after clean append")
	}
}

func TestRecordCreateFaultSafeRetry(t *testing.T) {
	inject := durablewrite.NewOneShot(map[string]error{durablewrite.OpCreate: durablewrite.ErrIO})
	store := faultStore(t, inject)
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected create fault")
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Fatalf("trail file created despite create fault")
	}
	// The fault fired before any bytes changed: retry is safe.
	event, err := store.Record(sampleInput())
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if event.Sequence != 1 {
		t.Fatalf("sequence=%d", event.Sequence)
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Count != 1 {
		t.Fatalf("report=%+v", report)
	}
}

func TestRecordWriteFaultSafeRetry(t *testing.T) {
	inject := durablewrite.NewOneShot(map[string]error{durablewrite.OpWrite: durablewrite.ErrIO})
	store := faultStore(t, inject)
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected write fault")
	}
	// The fault fired before any bytes were written: the trail is unchanged.
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("trail has bytes after write fault: %q", data)
	}
	event, err := store.Record(sampleInput())
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if event.Sequence != 1 {
		t.Fatalf("sequence=%d", event.Sequence)
	}
}

func TestRecordShortWriteDetectedFailClosed(t *testing.T) {
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpShortWrite: durablewrite.ErrIO})
	store := faultStore(t, inject)
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected short-write fault")
	}
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !containsFinding(report.Findings, "trailing partial write detected") {
		t.Fatalf("expected partial-write detection: %+v", report)
	}
	// Fail closed: the ambiguous append must not be silently retried or reset.
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected append to be refused while the trail is uncertain")
	}
	// Evidence preserved: the partial bytes are still on disk.
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("partial append evidence was lost")
	}
}

func TestRecordFileSyncFaultMarksUncertain(t *testing.T) {
	inject := durablewrite.NewOneShot(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO})
	store := faultStore(t, inject)
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected file-sync fault")
	}
	marker := filepath.Join(filepath.Dir(store.Path()), uncertainMarker)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("uncertain marker missing: %v", err)
	}
	// The full line is visible; verification reports the chain as uncertain.
	report, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || !containsFinding(report.Findings, "durability uncertain") {
		t.Fatalf("expected uncertainty finding: %+v", report)
	}
	if _, err := store.Events(); err == nil {
		t.Fatal("expected Events to fail closed")
	}
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected append to be refused while uncertain")
	}
	// Evidence preserved: the record line is on disk and remains valid.
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "req_123") || data[len(data)-1] != '\n' {
		t.Fatalf("evidence missing or truncated: %q", data)
	}
	// Operator action: inspect and remove the marker; the chain resumes from
	// the visible record without a silent reset.
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	report, err = store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.Count != 1 {
		t.Fatalf("expected valid chain after marker removal: %+v", report)
	}
	second, err := store.Record(sampleInput())
	if err != nil {
		t.Fatalf("append after marker removal failed: %v", err)
	}
	if second.Sequence != 2 || second.PreviousHash != report.HeadHash {
		t.Fatalf("chain did not continue from visible head: %+v", second)
	}
}

func TestRecordDirectorySyncFaultOnlyOnCreation(t *testing.T) {
	// First record creates the trail file: the directory-sync boundary fires
	// and the ambiguous append marks the chain uncertain.
	inject := durablewrite.NewFaultMap(map[string]error{durablewrite.OpDirectorySync: durablewrite.ErrIO})
	store := faultStore(t, inject)
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected directory-sync fault on creation")
	}
	marker := filepath.Join(filepath.Dir(store.Path()), uncertainMarker)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("uncertain marker missing after creation fault: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	// Existing-file appends keep the production directory sync but do not
	// consult the directory-sync fault boundary.
	second, err := store.Record(sampleInput())
	if err != nil {
		t.Fatalf("append to existing file failed: %v", err)
	}
	if second.Sequence != 2 {
		t.Fatalf("sequence=%d", second.Sequence)
	}
}

func TestRecordClassification(t *testing.T) {
	cases := []struct {
		name string
		fail error
		want string
	}{
		{"enospc", durablewrite.ErrDiskFull, "disk_full"},
		{"edquot", durablewrite.ErrQuotaExceeded, "quota_exceeded"},
		{"erofs", durablewrite.ErrReadOnlyFilesystem, "filesystem_read_only"},
		{"eio", durablewrite.ErrIO, "durable_write_failed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := faultStore(t, durablewrite.NewFaultMap(map[string]error{durablewrite.OpWrite: c.fail}))
			_, err := store.Record(sampleInput())
			if err == nil {
				t.Fatal("expected fault")
			}
			if got := durablewrite.Classify(err); got != c.want {
				t.Fatalf("Classify=%q want %q (err=%v)", got, c.want, err)
			}
		})
	}
}

func TestRecordNoFalseSuccess(t *testing.T) {
	ops := []string{
		durablewrite.OpCreate,
		durablewrite.OpWrite,
		durablewrite.OpShortWrite,
		durablewrite.OpFileSync,
		durablewrite.OpDirectorySync,
	}
	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			store := faultStore(t, durablewrite.NewFaultMap(map[string]error{op: durablewrite.ErrIO}))
			if _, err := store.Record(sampleInput()); err == nil {
				t.Fatalf("op %s returned success", op)
			}
		})
	}
}

func TestRecordConcurrentWithInjectorRaceSafe(t *testing.T) {
	store := faultStore(t, durablewrite.NewFaultMap(map[string]error{}))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input := sampleInput()
			input.OperationID = input.OperationID + string(rune('a'+i))
			input.Context.RequestID = input.OperationID
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
		t.Fatalf("report=%+v", report)
	}
	events, err := store.Events()
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Sequence != int64(i+1) || events[i].PreviousHash != events[i-1].EventHash {
			t.Fatalf("chain broken at %d", i)
		}
	}
}

func TestRecordRedactsAuthorizationHeader(t *testing.T) {
	store := faultStore(t, nil)
	input := sampleInput()
	input.Metadata = map[string]string{
		"authorization": "Bearer ghp_super_secret",
		"x-api-key":     "sk-live-abcdef",
	}
	event, err := store.Record(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := event.Metadata["authorization"]; !strings.HasPrefix(got, "[redacted sha256:") {
		t.Fatalf("authorization not redacted: %q", got)
	}
	if got := event.Metadata["x-api-key"]; !strings.HasPrefix(got, "[redacted sha256:") {
		t.Fatalf("x-api-key not redacted: %q", got)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super_secret") || strings.Contains(string(raw), "sk-live-abcdef") {
		t.Fatal("secret leaked into operator audit trail")
	}
}

func TestVerifyReportsUncertainWithoutTruncating(t *testing.T) {
	store := faultStore(t, nil)
	if _, err := store.Record(sampleInput()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	// A sync failure after bytes were written marks the chain uncertain.
	store.Injector = durablewrite.NewFaultMap(map[string]error{durablewrite.OpFileSync: durablewrite.ErrIO})
	if _, err := store.Record(sampleInput()); err == nil {
		t.Fatal("expected file-sync fault")
	}
	after, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	// The chain grew by the visible record; nothing was truncated or reset.
	if len(after) <= len(before) || !strings.HasPrefix(string(after), string(before)) {
		t.Fatalf("trail truncated or reset: before=%d after=%d", len(before), len(after))
	}
}
