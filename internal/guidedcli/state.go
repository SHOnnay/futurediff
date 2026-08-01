package guidedcli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

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
	if info.Mode()&os.ModeSymlink != 0 {
		return CurrentTransaction{}, fmt.Errorf("refusing symlink state file %s", path)
	}
	if !info.Mode().IsRegular() {
		return CurrentTransaction{}, fmt.Errorf("state path is not a regular file: %s", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return CurrentTransaction{}, fmt.Errorf("state file permissions are too broad: %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CurrentTransaction{}, err
	}
	var current CurrentTransaction
	if err := json.Unmarshal(data, &current); err != nil {
		return CurrentTransaction{}, fmt.Errorf("decode current transaction state: %w", err)
	}
	if current.TransactionID == "" {
		return CurrentTransaction{}, errors.New("current transaction state has no transaction_id")
	}
	return current, nil
}

func (s StateStore) Save(transactionID, repositoryRoot string) error {
	if transactionID == "" {
		return errors.New("transaction ID is required")
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
	return os.Chmod(path, 0o600)
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
