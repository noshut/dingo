package watch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakePinger struct {
	mu sync.Mutex
	ok bool
}

func (f *fakePinger) set(ok bool) { f.mu.Lock(); f.ok = ok; f.mu.Unlock() }

func (f *fakePinger) Ping(context.Context, netip.Addr) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ok {
		return nil
	}
	return errors.New("timeout")
}

func run(t *testing.T, p Pinger, d time.Duration) int32 {
	t.Helper()
	var triggers atomic.Int32
	w := &Watcher{
		Interval:    5 * time.Millisecond,
		Failures:    2,
		MaxBackoff:  time.Hour,
		Pinger:      p,
		CurrentAddr: func() netip.Addr { return netip.MustParseAddr("203.0.113.1") },
		Trigger:     func() { triggers.Add(1) },
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	w.Run(ctx)
	return triggers.Load()
}

func TestNoTriggerWhileReachable(t *testing.T) {
	if n := run(t, &fakePinger{ok: true}, 50*time.Millisecond); n != 0 {
		t.Fatalf("expected no trigger, got %d", n)
	}
}

func TestTriggerWithBackoffWhileUnreachable(t *testing.T) {
	// 100ms at 5ms interval = ~20 pings; with backoff 5,10,20,40,80ms only a
	// handful of triggers may fire.
	n := run(t, &fakePinger{ok: false}, 100*time.Millisecond)
	if n < 1 || n > 6 {
		t.Fatalf("expected 1..6 triggers with backoff, got %d", n)
	}
}

func TestChecksum(t *testing.T) {
	msg := echoRequest(0x1234, 1, []byte("dingo"))
	// a correct checksum makes the checksum over the whole message zero
	if c := checksum(msg); c != 0 {
		t.Fatalf("checksum verification failed: %#x", c)
	}
}

func TestPingLoopback(t *testing.T) {
	p := &ICMPPinger{Timeout: time.Second}
	if err := p.Probe(); err != nil {
		t.Skipf("no ICMP socket available: %v", err)
	}
	if err := p.Ping(context.Background(), netip.MustParseAddr("127.0.0.1")); err != nil {
		t.Fatalf("ping 127.0.0.1 via %s: %v", p.network, err)
	}
}

func TestRecoveryResetsFailures(t *testing.T) {
	p := &fakePinger{ok: false}
	go func() { time.Sleep(30 * time.Millisecond); p.set(true) }()
	n := run(t, p, 120*time.Millisecond)
	// at most the triggers before recovery (backoff 5,10,20ms within 30ms)
	if n < 1 || n > 3 {
		t.Fatalf("expected 1..3 triggers before recovery, got %d", n)
	}
}
