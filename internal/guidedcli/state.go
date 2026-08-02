package guidedcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// maxStateFileBytes bounds the current-transaction selection file. The file is
// written only by Save and is a few hundred bytes; anything near this cap is
// either corrupt or tampered with.
const maxStateFileBytes = 64 << 10

type StateStore struct {
	Path string
}

func DefaultStatePath() string {
	paths, err := resolvePathConfig(Options{})
	if err == nil {
		return paths.State.Path
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".futurediff", "current-transaction.json")
}

func (s StateStore) effectivePath() (string, error) {
	if s.Path == "" {
		return "", errors.New("current transaction state path is empty")
	}
	return canonicalizeFilePath(s.Path)
}

// openNoFollow opens the selection file without following a final symlink.
// Between the Lstat validation and the actual read a concurrent process could
// otherwise swap the validated file for a symlink to an attacker-controlled
// path; opening with O_NOFOLLOW and validating the opened descriptor closes
// that race on POSIX platforms.
func openNoFollow(path string) (*os.File, error) {
	if runtime.GOOS == "windows" {
		return os.Open(path)
	}
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}

func (s StateStore) Load() (CurrentTransaction, error) {
	path, err := s.effectivePath()
	if err != nil {
		return CurrentTransaction{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CurrentTransaction{}, os.ErrNotExist
		}
		return CurrentTransaction{}, err
	}
	if err := validateStateFileInfo(info, path); err != nil {
		return CurrentTransaction{}, err
	}
	file, err := openNoFollow(path)
	if err != nil {
		if os.IsNotExist(err) {
			return CurrentTransaction{}, os.ErrNotExist
		}
		return CurrentTransaction{}, err
	}
	defer file.Close()
	// Validate the descriptor we actually read from, not the path we
	// validated earlier (TOCTOU).
	opened, err := file.Stat()
	if err != nil {
		return CurrentTransaction{}, err
	}
	if err := validateStateFileInfo(opened, path); err != nil {
		return CurrentTransaction{}, err
	}
	if opened.Size() > maxStateFileBytes {
		return CurrentTransaction{}, fmt.Errorf("state file exceeds the maximum size of %d bytes", maxStateFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxStateFileBytes+1))
	if err != nil {
		return CurrentTransaction{}, err
	}
	if len(data) > maxStateFileBytes {
		return CurrentTransaction{}, fmt.Errorf("state file exceeds the maximum size of %d bytes", maxStateFileBytes)
	}
	current, err := decodeCurrentTransaction(data)
	if err != nil {
		return CurrentTransaction{}, err
	}
	if err := validateCurrentTransaction(current); err != nil {
		return CurrentTransaction{}, err
	}
	return current, nil
}

func validateStateFileInfo(info os.FileInfo, path string) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink state file %s", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("state path is not a regular file: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("state file permissions are too broad: %o", info.Mode().Perm())
	}
	return nil
}

// decodeCurrentTransaction strictly decodes the selection file: unknown
// fields and trailing JSON are rejected rather than silently ignored.
func decodeCurrentTransaction(data []byte) (CurrentTransaction, error) {
	var current CurrentTransaction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&current); err != nil {
		return CurrentTransaction{}, fmt.Errorf("decode current transaction state: %w", err)
	}
	if decoder.More() {
		return CurrentTransaction{}, errors.New("current transaction state contains trailing data")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return CurrentTransaction{}, errors.New("current transaction state contains trailing data")
	}
	return current, nil
}

// validateCurrentTransaction enforces the selection file contract: a
// transaction ID with the ledger shape, an absolute repository root when one
// is recorded, and a plausible selection timestamp.
func validateCurrentTransaction(current CurrentTransaction) error {
	if current.TransactionID == "" {
		return errors.New("current transaction state has no transaction_id")
	}
	if !validTransactionID(current.TransactionID) {
		return fmt.Errorf("current transaction state has an invalid transaction_id %q", current.TransactionID)
	}
	if current.RepositoryRoot != "" && !filepath.IsAbs(current.RepositoryRoot) {
		return fmt.Errorf("current transaction state repository_root is not an absolute path: %q", current.RepositoryRoot)
	}
	if current.SelectedAt.IsZero() {
		return errors.New("current transaction state has no selected_at timestamp")
	}
	if current.SelectedAt.After(time.Now().Add(24 * time.Hour)) {
		return fmt.Errorf("current transaction state selected_at is in the future: %s", current.SelectedAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// validTransactionID matches the ledger identifier shape (tx_ followed by a
// non-empty run of letters, digits, underscores, or hyphens).
func validTransactionID(value string) bool {
	if !strings.HasPrefix(value, "tx_") || len(value) <= len("tx_") {
		return false
	}
	for _, r := range value[len("tx_"):] {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func (s StateStore) Save(transactionID, repositoryRoot string) error {
	if transactionID == "" {
		return errors.New("transaction ID is required")
	}
	if !validTransactionID(transactionID) {
		return fmt.Errorf("invalid transaction ID %q", transactionID)
	}
	if repositoryRoot != "" && !filepath.IsAbs(repositoryRoot) {
		return fmt.Errorf("repository root is not an absolute path: %q", repositoryRoot)
	}
	path, err := s.effectivePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("state directory must be a real directory: %s", dir)
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink state file %s", path)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	current := CurrentTransaction{TransactionID: transactionID, RepositoryRoot: repositoryRoot, SelectedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".current-transaction-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}

// syncDirectory fsyncs the parent directory so the rename itself is durable,
// not just the file content. Best-effort on platforms without directory
// fsync support.
func syncDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	handle, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return nil
	}
	return nil
}

func (s StateStore) Clear() error {
	path, err := s.effectivePath()
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink state file %s", path)
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
