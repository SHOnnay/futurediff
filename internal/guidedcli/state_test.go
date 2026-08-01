package guidedcli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
