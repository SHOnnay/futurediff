//go:build !linux

package peerauth

import (
	"errors"
	"net"
)

func FromConn(net.Conn) (Identity, error) {
	return Identity{}, errors.New("kernel peer credentials are not implemented on this platform")
}
