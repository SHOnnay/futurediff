//go:build linux || darwin

package daemonlock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
)

const Version = "0.1"
const maxLockFileBytes = 64 << 10

type Metadata struct {
	Version       string    `json:"version"`
	PID           int       `json:"pid"`
	UID           int       `json:"uid"`
	StartedAt     time.Time `json:"started_at"`
	Root          string    `json:"root"`
	Hostname      string    `json:"hostname,omitempty"`
	StartedAtNs   int64     `json:"started_at_ns,omitempty"`
	BootID        string    `json:"boot_id,omitempty"`
	DaemonVersion string    `json:"daemon_version,omitempty"`
}

type Status struct {
	Path                    string    `json:"path"`
	Held                    bool      `json:"held"`
	Metadata                Metadata  `json:"metadata,omitempty"`
	CheckedAt               time.Time `json:"checked_at"`
	OwnerStatus             string    `json:"owner_status,omitempty"` // alive, dead, ambiguous, proved_stale
	LockStatus              string    `json:"lock_status,omitempty"`  // held, released, contested, unavailable
	AutomaticCleanupAllowed bool      `json:"automatic_cleanup_allowed,omitempty"`
	ReasonCode              string    `json:"reason_code,omitempty"`
	DaemonReachable         bool      `json:"daemon_reachable,omitempty"`
	DaemonSocketPath        string    `json:"daemon_socket_path,omitempty"`
	RecommendedAction       string    `json:"recommended_action,omitempty"`
	SafeToRetry             bool      `json:"safe_to_retry,omitempty"`
}

type Lock struct {
	path string
	file *os.File
	meta Metadata
}

func getHostname() string {
	hn, _ := os.Hostname()
	return hn
}

func getBootID() string {
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
			return string(bytes.TrimSpace([]byte(data)))
		}
	}
	if runtime.GOOS == "darwin" {
		// macOS: use sysctl kern.boottime
		// For simplicity, we'll use a stable identifier
		if data, err := os.ReadFile("/var/run/utmpx"); err == nil && len(data) > 0 {
			return "darwin-boot-" + fmt.Sprintf("%x", data[:8])
		}
	}
	return ""
}

func getProcessStartTimeNs(pid int) int64 {
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
			fields := bytes.Fields(data)
			if len(fields) > 21 {
				// starttime is field 22 (1-indexed)
				var starttime int64
				fmt.Sscanf(string(fields[21]), "%d", &starttime)
				// Convert from clock ticks to nanoseconds
				clkTck := 100 // sysconf(_SC_CLK_TCK) typically 100
				return int64(starttime) * (1_000_000_000 / int64(clkTck))
			}
		}
		return 0
	}
	if runtime.GOOS == "darwin" {
		return darwinProcessStartTimeNs(pid)
	}
	return 0
}

// darwin sysctl identifiers (from <sys/sysctl.h>).
const (
	darwinCTLKern      = 1
	darwinKernProc     = 14
	darwinKernProcPID  = 1
	darwinSysctlCallNo = 202 // SYS_sysctl on macOS (amd64 and arm64)
)

// darwinProcessStartTimeNs returns the wall-clock process start time in
// nanoseconds for pid via sysctl(KERN_PROC_PID). The kinfo_proc structure
// begins with struct extern_proc, whose first member is the p_un union whose
// p_starttime is a struct timeval {tv_sec int64; tv_usec int32}. Returns 0
// when the identity cannot be read (callers must fail closed to ambiguous).
func darwinProcessStartTimeNs(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	mib := [4]int32{darwinCTLKern, darwinKernProc, darwinKernProcPID, int32(pid)}
	buf := make([]byte, 4096)
	length := uintptr(len(buf))
	_, _, errno := syscall.Syscall6(uintptr(darwinSysctlCallNo),
		uintptr(unsafe.Pointer(&mib[0])),
		uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&length)),
		0, 0)
	if errno != 0 {
		return 0
	}
	if length < 16 {
		return 0
	}
	tv := *(*struct {
		Sec  int64
		Usec int32
	})(unsafe.Pointer(&buf[0]))
	if tv.Sec <= 0 {
		return 0
	}
	return tv.Sec*1e9 + int64(tv.Usec)*1e3
}

