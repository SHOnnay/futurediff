//go:build linux

package peerauth

import (
	"errors"
	"net"
	"syscall"
)

func FromConn(conn net.Conn) (Identity, error) {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return Identity{}, errors.New("peer credentials require a Unix-domain connection")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return Identity{}, err
	}
	var cred *syscall.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		cred, controlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return Identity{}, err
	}
	if controlErr != nil {
		return Identity{}, controlErr
	}
	if cred == nil {
		return Identity{}, errors.New("peer credentials unavailable")
	}
	return Identity{UID: cred.Uid, GID: cred.Gid, PID: cred.Pid, Available: true}, nil
}
