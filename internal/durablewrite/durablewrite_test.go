package durablewrite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeDest(t *testing.T, dir, name, content string) string {
	t.Helper()
	dest := filepath.Join(dir, name)
	if err := os.WriteFile(dest, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dest
}

func readDest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func leftoverTemps(t *testing.T, dir, name string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, name+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestReplaceFileSuccess(t *testing.T) {
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	if err := ReplaceFile(dest, []byte("new"), 0o640, nil); err != nil {
		t.Fatal(err)
	}
	if got := readDest(t, dest); got != "new" {
		t.Fatalf("content=%q", got)
	}
	st, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("perm=%o", st.Mode().Perm())
	}
	if temps := leftoverTemps(t, filepath.Dir(dest), "a.json"); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestReplaceFileCreateFailure(t *testing.T) {
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	inject := NewFaultMap(map[string]error{OpCreate: ErrIO})
	err := ReplaceFile(dest, []byte("new"), 0o600, inject)
	if err == nil {
		t.Fatal("expected create failure")
	}
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Code() != "create_failure" {
		t.Fatalf("err=%v", err)
	}
	if got := readDest(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
}

func TestReplaceFileWriteFailure(t *testing.T) {
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	inject := NewFaultMap(map[string]error{OpWrite: ErrIO})
	err := ReplaceFile(dest, []byte("new"), 0o600, inject)
	if err == nil {
		t.Fatal("expected write failure")
	}
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Code() != "write_failure" {
		t.Fatalf("err=%v", err)
	}
	if got := readDest(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if temps := leftoverTemps(t, filepath.Dir(dest), "a.json"); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestReplaceFileShortWriteLeavesNoAuthoritativeTemp(t *testing.T) {
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	inject := NewFaultMap(map[string]error{OpShortWrite: ErrIO})
	err := ReplaceFile(dest, []byte("this is a longer payload that cannot be truncated"), 0o600, inject)
	if err == nil {
		t.Fatal("expected short-write failure")
	}
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Code() != "short_write" {
		t.Fatalf("err=%v", err)
	}
	if got := readDest(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if temps := leftoverTemps(t, filepath.Dir(dest), "a.json"); len(temps) != 0 {
		t.Fatalf("partial temp leaked: %v", temps)
	}
}

func TestReplaceFileFileSyncFailure(t *testing.T) {
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	inject := NewFaultMap(map[string]error{OpFileSync: ErrIO})
	err := ReplaceFile(dest, []byte("new"), 0o600, inject)
	if err == nil {
		t.Fatal("expected file-sync failure")
	}
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Code() != "sync_failure" {
		t.Fatalf("err=%v", err)
	}
	if got := readDest(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if temps := leftoverTemps(t, filepath.Dir(dest), "a.json"); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestReplaceFileRenameFailure(t *testing.T) {
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	inject := NewFaultMap(map[string]error{OpRename: ErrIO})
	err := ReplaceFile(dest, []byte("new"), 0o600, inject)
	if err == nil {
		t.Fatal("expected rename failure")
	}
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Code() != "rename_failure" {
		t.Fatalf("err=%v", err)
	}
	if got := readDest(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if temps := leftoverTemps(t, filepath.Dir(dest), "a.json"); len(temps) != 0 {
		t.Fatalf("leftover temps: %v", temps)
	}
}

func TestReplaceFileDirectorySyncFailureIsReported(t *testing.T) {
	// Rename has committed by this point; the new content is authoritative but
	// the caller must still learn the directory entry may not be durable. This
	// is the "no false success" contract.
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	inject := NewFaultMap(map[string]error{OpDirectorySync: ErrIO})
	err := ReplaceFile(dest, []byte("new"), 0o600, inject)
	if err == nil {
		t.Fatal("expected directory-sync failure")
	}
	var fe *FaultError
	if !errors.As(err, &fe) || fe.Code() != "dir_sync_failure" {
		t.Fatalf("err=%v", err)
	}
	if got := readDest(t, dest); got != "new" {
		t.Fatalf("dest=%q", got)
	}
}

func TestClassifyMappings(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{ErrDiskFull, "disk_full"},
		{ErrQuotaExceeded, "quota_exceeded"},
		{ErrReadOnlyFilesystem, "filesystem_read_only"},
		{ErrIO, "durable_write_failed"},
		{fmt.Errorf("wrapped: %w", ErrDiskFull), "disk_full"},
		{errors.New("unexpected"), "durable_write_failed"},
	}
	for _, c := range cases {
		if got := Classify(c.err); got != c.want {
			t.Errorf("Classify(%v)=%q want %q", c.err, got, c.want)
		}
	}
}

func TestErrorsIsThroughFaultError(t *testing.T) {
	err := wrapFault(OpCreate, "/tmp/a.json", ErrDiskFull)
	if !errors.Is(err, ErrDiskFull) {
		t.Fatalf("errors.Is failed for %v", err)
	}
	if got := Classify(err); got != "disk_full" {
		t.Fatalf("Classify=%q", got)
	}
}

func TestOneShotRetrySucceeds(t *testing.T) {
	dest := writeDest(t, t.TempDir(), "a.json", "old")
	inject := NewOneShot(map[string]error{OpCreate: ErrIO})
	if err := ReplaceFile(dest, []byte("new"), 0o600, inject); err == nil {
		t.Fatal("expected first attempt to fail")
	}
	if got := readDest(t, dest); got != "old" {
		t.Fatalf("dest changed: %q", got)
	}
	if err := ReplaceFile(dest, []byte("new"), 0o600, inject); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if got := readDest(t, dest); got != "new" {
		t.Fatalf("dest=%q", got)
	}
}

func TestConcurrentReplaceFileRaceSafe(t *testing.T) {
	dir := t.TempDir()
	inject := NewFaultMap(map[string]error{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dest := filepath.Join(dir, fmt.Sprintf("f%d.json", i))
			if err := ReplaceFile(dest, []byte(fmt.Sprintf("payload-%d", i)), 0o600, inject); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 16; i++ {
		if got := readDest(t, filepath.Join(dir, fmt.Sprintf("f%d.json", i))); got != fmt.Sprintf("payload-%d", i) {
			t.Fatalf("f%d=%q", i, got)
		}
	}
}
