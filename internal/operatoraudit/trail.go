package operatoraudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SHOnnay/futurediff/internal/domain"
	"github.com/SHOnnay/futurediff/internal/durablewrite"
)

const Version = "0.1"

const (
	ResultRequested = "requested"
	ResultSucceeded = "succeeded"
	ResultDenied    = "denied"
	ResultFailed    = "failed"

	PolicyAllow         = "allow"
	PolicyDeny          = "deny"
	PolicyNotApplicable = "not_applicable"
)

type Actor struct {
	PrincipalID string `json:"principal_id,omitempty"`
	PeerUID     uint32 `json:"peer_uid,omitempty"`
	Source      string `json:"source"`
}

type ExecutionContext struct {
	Component string `json:"component"`
	RequestID string `json:"request_id,omitempty"`
	Method    string `json:"method,omitempty"`
	Path      string `json:"path,omitempty"`
}

type Target struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id,omitempty"`
}

type Event struct {
	SchemaVersion  string            `json:"schema_version"`
	Sequence       int64             `json:"sequence"`
	EventID        string            `json:"event_id"`
	OperationID    string            `json:"operation_id"`
	TransactionID  string            `json:"transaction_id,omitempty"`
	OccurredAt     time.Time         `json:"occurred_at"`
	Actor          Actor             `json:"actor"`
	Context        ExecutionContext  `json:"context"`
	EventType      string            `json:"event_type"`
	Target         Target            `json:"target"`
	Result         string            `json:"result"`
	PolicyDecision string            `json:"policy_decision"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Redacted       []string          `json:"redacted,omitempty"`
	PreviousHash   string            `json:"previous_hash,omitempty"`
	EventHash      string            `json:"event_hash"`
}

type Input struct {
	OperationID    string
	TransactionID  string
	Actor          Actor
	Context        ExecutionContext
	EventType      string
	Target         Target
	Result         string
	PolicyDecision string
	Metadata       map[string]string
}

type Report struct {
	Path         string    `json:"path"`
	Valid        bool      `json:"valid"`
	Count        int       `json:"count"`
	HeadHash     string    `json:"head_hash,omitempty"`
	Findings     []string  `json:"findings,omitempty"`
	VerifiedAt   time.Time `json:"verified_at"`
	Schema       string    `json:"schema_version"`
	ExportFormat string    `json:"export_format"`
}

type Store struct {
	Root string
	Now  func() time.Time
	// Injector is a test-only deterministic durable-write fault-injection seam
	// (ADR-099). Production callers leave it nil and every method behaves
	// exactly as before; nothing outside tests constructs an injector.
	Injector durablewrite.Injector
	mu       sync.Mutex
}

// uncertainMarker is written next to the trail when an append may be partially
// visible without confirmed durability (a sync or directory-sync failure after
// bytes were written). Verification then reports the chain as uncertain and
// appends fail closed until an operator inspects the trail and removes the
// marker. The marker is how an ambiguous append is distinguished from a record
// that never existed.
const uncertainMarker = ".operator-events.uncertain"

type metadataEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type hashMaterial struct {
	SchemaVersion  string           `json:"schema_version"`
	Sequence       int64            `json:"sequence"`
	EventID        string           `json:"event_id"`
	OperationID    string           `json:"operation_id"`
	TransactionID  string           `json:"transaction_id,omitempty"`
	OccurredAt     time.Time        `json:"occurred_at"`
	Actor          Actor            `json:"actor"`
	Context        ExecutionContext `json:"context"`
	EventType      string           `json:"event_type"`
	Target         Target           `json:"target"`
	Result         string           `json:"result"`
	PolicyDecision string           `json:"policy_decision"`
	Metadata       []metadataEntry  `json:"metadata,omitempty"`
	Redacted       []string         `json:"redacted,omitempty"`
	PreviousHash   string           `json:"previous_hash,omitempty"`
}

func (s *Store) Path() string { return filepath.Join(s.Root, "audit", "operator-events.jsonl") }

func (s *Store) Verify() (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifyLocked()
}

func (s *Store) Events() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eventsLocked()
}

func (s *Store) Record(input Input) (Event, error) {
	if !filepath.IsAbs(s.Root) {
		return Event{}, errors.New("operator audit root must be absolute")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.Path())
	if err := ensurePrivateDir(dir); err != nil {
		return Event{}, err
	}
	var recorded Event
	err := withFileLock(filepath.Join(dir, ".operator-events.lock"), func() error {
		report, events, err := verifyTrailFile(s.Path(), nowUTC(s.Now))
		if err != nil {
			return err
		}
		if !report.Valid {
			return fmt.Errorf("operator audit trail is not appendable: %s", strings.Join(report.Findings, "; "))
		}
		event, err := buildEvent(input, int64(len(events)+1), report.HeadHash, nowUTC(s.Now))
		if err != nil {
			return err
		}
		line, err := json.Marshal(event)
		if err != nil {
			return err
		}
		_, statErr := os.Stat(s.Path())
		created := os.IsNotExist(statErr)
		if statErr != nil && !created {
			return fmt.Errorf("operator audit append: stat: %w", statErr)
		}
		if s.Injector != nil {
			if err := s.Injector.Before(durablewrite.OpCreate); err != nil {
				// Nothing has been written yet; the state is unchanged and a
				// retry is safe.
				return fmt.Errorf("operator audit append: create/open: %w", err)
			}
		}
		f, err := os.OpenFile(s.Path(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			return fmt.Errorf("operator audit append: open: %w", err)
		}
		defer f.Close()
		if err := ensurePrivateRegularFile(s.Path()); err != nil {
			return fmt.Errorf("operator audit append: %w", err)
		}
		if s.Injector != nil {
			if err := s.Injector.Before(durablewrite.OpWrite); err != nil {
				// Nothing has been written yet; the state is unchanged and a
				// retry is safe.
				return fmt.Errorf("operator audit append: write: %w", err)
			}
		}
		payload := append(line, '\n')
		if s.Injector != nil {
			if err := s.Injector.Before(durablewrite.OpShortWrite); err != nil {
				// Simulate a short write: a prefix of the line is visible, then
				// the append fails. The partial bytes are preserved as evidence;
				// the next verification detects the trailing partial line and
				// the trail fails closed until an operator truncates it. This is
				// an ambiguous append, not a clean no-op.
				if half := len(payload) / 2; half > 0 {
					_, _ = f.Write(payload[:half])
				}
				return fmt.Errorf("operator audit append: short write: %w", err)
			}
		}
		if _, err := f.Write(payload); err != nil {
			// A short or failed write may leave some bytes visible; preserve
			// them as evidence. Verification detects a trailing partial line
			// and the trail fails closed.
			return fmt.Errorf("operator audit append: write: %w", err)
		}
		if s.Injector != nil {
			if err := s.Injector.Before(durablewrite.OpFileSync); err != nil {
				// The full line is visible but durability is unconfirmed:
				// report the chain as uncertain and fail closed.
				_ = s.markUncertain(dir)
				return fmt.Errorf("operator audit append: file sync: %w", err)
			}
		}
		if err := f.Sync(); err != nil {
			_ = s.markUncertain(dir)
			return fmt.Errorf("operator audit append: file sync: %w", err)
		}
		// The audit event must be durable: both the file content and the
		// directory entry (first creation) must survive a crash. The fault
		// boundary for directory sync applies only when the file is newly
		// created; for later appends the directory entry already exists.
		if created && s.Injector != nil {
			if err := s.Injector.Before(durablewrite.OpDirectorySync); err != nil {
				_ = s.markUncertain(dir)
				return fmt.Errorf("operator audit append: directory sync: %w", err)
			}
		}
		if err := syncParentDir(s.Path()); err != nil {
			_ = s.markUncertain(dir)
			return fmt.Errorf("operator audit append: directory sync: %w", err)
		}
		recorded = event
		return nil
	})
	if err != nil {
		return Event{}, err
	}
	return recorded, nil
}

// markUncertain records that an append may be partially visible without
// confirmed durability, so subsequent verification reports the chain as
// uncertain and appends fail closed until an operator inspects the trail and
// removes the marker. Best-effort: if the marker itself cannot be written the
// original error is still returned and the trail file remains the evidence.
func (s *Store) markUncertain(dir string) error {
	return os.WriteFile(filepath.Join(dir, uncertainMarker), []byte("operator audit chain durability uncertain: an append failed after bytes were written; inspect the trail and remove this marker before appending again\n"), 0o600)
}

func (s *Store) verifyLocked() (Report, error) {
	dir := filepath.Dir(s.Path())
	if !filepath.IsAbs(s.Root) {
		return Report{}, errors.New("operator audit root must be absolute")
	}
	if err := ensurePrivateDir(dir); err != nil {
		return Report{}, err
	}
	var report Report
	err := withFileLock(filepath.Join(dir, ".operator-events.lock"), func() error {
		var events []Event
		var verifyErr error
		report, events, verifyErr = verifyTrailFile(s.Path(), nowUTC(s.Now))
		if verifyErr == nil && len(events) == 0 {
			_ = ensurePrivateDir(dir)
		}
		return verifyErr
	})
	return report, err
}

func (s *Store) eventsLocked() ([]Event, error) {
	dir := filepath.Dir(s.Path())
	if err := ensurePrivateDir(dir); err != nil {
		return nil, err
	}
	var events []Event
	err := withFileLock(filepath.Join(dir, ".operator-events.lock"), func() error {
		report, loaded, err := verifyTrailFile(s.Path(), nowUTC(s.Now))
		if err != nil {
			return err
		}
		if !report.Valid {
			return fmt.Errorf("operator audit trail is invalid: %s", strings.Join(report.Findings, "; "))
		}
		events = loaded
		return nil
	})
	return events, err
}

func buildEvent(input Input, sequence int64, previousHash string, now time.Time) (Event, error) {
	if strings.TrimSpace(input.OperationID) == "" {
		return Event{}, errors.New("operator audit operation_id is required")
	}
	if strings.TrimSpace(input.EventType) == "" {
		return Event{}, errors.New("operator audit event_type is required")
	}
	if strings.TrimSpace(input.Target.ResourceType) == "" {
		return Event{}, errors.New("operator audit target.resource_type is required")
	}
	switch input.Result {
	case ResultRequested, ResultSucceeded, ResultDenied, ResultFailed:
	default:
		return Event{}, fmt.Errorf("unsupported operator audit result %q", input.Result)
	}
	policy := input.PolicyDecision
	if policy == "" {
		policy = PolicyNotApplicable
	}
	switch policy {
	case PolicyAllow, PolicyDeny, PolicyNotApplicable:
	default:
		return Event{}, fmt.Errorf("unsupported operator audit policy decision %q", policy)
	}
	actor := input.Actor
	if strings.TrimSpace(actor.Source) == "" {
		actor.Source = "local"
	}
	if strings.TrimSpace(input.Context.Component) == "" {
		return Event{}, errors.New("operator audit context.component is required")
	}
	metadata, redacted := sanitizeMetadata(input.Metadata)
	e := Event{
		SchemaVersion:  Version,
		Sequence:       sequence,
		EventID:        domain.NewID("audit"),
		OperationID:    strings.TrimSpace(input.OperationID),
		TransactionID:  strings.TrimSpace(input.TransactionID),
		OccurredAt:     now.UTC().Truncate(time.Second),
		Actor:          actor,
		Context:        sanitizeContext(input.Context),
		EventType:      strings.TrimSpace(input.EventType),
		Target:         sanitizeTarget(input.Target),
		Result:         input.Result,
		PolicyDecision: policy,
		Metadata:       metadata,
		Redacted:       redacted,
		PreviousHash:   previousHash,
	}
	e.EventHash = hashEvent(e)
	return e, nil
}

func verifyTrailFile(path string, now time.Time) (Report, []Event, error) {
	report := Report{Path: path, Valid: true, VerifiedAt: now.UTC(), Schema: Version, ExportFormat: "jsonl"}
	if err := verifyPrivatePath(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		report.Valid = false
		report.Findings = append(report.Findings, err.Error())
	}
	// An ambiguous append (sync/directory-sync failure after bytes were
	// written) marks the chain uncertain: the trail may look complete, but
	// durability of the last record is unconfirmed. Fail closed until an
	// operator inspects the trail and removes the marker.
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), uncertainMarker)); err == nil {
		report.Valid = false
		report.Findings = append(report.Findings, "audit chain durability uncertain: a previous append failed after bytes were written; inspect the trail and remove "+uncertainMarker+" before appending")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return report, nil, nil
	}
	if err != nil {
		return report, nil, err
	}
	if len(data) == 0 {
		return report, nil, nil
	}
	if data[len(data)-1] != '\n' {
		report.Valid = false
		report.Findings = append(report.Findings, "trailing partial write detected")
	}
	lines := strings.Split(string(data), "\n")
	events := make([]Event, 0, len(lines))
	seenIDs := map[string]bool{}
	prev := ""
	for index, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("malformed record at line %d: %v", index+1, err))
			continue
		}
		if err := validateEvent(event); err != nil {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("invalid record at sequence %d: %v", event.Sequence, err))
		}
		if seenIDs[event.EventID] {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("duplicate event_id at sequence %d", event.Sequence))
		}
		seenIDs[event.EventID] = true
		if event.Sequence != int64(len(events)+1) {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("sequence mismatch at position %d", len(events)+1))
		}
		if event.PreviousHash != prev {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("previous hash mismatch at sequence %d", event.Sequence))
		}
		expected := hashEvent(event)
		if expected != event.EventHash {
			report.Valid = false
			report.Findings = append(report.Findings, fmt.Sprintf("event hash mismatch at sequence %d", event.Sequence))
		}
		prev = event.EventHash
		events = append(events, event)
	}
	report.Count = len(events)
	report.HeadHash = prev
	return report, events, nil
}

func validateEvent(event Event) error {
	if event.SchemaVersion != Version {
		return fmt.Errorf("unsupported schema version %q", event.SchemaVersion)
	}
	if event.Sequence < 1 {
		return errors.New("sequence must be positive")
	}
	if strings.TrimSpace(event.EventID) == "" {
		return errors.New("event_id is required")
	}
	if strings.TrimSpace(event.OperationID) == "" {
		return errors.New("operation_id is required")
	}
	if event.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}
	if strings.TrimSpace(event.Actor.Source) == "" {
		return errors.New("actor.source is required")
	}
	if strings.TrimSpace(event.Context.Component) == "" {
		return errors.New("context.component is required")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return errors.New("event_type is required")
	}
	if strings.TrimSpace(event.Target.ResourceType) == "" {
		return errors.New("target.resource_type is required")
	}
	if strings.TrimSpace(event.EventHash) == "" {
		return errors.New("event_hash is required")
	}
	return nil
}

func hashEvent(event Event) string {
	metadata := make([]metadataEntry, 0, len(event.Metadata))
	for key, value := range event.Metadata {
		metadata = append(metadata, metadataEntry{Key: key, Value: value})
	}
	sort.Slice(metadata, func(i, j int) bool { return metadata[i].Key < metadata[j].Key })
	redacted := append([]string(nil), event.Redacted...)
	sort.Strings(redacted)
	material := hashMaterial{
		SchemaVersion:  event.SchemaVersion,
		Sequence:       event.Sequence,
		EventID:        event.EventID,
		OperationID:    event.OperationID,
		TransactionID:  event.TransactionID,
		OccurredAt:     event.OccurredAt.UTC(),
		Actor:          event.Actor,
		Context:        event.Context,
		EventType:      event.EventType,
		Target:         event.Target,
		Result:         event.Result,
		PolicyDecision: event.PolicyDecision,
		Metadata:       metadata,
		Redacted:       redacted,
		PreviousHash:   event.PreviousHash,
	}
	canonical, _ := json.Marshal(material)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func sanitizeContext(ctx ExecutionContext) ExecutionContext {
	ctx.Component = strings.TrimSpace(ctx.Component)
	ctx.RequestID = strings.TrimSpace(ctx.RequestID)
	ctx.Method = strings.TrimSpace(strings.ToUpper(ctx.Method))
	ctx.Path = strings.TrimSpace(ctx.Path)
	return ctx
}

func sanitizeTarget(target Target) Target {
	target.ResourceType = strings.TrimSpace(target.ResourceType)
	target.ResourceID = strings.TrimSpace(target.ResourceID)
	return target
}

func sanitizeMetadata(input map[string]string) (map[string]string, []string) {
	if len(input) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	sort.Strings(keys)
	metadata := make(map[string]string, len(keys))
	redacted := make([]string, 0, len(keys))
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		value := strings.TrimSpace(input[key])
		clean, changed := sanitizeValue(key, value)
		metadata[key] = clean
		if changed {
			redacted = append(redacted, key)
		}
	}
	if len(redacted) == 0 {
		redacted = nil
	}
	return metadata, redacted
}

func sanitizeValue(key, value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if isSensitiveKey(key) || isSensitiveValue(value) {
		return redactValue(value), true
	}
	if len(value) > 256 {
		return fmt.Sprintf("[truncated sha256:%s]", shortDigest(value)), true
	}
	return value, false
}

func RedactText(value string) string {
	clean, changed := sanitizeValue("message", value)
	if changed {
		return clean
	}
	return clean
}

func redactValue(value string) string {
	return fmt.Sprintf("[redacted sha256:%s]", shortDigest(value))
}

func shortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:6])
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"secret", "token", "password", "authorization", "cookie", "bearer", "private_key", "env_dump", "provider_error_body"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isSensitiveValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, "-----BEGIN ") {
		return true
	}
	for _, marker := range []string{"bearer ", "ghp_", "github_pat_", "xoxb-", "xoxp-", "xoxa-", "sk-"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func verifyPrivatePath(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return errors.New("operator audit trail path must not be a symlink")
	}
	if !st.Mode().IsRegular() {
		return errors.New("operator audit trail path must be a regular file")
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		return fmt.Errorf("operator audit trail file permissions must be 0600, found %03o", st.Mode().Perm())
	}
	dir := filepath.Dir(path)
	dst, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && dst.Mode().Perm() != 0o700 {
		return fmt.Errorf("operator audit directory permissions must be 0700, found %03o", dst.Mode().Perm())
	}
	return nil
}

func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(path, 0o700)
	}
	return nil
}

func ensurePrivateRegularFile(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return errors.New("operator audit trail file must not be a symlink")
	}
	if !st.Mode().IsRegular() {
		return errors.New("operator audit trail file must be regular")
	}
	if runtime.GOOS != "windows" {
		return os.Chmod(path, 0o600)
	}
	return nil
}

func syncParentDir(path string) error {
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func nowUTC(now func() time.Time) time.Time {
	if now != nil {
		return now().UTC()
	}
	return time.Now().UTC()
}
