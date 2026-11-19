package watch

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"time"
)

// ICMPPinger sends ICMPv4 echo requests. It prefers unprivileged ICMP
// datagram sockets (needs net.ipv4.ping_group_range to include the process
// gid) and falls back to raw sockets (needs root or CAP_NET_RAW).
type ICMPPinger struct {
	Timeout time.Duration
	network string // "udp4" or "ip4:icmp", chosen by Probe
	seq     atomic.Uint32
}

// Probe checks which socket type is usable. It must succeed before Ping.
func (p *ICMPPinger) Probe() error {
	var errs []error
	for _, network := range []string{"udp4", "ip4:icmp"} {
		c, err := net.ListenPacket(network, "0.0.0.0")
		if err == nil {
			_ = c.Close()
			p.network = network
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", network, err))
	}
	return fmt.Errorf("no usable ICMP socket (set net.ipv4.ping_group_range or grant CAP_NET_RAW): %w", errors.Join(errs...))
}

// Ping sends one echo request to addr and waits for the matching reply.
func (p *ICMPPinger) Ping(ctx context.Context, addr netip.Addr) error {
	if !addr.Is4() {
		return fmt.Errorf("ping: %s is not an IPv4 address", addr)
	}
	if p.network == "" {
		return errors.New("ping: Probe not called")
	}
	conn, err := net.ListenPacket(p.network, "0.0.0.0")
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	timeout := p.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	id := uint16(os.Getpid() & 0xffff)
	seq := uint16(p.seq.Add(1))
	payload := []byte("dingo")
	msg := echoRequest(id, seq, payload)

	var dst net.Addr = &net.IPAddr{IP: addr.AsSlice()}
	if p.network == "udp4" {
		dst = &net.UDPAddr{IP: addr.AsSlice()}
	}
	if _, err := conn.WriteTo(msg, dst); err != nil {
		return err
	}

	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		if !sameIP(from, addr) {
			continue
		}
		rid, rseq, ok := parseEchoReply(buf[:n])
		// Datagram sockets rewrite the identifier, so only raw sockets check it.
		if ok && rseq == seq && (p.network == "udp4" || rid == id) {
			return nil
		}
	}
}

func sameIP(a net.Addr, want netip.Addr) bool {
	var ip net.IP
	switch v := a.(type) {
	case *net.IPAddr:
		ip = v.IP
	case *net.UDPAddr:
		ip = v.IP
	default:
		return false
	}
	got, ok := netip.AddrFromSlice(ip)
	return ok && got.Unmap() == want
}

func echoRequest(id, seq uint16, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	b[0] = 8 // echo request
	binary.BigEndian.PutUint16(b[4:], id)
	binary.BigEndian.PutUint16(b[6:], seq)
	copy(b[8:], payload)
	binary.BigEndian.PutUint16(b[2:], checksum(b))
	return b
}

func parseEchoReply(b []byte) (id, seq uint16, ok bool) {
	if len(b) < 8 || b[0] != 0 { // echo reply
		return 0, 0, false
	}
	return binary.BigEndian.Uint16(b[4:]), binary.BigEndian.Uint16(b[6:]), true
}

func checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
