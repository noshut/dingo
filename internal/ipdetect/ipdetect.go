// Package ipdetect determines the public IPv4/IPv6 address via HTTP echo services.
package ipdetect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type Family int

const (
	IPv4 Family = 4
	IPv6 Family = 6
)

// Detector queries sources in order and returns the first valid address.
type Detector struct {
	client  *http.Client
	sources []string
	family  Family
}

// New returns a Detector whose HTTP client is pinned to the given IP family,
// so a dual-stack host reports the correct address per family.
func New(family Family, sources []string) *Detector {
	network := "tcp4"
	if family == IPv6 {
		network = "tcp6"
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, addr)
	}
	return &Detector{
		client:  &http.Client{Transport: transport, Timeout: 10 * time.Second},
		sources: sources,
		family:  family,
	}
}

func (d *Detector) Detect(ctx context.Context) (netip.Addr, error) {
	var errs []error
	for _, src := range d.sources {
		addr, err := d.query(ctx, src)
		if err == nil {
			return addr, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", src, err))
	}
	return netip.Addr{}, errors.Join(errs...)
}

func (d *Detector) query(ctx context.Context, url string) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return netip.Addr{}, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return netip.Addr{}, err
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil {
		return netip.Addr{}, err
	}
	addr = addr.Unmap()
	if (d.family == IPv4) != addr.Is4() {
		return netip.Addr{}, fmt.Errorf("got %s, want IPv%d", addr, d.family)
	}
	return addr, nil
}
