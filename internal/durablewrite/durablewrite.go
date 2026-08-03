// Package durablewrite provides the deterministic durable-write
// fault-injection foundation for the corruption, stale-lock, and disk-pressure
// hardening milestone (ADR-099).
//
// The package is a test seam, not a production configuration surface: an
// Injector is supplied by dependency injection, never by environment
// variables, CLI flags, or configuration files. Production callers pass nil
// and ReplaceFile behaves exactly like a plain atomic durable write.
//
// ReplaceFile performs the six durable-write boundaries in order: create,
// write (including short_write), file_sync, rename, and directory_sync. Each
// boundary consults the injector when one is present. A failure at any
// boundary before rename leaves the previous authoritative file untouched and
// removes the temporary file; a directory-sync failure after rename reports
// the fault without a false success.
package durablewrite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Operation names consulted on Injector.Before, in execution order. They match
// the durable-write boundaries named in ADR-099.
const (
	OpCreate        = "create"
	OpWrite         = "write"
	OpShortWrite    = "short_write"
	OpFileSync      = "file_sync"
	OpRename        = "rename"
	OpDirectorySync = "directory_sync"
)

// Classified sentinel errors. Tests inject these (or errors wrapping them) so
// callers can classify the underlying failure deterministically. Real write
// paths should wrap the equivalent errno (ENOSPC, EDQUOT, EROFS, EIO) with one
// of these sentinels so Classify keeps working.
var (
	ErrDiskFull           = errors.New("durablewrite: disk full")
	ErrQuotaExceeded      = errors.New("durablewrite: quota exceeded")
	ErrReadOnlyFilesystem = errors.New("durablewrite: read-only filesystem")
	ErrIO                 = errors.New("durablewrite: input/output error")
)

// Injector is the test-only fault-injection seam. Before is called once per
// durable-write boundary; returning a non-nil error fails that boundary with
// the returned error. A nil Injector disables injection entirely.
type Injector interface {
	Before(operation string) error
}

// FaultError wraps a failure at a specific durable-write boundary, retaining
// the operation, the destination path, and the underlying error for
// diagnostics. Code returns a stable ADR-099 fault-type code.
type FaultError struct {
	Op   string
	Path string
	Err  error
}

func (e *FaultError) Error() string {
	return fmt.Sprintf("durablewrite: %s %s: %v", e.Op, e.Path, e.Err)
}

// Unwrap exposes the underlying error so errors.Is and errors.As work.
func (e *FaultError) Unwrap() error { return e.Err }

// Code returns the stable ADR-099 fault-type code for the boundary.
func (e *FaultError) Code() string {
	switch e.Op {
	case OpCreate:
		return "create_failure"
	case OpWrite:
		return "write_failure"
	case OpShortWrite:
		return "short_write"
	case OpFileSync:
		return "sync_failure"
	case OpRename:
		return "rename_failure"
	case OpDirectorySync:
		return "dir_sync_failure"
	}
	return "durable_write_failed"
}

// Classify maps an error to a stable ADR-099 reason code. It walks the error
// chain via errors.Is, so wrapped sentinels and real errno wrappers both work.
func Classify(err error) string {
	switch {
	case errors.Is(err, ErrDiskFull):
		return "disk_full"
	case errors.Is(err, ErrQuotaExceeded):
		return "quota_exceeded"
	case errors.Is(err, ErrReadOnlyFilesystem):
		return "filesystem_read_only"
	}
	return "durable_write_failed"
}

func wrapFault(op, path string, err error) error {
	return &FaultError{Op: op, Path: path, Err: err}
}

// ReplaceFile atomically and durably replaces dest with data.
//
// Steps, each of which may fail via inject:
//  1. create  — create a temporary file in dest's directory
//  2. write   — write the full payload (a short_write fault writes a prefix
//     and then fails, simulating a partially written temporary file)
//  3. file_sync — fsync the temporary file
//  4. rename  — atomically move the temporary file over dest
//  5. directory_sync — fsync dest's parent directory so the rename is durable
//
// Until rename succeeds, the previous authoritative file at dest is never
// touched, and on any failure the temporary file is removed. After rename the
// new content is authoritative (the file was fsynced); a directory-sync
// failure is still reported so the caller knows the entry may not survive a
// crash. Concurrent calls are safe: each uses its own temporary file and the
// final rename is atomic.
func ReplaceFile(dest string, data []byte, perm os.FileMode, inject Injector) error {
	if dest == "" {
		return errors.New("durablewrite: destination path required")
	}
	dir := filepath.Dir(dest)
	if inject != nil {
		if err := inject.Before(OpCreate); err != nil {
			return wrapFault(OpCreate, dest, err)
		}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(dest)+".tmp-*")
	if err != nil {
		return wrapFault(OpCreate, dest, err)
	}
	tmpName := tmp.Name()
	// Remove the temporary file unless rename already moved it (in which case
	// the name no longer exists and the removal is a no-op).
	defer func() { _ = os.Remove(tmpName) }()

	if inject != nil {
		if err := inject.Before(OpWrite); err != nil {
			_ = tmp.Close()
			return wrapFault(OpWrite, dest, err)
		}
	}
	if inject != nil {
		if err := inject.Before(OpShortWrite); err != nil {
			// Simulate a partial write, then fail: the temporary file is
			// incomplete and must never become authoritative.
			if half := len(data) / 2; half > 0 {
				_, _ = tmp.Write(data[:half])
			}
			_ = tmp.Close()
			return wrapFault(OpShortWrite, dest, err)
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return wrapFault(OpWrite, dest, err)
	}
	if inject != nil {
		if err := inject.Before(OpFileSync); err != nil {
			_ = tmp.Close()
			return wrapFault(OpFileSync, dest, err)
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return wrapFault(OpFileSync, dest, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return wrapFault(OpFileSync, dest, err)
	}
	if err := tmp.Close(); err != nil {
		return wrapFault(OpFileSync, dest, err)
	}
	if inject != nil {
		if err := inject.Before(OpRename); err != nil {
			return wrapFault(OpRename, dest, err)
		}
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return wrapFault(OpRename, dest, err)
	}
	if inject != nil {
		if err := inject.Before(OpDirectorySync); err != nil {
			return wrapFault(OpDirectorySync, dest, err)
		}
	}
	if err := syncDir(dir); err != nil {
		return wrapFault(OpDirectorySync, dest, err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
