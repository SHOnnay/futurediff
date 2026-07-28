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
	if root := os.Getenv("FUTUREDIFF_ROOT"); root != "" {
		return filepath.Join(root, "current-transaction.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".futurediff", "current-transaction.json")
}

func (s StateStore) Load() (CurrentTransaction, error) {
	if s.Path == "" {
		return CurrentTransaction{}, errors.New("current transaction state path is empty")
	}
	if err := rejectSymlinkedParent(filepath.Dir(s.Path)); err != nil {
		return CurrentTransaction{}, err
	}
	info, err := os.Lstat(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return CurrentTransaction{}, os.ErrNotExist
		}
		return CurrentTransaction{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return CurrentTransaction{}, fmt.Errorf("refusing symlink state file %s", s.Path)
	}
	if !info.Mode().IsRegular() {
		return CurrentTransaction{}, fmt.Errorf("state path is not a regular file: %s", s.Path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return CurrentTransaction{}, fmt.Errorf("state file permissions are too broad: %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(s.Path)
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
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := rejectSymlinkedParent(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(s.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to replace symlink state file %s", s.Path)
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
	if err := os.Rename(tmpName, s.Path); err != nil {
		return err
	}
	return os.Chmod(s.Path, 0o600)
}

func (s StateStore) Clear() error {
	err := os.Remove(s.Path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func rejectSymlinkedParent(dir string) error {
	clean, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return err
	}
	if resolved != clean {
		return fmt.Errorf("refusing state path through symlinked directory: %s", dir)
	}
	return nil
}