func isProcessAlive(pid int, startedAtNs int64, bootID string) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	// A boot-id mismatch means the lock predates a reboot; the recorded
	// process cannot be alive as the same identity.
	if bootID != "" && getBootID() != "" && bootID != getBootID() {
		return false, nil
	}
	if runtime.GOOS == "linux" {
		// Check if process exists and start time matches
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
			startNs := getProcessStartTimeNs(pid)
			if startedAtNs > 0 && startNs > 0 && startNs != startedAtNs {
				return false, nil // PID reused
			}
			return true, nil
		}
		return false, nil
	}
	if runtime.GOOS == "darwin" {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return false, nil
		}
		if err != nil && !errors.Is(err, syscall.EPERM) {
			return false, err
		}
		// Process exists; verify start time to defeat PID reuse.
		if startedAtNs > 0 {
			startNs := getProcessStartTimeNs(pid)
			if startNs > 0 && startNs != startedAtNs {
				return false, nil // PID reused
			}
		}
		return true, nil
	}
	return false, nil
}

func getDaemonVersion() string {
	v := buildinfo.Current()
	return v.Version
}

func Acquire(path, root string, now time.Time) (*Lock, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("daemon lock path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("daemon lock path must not be a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("another FutureDiff daemon holds %s", path)
		}
		return nil, fmt.Errorf("acquire daemon lock: %w", err)
	}
	hostname := getHostname()
	bootID := getBootID()
	meta := Metadata{
		Version:       Version,
		PID:           os.Getpid(),
		UID:           os.Geteuid(),
		StartedAt:     now.UTC(),
		Root:          root,
		Hostname:      hostname,
		StartedAtNs:   getProcessStartTimeNs(os.Getpid()),
		BootID:        bootID,
		DaemonVersion: getDaemonVersion(),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	if err := f.Truncate(0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	if err := f.Sync(); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, err
	}
	return &Lock{path: path, file: f, meta: meta}, nil
}

func (l *Lock) Metadata() Metadata { return l.meta }

func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

// ErrLockHeld is returned by RemoveIfUnheld when another process currently
// holds the exclusive flock on the lock file.
var ErrLockHeld = errors.New("daemon lock is currently held")

// RemoveIfUnheld removes path only when no process holds an exclusive flock on
// it. It acquires the flock itself, verifies the path still refers to the same
// inode it locked (so a concurrently re-created lock file is never removed),
// then unlinks while still holding the flock. A daemon that starts between the
// check and the unlink cannot acquire the lock, because its flock attempt fails
// while ours is held. The two removal steps in a cleanup (lock file and socket)
// are intentionally separate; this helper makes each individual removal safe.
func RemoveIfUnheld(path string) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("daemon lock path must be absolute")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // already gone; idempotent
		}
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return ErrLockHeld
		}
		return fmt.Errorf("flock lock file: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	pathInfo, pathErr := os.Lstat(path)
	fdInfo, fdErr := f.Stat()
	if pathErr != nil {
		if os.IsNotExist(pathErr) {
			return nil // already removed; idempotent
		}
		return pathErr
	}
	if fdErr != nil {
		return fdErr
	}
	if !os.SameFile(pathInfo, fdInfo) {
		return errors.New("lock file was replaced during cleanup; refusing to remove")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove lock file: %w", err)
	}
	return nil
}

func readLockFileBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxLockFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxLockFileBytes {
		return nil, fmt.Errorf("lock file exceeds maximum size of %d bytes", maxLockFileBytes)
	}
	return data, nil
}

