//go:build darwin && !cgo

package peerauth

import (
	"errors"
	"net"
)

var errDarwinPeerAuthRequiresCGO = errors.New("secure macOS peer credentials require a CGO-enabled build")

// CheckSupport reports whether secure kernel-authenticated peer identity is available.
func CheckSupport() error { return errDarwinPeerAuthRequiresCGO }

func FromConn(net.Conn) (Identity, error) {
	return Identity{}, errDarwinPeerAuthRequiresCGO
}
