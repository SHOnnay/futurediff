//go:build linux || (darwin && cgo)

package peerauth

import "testing"

func TestCheckSupportAvailable(t *testing.T) {
	if err := CheckSupport(); err != nil {
		t.Fatalf("peer authentication should be supported: %v", err)
	}
}
