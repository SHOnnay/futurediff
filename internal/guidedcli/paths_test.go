package guidedcli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolvePathConfigPrecedenceAndDerivation(t *testing.T) {
	t.Setenv("FDIF_HOME", filepath.Join(t.TempDir(), "env-home"))
	t.Setenv("FUTUREDIFF_ROOT", filepath.Join(t.TempDir(), "legacy-home"))
	t.Setenv("FUTUREDIFF_SOCKET", "")
	explicit := filepath.Join(t.TempDir(), "explicit-home")
	paths, err := resolvePathConfig(Options{Home: explicit})
	if err != nil {
		t.Fatal(err)
	}
	wantHome, err := canonicalizeHomePath(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Home.Path != wantHome || paths.Home.Source != pathSourceHomeFlag {
		t.Fatalf("home = %+v, want %s from %s", paths.Home, wantHome, pathSourceHomeFlag)
	}
	if paths.State.Path != filepath.Join(wantHome, "current-transaction.json") {
		t.Fatalf("state = %s", paths.State.Path)
	}
	if paths.Socket.Path != filepath.Join(wantHome, "futurediff.sock") {
		t.Fatalf("socket = %s", paths.Socket.Path)
	}
	if paths.Runtime.Path != filepath.Join(wantHome, "runtime") || paths.WorkspaceRoot.Path != paths.Runtime.Path {
		t.Fatalf("runtime/workspace mismatch: %+v", paths)
	}
}

func TestResolvePathConfigUsesFDIFHomeBeforeLegacyRoot(t *testing.T) {
	fdifHome := filepath.Join(t.TempDir(), "fdif")
	t.Setenv("FDIF_HOME", fdifHome)
	t.Setenv("FUTUREDIFF_ROOT", filepath.Join(t.TempDir(), "legacy"))
	paths, err := resolvePathConfig(Options{})
	if err != nil {
		t.Fatal(err)
	}
	wantHome, err := canonicalizeHomePath(fdifHome)
	if err != nil {
		t.Fatal(err)
	}
	if paths.Home.Source != pathSourceFDIFHome || paths.Home.Path != wantHome {
		t.Fatalf("unexpected home: %+v", paths.Home)
	}
}

func TestResolvePathConfigKeepsAdvancedStateOverrideSeparate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	state := filepath.Join(t.TempDir(), "selection", "current.json")
	paths, err := resolvePathConfig(Options{Home: home, StatePath: state})
	if err != nil {
		t.Fatal(err)
	}
	wantState, err := canonicalizeFilePath(state)
	if err != nil {
		t.Fatal(err)
	}
	wantHome, err := canonicalizeHomePath(home)
	if err != nil {
		t.Fatal(err)
	}
	if paths.State.Path != wantState || paths.State.Source != pathSourceStateFlag {
		t.Fatalf("unexpected state: %+v", paths.State)
	}
	if paths.Runtime.Path != filepath.Join(wantHome, "runtime") {
		t.Fatalf("state override unexpectedly changed runtime: %+v", paths)
	}
}

func TestCanonicalizeRejectsArbitrarySymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	_, err := canonicalizeFilePath(filepath.Join(link, "current.json"))
	if err == nil || !strings.Contains(err.Error(), "symlinked directory") {
		t.Fatalf("arbitrary symlink parent accepted: %v", err)
	}
}

func TestCanonicalizeRejectsHomeThatIsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "home-link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	if _, err := canonicalizeHomePath(link); err == nil {
		t.Fatal("symlink home accepted")
	}
}

func TestDarwinTmpAliasCanonicalizesUnderPrivateTmp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS platform alias test")
	}
	path := filepath.Join("/tmp", "futurediff-path-test", "current.json")
	got, err := canonicalizeFilePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "/private/tmp/") {
		t.Fatalf("macOS /tmp alias was not canonicalized: %s", got)
	}
}

func TestEnsurePrivateDirectoryRejectsBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not authoritative on Windows")
	}
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(dir); err == nil || !strings.Contains(err.Error(), "chmod 700") {
		t.Fatalf("broad directory accepted: %v", err)
	}
}

func TestDarwinFDIFHomeBelowTmpUsesCanonicalDaemonRoot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS platform alias test")
	}
	home := filepath.Join("/tmp", "futurediff-home-test", t.Name())
	t.Setenv("FDIF_HOME", home)
	app := New(Options{NonInteractive: true, NoColor: true})
	if app.InitErr != nil {
		t.Fatal(app.InitErr)
	}
	if !strings.HasPrefix(app.Paths.Home.Path, "/private/tmp/") {
		t.Fatalf("home was not canonicalized: %s", app.Paths.Home.Path)
	}
	if app.Daemon.Root != app.Paths.Home.Path {
		t.Fatalf("daemon root diverged: %s != %s", app.Daemon.Root, app.Paths.Home.Path)
	}
	if app.Paths.WorkspaceRoot.Path != filepath.Join(app.Paths.Home.Path, "runtime") {
		t.Fatalf("workspace root diverged: %+v", app.Paths)
	}
}
