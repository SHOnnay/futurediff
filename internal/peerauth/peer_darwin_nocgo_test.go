//go:build darwin && !cgo

package peerauth

import (
	"net"
	"strings"
	"testing"
)

func TestCheckSupportRequiresCGOOnDarwin(t *testing.T) {
	err := CheckSupport()
	if err == nil {
		t.Fatal("expected a CGO requirement error")
	}
	if !strings.Contains(err.Error(), "CGO-enabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFromConnRequiresCGOOnDarwin(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	_, err := FromConn(server)
	if err == nil {
		t.Fatal("expected a CGO requirement error")
	}
	if !strings.Contains(err.Error(), "CGO-enabled") {
		t.Fatalf("unexpected error: %v", err)
	}
}
