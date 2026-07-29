//go:build darwin && cgo

package peerauth

/*
#include <errno.h>
#include <sys/types.h>
#include <unistd.h>

static int futurediff_getpeereid(int fd, uid_t *uid, gid_t *gid, int *errnum) {
	if (getpeereid(fd, uid, gid) == -1) {
		*errnum = errno;
		return -1;
	}
	*errnum = 0;
	return 0;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"net"
	"syscall"
)

// CheckSupport reports whether secure kernel-authenticated peer identity is available.
func CheckSupport() error { return nil }

// FromConn returns the effective user and group IDs of the peer connected to a
// macOS Unix-domain stream socket. macOS getpeereid does not expose a peer PID,
// so PID is set to -1 to explicitly represent that the value is unavailable.
func FromConn(conn net.Conn) (Identity, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return Identity{}, errors.New("peer credentials require a Unix-domain connection")
	}

	raw, err := unixConn.SyscallConn()
	if err != nil {
		return Identity{}, fmt.Errorf("access peer socket: %w", err)
	}

	var uid C.uid_t
	var gid C.gid_t
	var errnoValue C.int
	var credentialErr error

	if err := raw.Control(func(fd uintptr) {
		if C.futurediff_getpeereid(C.int(fd), &uid, &gid, &errnoValue) != 0 {
			if errnoValue == 0 {
				credentialErr = errors.New("getpeereid failed without an errno value")
				return
			}
			credentialErr = syscall.Errno(uintptr(errnoValue))
		}
	}); err != nil {
		return Identity{}, fmt.Errorf("inspect peer socket: %w", err)
	}
	if credentialErr != nil {
		return Identity{}, fmt.Errorf("getpeereid: %w", credentialErr)
	}

	return Identity{
		UID:       uint32(uid),
		GID:       uint32(gid),
		PID:       -1,
		Available: true,
	}, nil
}
