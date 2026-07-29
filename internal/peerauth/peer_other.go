//go:build !linux && !darwin

package peerauth

import (
	"errors"
	"net"
)

var errPeerAuthUnsupported = errors.New("kernel peer credentials are not implemented on this platform")

// CheckSupport reports whether secure kernel-authenticated peer identity is available.
func CheckSupport() error { return errPeerAuthUnsupported }

func FromConn(net.Conn) (Identity, error) {
	return Identity{}, errPeerAuthUnsupported
}
