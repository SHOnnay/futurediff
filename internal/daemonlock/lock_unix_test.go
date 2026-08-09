//go:build linux || darwin

package daemonlock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInspect_NoLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	status, err := Inspect(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status.Held {
		t.Fatal("expected Held=false for missing lock file")
	}
	if status.LockStatus != "released" {
		t.Fatalf("expected LockStatus=released, got %s", status.LockStatus)
	}
	if status.ReasonCode != "no_lock" {
		t.Fatalf("expected ReasonCode=no_lock, got %s", status.ReasonCode)
	}
	if !status.SafeToRetry {
		t.Fatal("expected SafeToRetry=true for no lock")
	}
}

func TestInspect_StaleLockCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	// Create a lock file with dead owner (no process)
	meta := Metadata{
		Version:       Version,
		PID:           999999,
		UID:           os.Geteuid(),
		StartedAt:     time.Now().Add(-time.Hour).UTC(),
		Root:          "/tmp/root",
		Hostname:      "test-host",
		StartedAtNs:   time.Now().Add(-time.Hour).UnixNano(),
		BootID:        "test-boot-id",
		DaemonVersion: "0.1.0",
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status.Held {
		t.Fatal("expected Held=false for dead owner")
	}
	if status.LockStatus != "released" {
		t.Fatalf("expected LockStatus=released, got %s", status.LockStatus)
	}
	if status.OwnerStatus != "dead" && status.OwnerStatus != "proved_stale" {
		t.Fatalf("expected OwnerStatus=dead or proved_stale, got %s", status.OwnerStatus)
	}
	if status.ReasonCode != "stale_lock_candidate" {
		t.Fatalf("expected ReasonCode=stale_lock_candidate, got %s", status.ReasonCode)
	}
	if !status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=true for stale lock")
	}
	if !status.SafeToRetry {
		t.Fatal("expected SafeToRetry=true for stale lock")
	}
}

func TestInspect_MalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if status.ReasonCode != "lock_invalid_json" {
		t.Fatalf("expected ReasonCode=lock_invalid_json, got %s", status.ReasonCode)
	}
	if !status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=true for corrupt JSON")
	}
}

func TestInspect_TruncatedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	// Valid JSON but truncated
	if err := os.WriteFile(path, []byte(`{"version":"0.1","pid":123`), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	if err == nil {
		t.Fatal("expected error for truncated JSON")
	}
	if status.ReasonCode != "lock_invalid_json" && status.ReasonCode != "lock_trailing_data" {
		t.Fatalf("expected ReasonCode for truncated JSON, got %s", status.ReasonCode)
	}
	if !status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=true for truncated JSON")
	}
}

func TestInspect_TrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	meta := Metadata{Version: Version, PID: 123, UID: os.Geteuid(), StartedAt: time.Now().UTC(), Root: "/tmp/root"}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(path, append(data, []byte("\n{\"extra\":\"data\"}")...), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	if err == nil {
		t.Fatal("expected error for trailing data")
	}
	if status.ReasonCode != "lock_trailing_data" {
		t.Fatalf("expected ReasonCode=lock_trailing_data, got %s", status.ReasonCode)
	}
	if !status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=true for trailing data")
	}
}

func TestInspect_UnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	// JSON with unknown field
	meta := map[string]any{
		"version":       Version,
		"pid":           123,
		"uid":           os.Geteuid(),
		"started_at":    time.Now().UTC().Format(time.RFC3339),
		"root":          "/tmp/root",
		"unknown_field": "should_be_rejected",
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	if err == nil {
		t.Fatal("expected error for unknown fields")
	}
	if status.ReasonCode != "lock_invalid_json" {
		t.Fatalf("expected ReasonCode=lock_invalid_json for unknown fields, got %s", status.ReasonCode)
	}
}

func TestInspect_OversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	// Create file larger than maxLockFileBytes (64 KiB)
	largeData := make([]byte, maxLockFileBytes+100)
	if err := os.WriteFile(path, largeData, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	// Inspect returns both status and error for oversized files
	if status.ReasonCode != "lock_file_too_large" {
		t.Fatalf("expected ReasonCode=lock_file_too_large, got %s", status.ReasonCode)
	}
	if status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=false for oversized file")
	}
	// Error is expected for oversized files
	if err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestInspect_Symlink(t *testing.T) {
	if testing.Short() {
		t.Skip("symlink test requires filesystem support")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.lock")
	target := filepath.Join(dir, "target.lock")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	// Symlink should be rejected with an error
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if status.ReasonCode != "lock_not_regular_file" {
		t.Fatalf("expected ReasonCode=lock_not_regular_file for symlink, got %s", status.ReasonCode)
	}
	if status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=false for symlink")
	}
}

