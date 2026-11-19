//go:build linux || darwin

package watch

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// listenICMPDatagram opens an unprivileged ICMP "ping socket"
// (socket(AF_INET, SOCK_DGRAM, IPPROTO_ICMP)). On Linux this requires the
// process gid to be within net.ipv4.ping_group_range. The kernel manages the
// echo identifier and delivers replies without the IP header.
func listenICMPDatagram() (net.PacketConn, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_ICMP)
	if err != nil {
		return nil, fmt.Errorf("icmp datagram socket: %w", err)
	}
	f := os.NewFile(uintptr(fd), "icmp")
	defer func() { _ = f.Close() }() // FilePacketConn dups the fd
	c, err := net.FilePacketConn(f)
	if err != nil {
		return nil, fmt.Errorf("icmp datagram socket: %w", err)
	}
	return c, nil
}
