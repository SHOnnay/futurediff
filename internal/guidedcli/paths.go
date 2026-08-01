package guidedcli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	pathSourceHomeFlag    = "--home / --root"
	pathSourceFDIFHome    = "FDIF_HOME"
	pathSourceLegacyRoot  = "FUTUREDIFF_ROOT (legacy)"
	pathSourceDefault     = "default"
	pathSourceStateFlag   = "--state (advanced)"
	pathSourceSocketFlag  = "--socket"
	pathSourceSocketEnv   = "FUTUREDIFF_SOCKET"
	pathSourceDerivedHome = "derived from home"
)

// ResolvedPath records both the canonical path FutureDiff will use and the
// configuration source that selected it.
type ResolvedPath struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

// PathConfig is the single resolved path model shared by the guided CLI,
// daemon launcher, current-transaction selection, and user-facing config
// output.
type PathConfig struct {
	Home          ResolvedPath `json:"home"`
	State         ResolvedPath `json:"current_selection"`
	Socket        ResolvedPath `json:"socket"`
	Runtime       ResolvedPath `json:"runtime"`
	WorkspaceRoot ResolvedPath `json:"workspace_root"`
}

func resolvePathConfig(options Options) (PathConfig, error) {
	homeRaw, homeSource, err := configuredHome(options)
	if err != nil {
		return PathConfig{}, err
	}
	home, err := canonicalizeHomePath(homeRaw)
	if err != nil {
		return PathConfig{}, fmt.Errorf("FutureDiff home: %w", err)
	}

	stateRaw, stateSource := options.StatePath, pathSourceStateFlag
	if strings.TrimSpace(stateRaw) == "" {
		stateRaw = filepath.Join(home, "current-transaction.json")
		stateSource = pathSourceDerivedHome
	}
	state, err := canonicalizeFilePath(stateRaw)
	if err != nil {
		return PathConfig{}, fmt.Errorf("current-selection path: %w", err)
	}

	socketRaw, socketSource := strings.TrimSpace(options.Socket), pathSourceSocketFlag
	if socketRaw == "" {
		socketRaw = strings.TrimSpace(os.Getenv("FUTUREDIFF_SOCKET"))
		socketSource = pathSourceSocketEnv
	}
	if socketRaw == "" {
		socketRaw = filepath.Join(home, "futurediff.sock")
		socketSource = pathSourceDerivedHome
	}
	socket, err := canonicalizeFilePath(socketRaw)
	if err != nil {
		return PathConfig{}, fmt.Errorf("daemon socket path: %w", err)
	}

	runtimeRoot := filepath.Join(home, "runtime")
	return PathConfig{
		Home:          ResolvedPath{Path: home, Source: homeSource},
		State:         ResolvedPath{Path: state, Source: stateSource},
		Socket:        ResolvedPath{Path: socket, Source: socketSource},
		Runtime:       ResolvedPath{Path: runtimeRoot, Source: pathSourceDerivedHome},
		WorkspaceRoot: ResolvedPath{Path: runtimeRoot, Source: pathSourceDerivedHome},
	}, nil
}

func configuredHome(options Options) (string, string, error) {
	if value := strings.TrimSpace(options.Home); value != "" {
		return value, pathSourceHomeFlag, nil
	}
	if value := strings.TrimSpace(os.Getenv("FDIF_HOME")); value != "" {
		return value, pathSourceFDIFHome, nil
	}
	if value := strings.TrimSpace(os.Getenv("FUTUREDIFF_ROOT")); value != "" {
		return value, pathSourceLegacyRoot, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve user home: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", "", errors.New("resolve user home: empty path")
	}
	return filepath.Join(home, ".futurediff"), pathSourceDefault, nil
}

func canonicalizeHomePath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", err
	}
	if info, statErr := os.Lstat(abs); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing home path that is itself a symlink: %s", raw)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	return canonicalizeExistingParents(abs)
}

func canonicalizeFilePath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("path is empty")
	}
	abs, err := filepath.Abs(filepath.Clean(raw))
	if err != nil {
		return "", err
	}
	if info, statErr := os.Lstat(abs); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing path that is itself a symlink: %s", raw)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return "", statErr
	}
	parent, err := canonicalizeExistingParents(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}

// canonicalizeExistingParents resolves trusted operating-system aliases such
// as macOS /tmp -> /private/tmp while rejecting arbitrary user-controlled
// symlink traversal. Non-existing suffixes are appended to the resolved
// longest existing prefix.
func canonicalizeExistingParents(abs string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(abs))
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return canonicalizeLongestExistingPrefix(abs)
	}

	root := string(os.PathSeparator)
	if volume := filepath.VolumeName(abs); volume != "" {
		root = volume + string(os.PathSeparator)
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	current := root
	if rel != "." {
		for _, component := range strings.Split(rel, string(os.PathSeparator)) {
			if component == "" || component == "." {
				continue
			}
			current = filepath.Join(current, component)
			info, statErr := os.Lstat(current)
			if os.IsNotExist(statErr) {
				break
			}
			if statErr != nil {
				return "", statErr
			}
			if info.Mode()&os.ModeSymlink != 0 && !trustedPlatformAlias(current) {
				return "", fmt.Errorf("refusing path through symlinked directory: %s", current)
			}
		}
	}
	return canonicalizeLongestExistingPrefix(abs)
}

func canonicalizeLongestExistingPrefix(abs string) (string, error) {
	existing := abs
	var suffix []string
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", fmt.Errorf("no existing parent for %s", abs)
		}
		suffix = append(suffix, filepath.Base(existing))
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return filepath.Clean(resolved), nil
}

func trustedPlatformAlias(path string) bool {
	path = filepath.Clean(path)
	switch runtime.GOOS {
	case "darwin":
		return path == "/tmp" || path == "/var" || path == "/etc"
	case "linux":
		return path == "/var/run"
	default:
		return false
	}
}

func validatePrivateDirectory(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("private directory path is empty")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("private path must be a real directory, not a symlink: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("private directory permissions are too broad: %s has %o; use chmod 700", path, info.Mode().Perm())
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("private directory path is empty")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return validatePrivateDirectory(path)
}
