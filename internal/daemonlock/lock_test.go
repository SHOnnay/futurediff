//go:build linux || darwin

package daemonlock

import (
	"path/filepath"
	"testing"
	"time"
)

func TestExclusiveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	first, err := Acquire(path, "/tmp/root", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	if _, err := Acquire(path, "/tmp/root", time.Now()); err == nil {
		t.Fatal("expected second acquisition to fail")
	}
	status, err := Inspect(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Held || status.Metadata.PID == 0 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	status, err = Inspect(path, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if status.Held {
		t.Fatal("lock should be released")
	}
}
