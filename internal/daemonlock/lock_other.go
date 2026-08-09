//go:build !linux && !darwin

package daemonlock

import (
	"errors"
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
type Lock struct{}

func Acquire(path, root string, now time.Time) (*Lock, error) {
	return nil, errors.New("daemon file locks are unsupported on this platform")
}
func (l *Lock) Metadata() Metadata { return Metadata{} }
func (l *Lock) Release() error     { return nil }
func Inspect(path string, now time.Time) (Status, error) {
	return Status{Path: path, CheckedAt: now.UTC()}, errors.New("daemon file locks are unsupported on this platform")
}

// ErrLockHeld is returned by RemoveIfUnheld when a process holds the lock.
var ErrLockHeld = errors.New("daemon lock is currently held")

func RemoveIfUnheld(path string) error {
	return errors.New("daemon file locks are unsupported on this platform")
}