func Inspect(path string, now time.Time) (Status, error) {
	status := Status{
		Path:        path,
		CheckedAt:   now.UTC(),
		LockStatus:  "unavailable",
		OwnerStatus: "unavailable",
		SafeToRetry: false,
	}
	if path == "" || !filepath.IsAbs(path) {
		status.ReasonCode = "invalid_lock_path"
		status.RecommendedAction = "use absolute path for lock file"
		return status, errors.New("daemon lock path must be absolute")
	}
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		status.LockStatus = "released"
		status.OwnerStatus = "dead"
		status.ReasonCode = "no_lock"
		status.RecommendedAction = "start daemon to acquire lock"
		status.SafeToRetry = true
		return status, nil
	}
	if err != nil {
		status.ReasonCode = "lock_stat_failed"
		status.RecommendedAction = "check filesystem and permissions"
		return status, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		status.ReasonCode = "lock_not_regular_file"
		status.RecommendedAction = "remove symlink or non-regular file manually; then start daemon"
		status.AutomaticCleanupAllowed = false
		return status, errors.New("daemon lock path is not a regular file")
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0 {
		status.ReasonCode = "lock_permissions_too_broad"
		status.RecommendedAction = "chmod 0600 lock file; then start daemon"
		status.AutomaticCleanupAllowed = false
		return status, fmt.Errorf("lock file permissions are too broad: %o", st.Mode().Perm())
	}
	if st.Size() > maxLockFileBytes {
		status.ReasonCode = "lock_file_too_large"
		status.RecommendedAction = "remove oversized lock file manually; then start daemon"
		status.AutomaticCleanupAllowed = false
		return status, fmt.Errorf("lock file exceeds maximum size of %d bytes", maxLockFileBytes)
	}
	data, err := readLockFileBounded(path)
	if err != nil {
		status.ReasonCode = "lock_read_failed"
		status.RecommendedAction = "check filesystem and permissions"
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&status.Metadata); err != nil {
		status.ReasonCode = "lock_invalid_json"
		status.RecommendedAction = "remove corrupt lock file manually; then start daemon"
		status.AutomaticCleanupAllowed = true // corrupt lock can be cleaned
		return status, fmt.Errorf("decode lock metadata: %w", err)
	}
	if decoder.More() {
		status.ReasonCode = "lock_trailing_data"
		status.RecommendedAction = "remove corrupt lock file manually; then start daemon"
		status.AutomaticCleanupAllowed = true
		return status, errors.New("lock file contains trailing data")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		status.ReasonCode = "lock_open_failed"
		status.RecommendedAction = "check filesystem and permissions"
		return status, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		status.Held = false
		status.LockStatus = "released"
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	} else if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		status.Held = true
		status.LockStatus = "held"
	} else {
		status.ReasonCode = "lock_flock_failed"
		status.RecommendedAction = "check filesystem and permissions"
		return status, err
	}
	if !status.Held {
		status.LockStatus = "released"
		status.OwnerStatus = "dead"
		status.ReasonCode = "stale_lock_candidate"
		status.RecommendedAction = "run fdif cleanup-lock --yes to remove stale lock"
		status.AutomaticCleanupAllowed = true
		status.SafeToRetry = true
		return status, nil
	}
	status.LockStatus = "held"
	// Lock is held; verify owner identity
	pid := status.Metadata.PID
	startedAtNs := status.Metadata.StartedAtNs
	alive, err := isProcessAlive(pid, startedAtNs, status.Metadata.BootID)
	if err != nil {
		status.OwnerStatus = "ambiguous"
		status.ReasonCode = "lock_owner_ambiguous"
		status.RecommendedAction = "cannot verify owner; manual inspection required"
		status.AutomaticCleanupAllowed = false
		return status, nil
	}
	if !alive {
		if startedAtNs > 0 || status.Metadata.BootID != "" {
			status.OwnerStatus = "proved_stale"
			status.ReasonCode = "stale_lock_candidate"
			status.RecommendedAction = "run fdif cleanup-lock --yes to remove proved-stale lock"
			status.AutomaticCleanupAllowed = true
			status.SafeToRetry = true
			return status, nil
		}
		status.OwnerStatus = "dead"
		status.ReasonCode = "stale_lock_candidate"
		status.RecommendedAction = "run fdif cleanup-lock --yes to remove stale lock"
		status.AutomaticCleanupAllowed = true
		status.SafeToRetry = true
		return status, nil
	}
	// Process alive — check if it's our daemon by trying to reach it
	// We need the socket path. Derive from lock file path.
	lockDir := filepath.Dir(path)
	socketPath := filepath.Join(lockDir, "futurediff.sock")
	status.DaemonSocketPath = socketPath
	reachable := false
	if conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond); err == nil {
		_ = conn.Close()
		reachable = true
	}
	status.DaemonReachable = reachable
	if reachable {
		status.OwnerStatus = "alive"
		status.ReasonCode = "lock_owner_alive"
		status.RecommendedAction = "daemon is running; do not remove lock"
		status.AutomaticCleanupAllowed = false
		status.SafeToRetry = false
		return status, nil
	}
	// Process alive but daemon not reachable — ambiguous
	status.OwnerStatus = "ambiguous"
	status.ReasonCode = "lock_owner_ambiguous"
	status.RecommendedAction = "process alive but daemon unreachable; manual inspection required"
	status.AutomaticCleanupAllowed = false
	status.SafeToRetry = false
	return status, nil
}
