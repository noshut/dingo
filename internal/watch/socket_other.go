//go:build !linux && !darwin

package watch

import (
	"errors"
	"net"
)

func listenICMPDatagram() (net.PacketConn, error) {
	return nil, errors.New("icmp datagram sockets are not supported on this platform")
}
