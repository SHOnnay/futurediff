package api

import (
	"os"
	"path/filepath"
	"testing"
)

func shortSocketPath(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", prefix)
	if err != nil {
		t.Fatalf("create short socket dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "fd.sock")
}
