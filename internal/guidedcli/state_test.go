package guidedcli

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

func realTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func writeStateFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStateStoreSaveLoadAndPermissions(t *testing.T) {
	dir := realTempDir(t)
	store := StateStore{Path: filepath.Join(dir, "state", "current.json")}
	if err := store.Save("tx_123", "/repo"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.TransactionID != "tx_123" || got.RepositoryRoot != "/repo" {
		t.Fatalf("unexpected state: %+v", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("state permissions = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestStateStoreRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	dir := realTempDir(t)
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(`{"transaction_id":"tx_bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "current.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	store := StateStore{Path: link}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted a symlink")
	}
	if err := store.Save("tx_new", "/repo"); err == nil {
		t.Fatal("Save replaced a symlink")
	}
	if err := store.Clear(); err == nil {
		t.Fatal("Clear removed a symlink")
	}
}

func TestStateStoreRejectsSymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := realTempDir(t)
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedDir := filepath.Join(root, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatal(err)
	}
	store := StateStore{Path: filepath.Join(linkedDir, "current.json")}
	if err := store.Save("tx_bad", "/repo"); err == nil {
		t.Fatal("Save accepted a symlinked parent")
	}
}

func TestStateStoreRejectsBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not authoritative on Windows")
	}
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	if err := os.WriteFile(path, []byte("{\"transaction_id\":\"tx_open\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("state file with broad permissions was accepted")
	}
}

func TestStateStoreRejectsNonRegularFile(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted a directory as the state file")
	}
}

func TestStateStoreRejectsOversizedFile(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	big := `{"transaction_id":"tx_big","selected_at":"2026-08-02T00:00:00Z","pad":"` + strings.Repeat("x", maxStateFileBytes) + `"}`
	writeStateFile(t, path, big)
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted an oversized state file")
	}
}

func TestStateStoreRejectsUnknownFields(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	writeStateFile(t, path, `{"transaction_id":"tx_1","selected_at":"2026-08-02T00:00:00Z","evil":"payload"}`)
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted unknown fields")
	}
}

func TestStateStoreRejectsTrailingJSON(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	writeStateFile(t, path, `{"transaction_id":"tx_1","selected_at":"2026-08-02T00:00:00Z"} {"second":"object"}`)
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted trailing JSON")
	}
}

func TestStateStoreRejectsMalformedJSON(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	writeStateFile(t, path, `{"transaction_id":`)
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted malformed JSON")
	}
}

func TestStateStoreRejectsMissingTransactionID(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	writeStateFile(t, path, `{"selected_at":"2026-08-02T00:00:00Z"}`)
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted a file without transaction_id")
	}
}

func TestStateStoreRejectsInvalidTransactionIDShape(t *testing.T) {
	for _, id := range []string{"tx_", "123", "", "tx_!"} {
		dir := realTempDir(t)
		path := filepath.Join(dir, "current.json")
		writeStateFile(t, path, `{"transaction_id":`+jsonString(id)+`,"selected_at":"2026-08-02T00:00:00Z"}`)
		store := StateStore{Path: path}
		if _, err := store.Load(); err == nil {
			t.Fatalf("Load accepted invalid transaction_id %q", id)
		}
		if err := store.Save(id, "/repo"); err == nil {
			t.Fatalf("Save accepted invalid transaction_id %q", id)
		}
	}
}

func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestStateStoreRejectsRelativeRepositoryRoot(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	writeStateFile(t, path, `{"transaction_id":"tx_1","repository_root":"relative/path","selected_at":"2026-08-02T00:00:00Z"}`)
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted a relative repository_root")
	}
}

func TestStateStoreRejectsZeroAndFutureSelectedAt(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "current.json")
	writeStateFile(t, path, `{"transaction_id":"tx_1"}`)
	store := StateStore{Path: path}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted a file without selected_at")
	}
	future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	writeStateFile(t, path, `{"transaction_id":"tx_1","selected_at":`+jsonString(future)+`}`)
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted a far-future selected_at")
	}
}

func TestStateStoreSaveValidatesBeforeTouch(t *testing.T) {
	dir := realTempDir(t)
	store := StateStore{Path: filepath.Join(dir, "current.json")}
	if err := store.Save("tx_good", "/repo"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.Path)
	if err := store.Save("", "/repo"); err == nil {
		t.Fatal("Save accepted an empty transaction ID")
	}
	after, _ := os.ReadFile(store.Path)
	if string(before) != string(after) {
		t.Fatal("Save mutated the file before validating the transaction ID")
	}
}

func TestStateStoreConcurrentAccess(t *testing.T) {
	dir := realTempDir(t)
	store := StateStore{Path: filepath.Join(dir, "current.json")}
	ids := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		ids = append(ids, "tx_"+string(rune('a'+i)))
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := ids[n]
			for j := 0; j < 50; j++ {
				if j%3 == 0 {
					_ = store.Clear()
				}
				if err := store.Save(id, "/repo"); err != nil {
					errs <- err
					return
				}
				current, err := store.Load()
				if err == os.ErrNotExist {
					continue
				}
				if err != nil {
					errs <- err
					return
				}
				if !validTransactionID(current.TransactionID) {
					errs <- &corruptStateError{current.TransactionID}
					return
				}
			}
		}(n)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

type corruptStateError struct {
	ID string
}

func (e *corruptStateError) Error() string {
	return "concurrent access observed corrupt state: " + e.ID
}

func TestStateStoreRejectsSwapAfterValidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O_NOFOLLOW semantics are POSIX-specific")
	}
	dir := realTempDir(t)
	target := filepath.Join(dir, "target")
	path := filepath.Join(dir, "current.json")
	writeStateFile(t, path, `{"transaction_id":"tx_1","selected_at":"2026-08-02T00:00:00Z"}`)
	writeStateFile(t, target, `{"transaction_id":"tx_evil","selected_at":"2026-08-02T00:00:00Z"}`)
	store := StateStore{Path: path}
	// Remove the real file and place a symlink at the same path; Load must
	// refuse it rather than following the symlink.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load followed a symlink swapped in at the validated path")
	}
}

func TestDarwinStateStoreWorksBelowTmpAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS /tmp alias test")
	}
	rawRoot, err := os.MkdirTemp("/tmp", "futurediff-state-test-")
	if err != nil {
		t.Fatal(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(rawRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(resolvedRoot)
	if err := os.Chmod(resolvedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	store := StateStore{Path: filepath.Join(rawRoot, "current.json")}
	if err := store.Save("tx_tmp", "/repo"); err != nil {
		t.Fatalf("save below /tmp failed: %v", err)
	}
	current, err := store.Load()
	if err != nil {
		t.Fatalf("load below /tmp failed: %v", err)
	}
	if current.TransactionID != "tx_tmp" {
		t.Fatalf("unexpected state: %+v", current)
	}
}