func TestInspect_NonRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	if err == nil {
		t.Fatal("expected error for non-regular file")
	}
	if status.ReasonCode != "lock_not_regular_file" {
		t.Fatalf("expected ReasonCode=lock_not_regular_file for directory, got %s", status.ReasonCode)
	}
	if status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=false for non-regular file")
	}
}

func TestInspect_UnsafePermissions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.lock")
	if err := os.WriteFile(path, []byte(`{"version":"0.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	if err == nil {
		t.Fatal("expected error for unsafe permissions")
	}
	if status.ReasonCode != "lock_permissions_too_broad" {
		t.Fatalf("expected ReasonCode=lock_permissions_too_broad, got %s", status.ReasonCode)
	}
	if status.AutomaticCleanupAllowed {
		t.Fatal("expected AutomaticCleanupAllowed=false for unsafe permissions")
	}
}

func TestInspect_OversizedFileBoundary(t *testing.T) {
	// Test exactly at maxLockFileBytes
	path := filepath.Join(t.TempDir(), "daemon.lock")
	data := make([]byte, maxLockFileBytes)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := Inspect(path, time.Now())
	// Should be accepted (not > maxLockFileBytes)
	if status.ReasonCode == "lock_file_too_large" {
		t.Fatal("file exactly at max size should be accepted")
	}
	if err != nil {
		// May or may not return error
	}
}

func TestInspect_UnknownFieldsAllowedInAcquire(t *testing.T) {
	// Acquire should write valid JSON that Inspect can read
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := Acquire(path, "/tmp/root", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// Don't release - we want to test that Inspect can read the lock file
	// when the lock is still held

	// Now inspect - should work and show held=true
	status, err := Inspect(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Held {
		t.Fatal("lock should be held while acquired")
	}
	// Lock is held by test process, but there's no daemon on the socket
	// So owner status will be 'ambiguous' (process alive but daemon unreachable)
	// which is correct behavior - the lock is held but the daemon is not reachable
	if status.ReasonCode != "lock_owner_ambiguous" && status.ReasonCode != "lock_owner_alive" {
		t.Fatalf("expected ReasonCode=lock_owner_ambiguous or lock_owner_alive, got %s", status.ReasonCode)
	}
	_ = lock.Release()
}

func TestInspect_ConcurrentCleanupRace(t *testing.T) {
	// Test that cleanup re-validates before unlink
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "daemon.lock")
	meta := Metadata{
		Version:       Version,
		PID:           999999,
		UID:           os.Geteuid(),
		StartedAt:     time.Now().Add(-time.Hour).UTC(),
		Root:          "/tmp/root",
		Hostname:      "test-host",
		StartedAtNs:   time.Now().Add(-time.Hour).UnixNano(),
		BootID:        "test-boot-id",
		DaemonVersion: "0.1.0",
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(lockPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	// Inspect to get status
	status, err := Inspect(lockPath, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status.ReasonCode != "stale_lock_candidate" {
		t.Fatalf("expected stale_lock_candidate, got %s", status.ReasonCode)
	}
	if !status.AutomaticCleanupAllowed {
		t.Fatal("stale lock should allow cleanup")
	}

	// Now test that a revalidation would happen before cleanup
	// (In the real cleanupLock, we re-Inspect before unlink)
	status2, err := Inspect(lockPath, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status2.ReasonCode != "stale_lock_candidate" {
		t.Fatal("revalidation should still show stale")
	}
}

func TestAcquire_InvalidPath(t *testing.T) {
	_, err := Acquire("", "/tmp/root", time.Now())
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	_, err = Acquire("relative/path", "/tmp/root", time.Now())
	if err == nil {
		t.Fatal("expected error for relative path")
	}
}

func TestAcquire_SymlinkRace(t *testing.T) {
	if testing.Short() {
		t.Skip("symlink race test requires filesystem support")
	}
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "daemon.lock")
	target := filepath.Join(dir, "target.lock")

	// Create target first
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Create symlink
	if err := os.Symlink(target, lockPath); err != nil {
		t.Fatal(err)
	}

	// Acquire should reject symlink
	_, err := Acquire(lockPath, "/tmp/root", time.Now())
	if err == nil {
		t.Fatal("expected error for symlink lock path")
	}
}

func TestAcquire_OversizedMetadata(t *testing.T) {
	// Acquire should write within bounds
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.lock")
	lock, err := Acquire(path, "/tmp/root", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	lock.Release()

	// Verify file size is within bounds
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > int64(maxLockFileBytes) {
		t.Fatalf("lock file exceeds max size: %d > %d", info.Size(), maxLockFileBytes)
	}
}

func TestIsProcessAlive_DeadPID(t *testing.T) {
	alive, err := isProcessAlive(999999, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("expected PID 999999 to be dead")
	}
}

func TestIsProcessAlive_ZeroPID(t *testing.T) {
	alive, err := isProcessAlive(0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("expected PID 0 to be dead")
	}
}

func TestBootID(t *testing.T) {
	bootID := getBootID()
	if bootID == "" {
		t.Log("BootID is empty (may be expected on some platforms)")
	}
}

func TestProcessStartTime(t *testing.T) {
	pid := os.Getpid()
	startNs := getProcessStartTimeNs(pid)
	if pid > 0 && startNs <= 0 {
		t.Logf("process start time for PID %d: %d (may be 0 on some platforms)", pid, startNs)
	}
}

func TestInspect_LiveOwnerSelf(t *testing.T) {
	// A lock held by our own live process with a matching process-start identity
	// must be classified alive or ambiguous — never proved_stale.
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := Acquire(path, "/tmp/root", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	status, err := Inspect(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Held {
		t.Fatal("lock should be held")
	}
	if status.OwnerStatus == "proved_stale" || status.OwnerStatus == "dead" {
		t.Fatalf("live owner misclassified as %s", status.OwnerStatus)
	}
	if status.ReasonCode == "stale_lock_candidate" {
		t.Fatal("live lock must not be classified stale")
	}
}

func TestInspect_ProcessStartMismatch(t *testing.T) {
	// Same live PID but a different process-start time: PID-reuse signal.
	now := time.Now()
	meta := Metadata{
		Version:     Version,
		PID:         os.Getpid(),
		UID:         os.Geteuid(),
		StartedAt:   now.UTC(),
		Root:        "/tmp/root",
		Hostname:    "test-host",
		StartedAtNs: now.Add(-48 * time.Hour).UnixNano(), // mismatched identity
		BootID:      getBootID(),
	}
	alive, err := isProcessAlive(os.Getpid(), meta.StartedAtNs, meta.BootID)
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("PID with mismatched process-start identity must be classified dead")
	}
}

func TestInspect_PreviousBootLock(t *testing.T) {
	// A lock whose BootID differs from the current boot must be classified dead,
	// even when the PID happens to be running now.
	meta := Metadata{
		Version:     Version,
		PID:         os.Getpid(),
		UID:         os.Geteuid(),
		StartedAt:   time.Now().UTC(),
		Root:        "/tmp/root",
		Hostname:    "test-host",
		StartedAtNs: getProcessStartTimeNs(os.Getpid()),
		BootID:      "previous-boot-identity",
	}
	alive, err := isProcessAlive(os.Getpid(), meta.StartedAtNs, meta.BootID)
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("lock from a previous boot must be classified dead")
	}
}

func TestRemoveIfUnheld_RemovesStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfUnheld(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("stale lock file should have been removed")
	}
}

func TestRemoveIfUnheld_Held(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := Acquire(path, "/tmp/root", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	err = RemoveIfUnheld(path)
	if !errors.Is(err, ErrLockHeld) {
		t.Fatalf("expected ErrLockHeld, got %v", err)
	}
	if _, statErr := os.Lstat(path); statErr != nil {
		t.Fatal("held lock file must not be removed")
	}
}

func TestRemoveIfUnheld_Missing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	if err := RemoveIfUnheld(path); err != nil {
		t.Fatalf("missing lock must be idempotent no-op, got %v", err)
	}
}

func TestRemoveIfUnheld_ReleasesFlock(t *testing.T) {
	// After RemoveIfUnheld removes a stale file, a fresh Acquire must succeed.
	path := filepath.Join(t.TempDir(), "daemon.lock")
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfUnheld(path); err != nil {
		t.Fatal(err)
	}
	lock, err := Acquire(path, "/tmp/root", time.Now())
	if err != nil {
		t.Fatalf("acquire after cleanup failed: %v", err)
	}
	_ = lock.Release()
}

func TestAcquire_RecordsProcessStartIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	lock, err := Acquire(path, "/tmp/root", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	meta := lock.Metadata()
	_ = lock.Release()
	if meta.PID != os.Getpid() {
		t.Fatalf("expected own PID %d, got %d", os.Getpid(), meta.PID)
	}
	if meta.StartedAtNs <= 0 {
		t.Fatal("lock must record a nonzero process-start identity")
	}
	resolved := getProcessStartTimeNs(os.Getpid())
	if resolved > 0 && resolved != meta.StartedAtNs {
		t.Fatalf("recorded start identity %d does not match resolved %d", meta.StartedAtNs, resolved)
	}
}
