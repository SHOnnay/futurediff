//go:build linux || darwin

package daemonlock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const Version = "0.1"

type Metadata struct {
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	UID       int       `json:"uid"`
	StartedAt time.Time `json:"started_at"`
	Root      string    `json:"root"`
}

type Status struct {
	Path      string    `json:"path"`
	Held      bool      `json:"held"`
	Metadata  Metadata  `json:"metadata,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type Lock struct {
	path string
	file *os.File
	meta Metadata
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
	meta := Metadata{Version: Version, PID: os.Getpid(), UID: os.Geteuid(), StartedAt: now.UTC(), Root: root}
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

func Inspect(path string, now time.Time) (Status, error) {
	status := Status{Path: path, CheckedAt: now.UTC()}
	if path == "" || !filepath.IsAbs(path) {
		return status, errors.New("daemon lock path must be absolute")
	}
	st, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return status, errors.New("daemon lock path is not a regular file")
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return status, err
	}
	defer f.Close()
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &status.Metadata)
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		status.Held = false
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return status, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		status.Held = true
		return status, nil
	}
	return status, err
}
