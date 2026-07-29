//go:build linux || (darwin && cgo)

package peerauth

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFromConnUnixPeerIdentity(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "peer.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatalf("listen on Unix socket: %v", err)
	}
	defer listener.Close()

	clientReady := make(chan error, 1)
	releaseClient := make(chan struct{})
	go func() {
		client, dialErr := net.DialUnix("unix", nil, &net.UnixAddr{Name: socketPath, Net: "unix"})
		clientReady <- dialErr
		if dialErr != nil {
			return
		}
		defer client.Close()
		<-releaseClient
	}()

	serverConn, err := listener.AcceptUnix()
	if err != nil {
		close(releaseClient)
		t.Fatalf("accept Unix connection: %v", err)
	}
	defer serverConn.Close()
	if dialErr := <-clientReady; dialErr != nil {
		close(releaseClient)
		t.Fatalf("dial Unix socket: %v", dialErr)
	}
	defer close(releaseClient)

	identity, err := FromConn(serverConn)
	if err != nil {
		t.Fatalf("read peer identity: %v", err)
	}
	if !identity.Available {
		t.Fatal("peer identity should be available")
	}
	if identity.UID != uint32(os.Geteuid()) {
		t.Fatalf("peer UID = %d, want %d", identity.UID, os.Geteuid())
	}
	if identity.GID != uint32(os.Getegid()) {
		t.Fatalf("peer GID = %d, want %d", identity.GID, os.Getegid())
	}

	switch runtime.GOOS {
	case "linux":
		if identity.PID <= 0 {
			t.Fatalf("Linux peer PID = %d, want a positive PID", identity.PID)
		}
	case "darwin":
		if identity.PID != -1 {
			t.Fatalf("macOS peer PID = %d, want -1 because getpeereid does not expose PID", identity.PID)
		}
	}
}

func TestFromConnRejectsNonUnixConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	_, err := FromConn(server)
	if err == nil {
		t.Fatal("expected non-Unix connection to be rejected")
	}
	if !strings.Contains(err.Error(), "Unix-domain") {
		t.Fatalf("unexpected error: %v", err)
	}
}
