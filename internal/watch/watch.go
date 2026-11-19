// Package watch detects a likely public IP change early by pinging the
// current public IPv4 address. The router answers pings to its own WAN
// address locally, so once that stops working the line is down or the
// address has changed, and an immediate sync is triggered instead of waiting
// for the next regular interval.
package watch

import (
	"context"
	"log/slog"
	"net/netip"
	"time"
)

type Pinger interface {
	Ping(ctx context.Context, addr netip.Addr) error
}

type Watcher struct {
	Interval    time.Duration // ping interval
	Failures    int           // consecutive failures before triggering
	MaxBackoff  time.Duration // upper bound between triggers while unreachable
	Pinger      Pinger
	CurrentAddr func() netip.Addr  // last detected public IPv4
	Trigger     func()             // requests an immediate sync (must not block)
	OnPing      func(success bool) // optional hook, e.g. for metrics
	Log         *slog.Logger
}

// Run pings until ctx is done. While the address stays unreachable, triggers
// are spaced with exponential backoff (Interval, 2x, 4x … MaxBackoff) so a
// router that simply ignores ICMP does not cause a sync every few seconds.
func (w *Watcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()

	failures := 0
	backoff := w.Interval
	var nextTrigger time.Time
	var lastAddr netip.Addr

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		addr := w.CurrentAddr()
		if !addr.IsValid() {
			continue // nothing detected yet
		}
		if addr != lastAddr {
			failures, backoff, nextTrigger, lastAddr = 0, w.Interval, time.Time{}, addr
		}

		err := w.Pinger.Ping(ctx, addr)
		if w.OnPing != nil {
			w.OnPing(err == nil)
		}
		if err == nil {
			if failures >= w.Failures {
				w.Log.Info("public ip reachable again", "addr", addr)
			}
			failures, backoff, nextTrigger = 0, w.Interval, time.Time{}
			continue
		}

		failures++
		w.Log.Debug("public ip ping failed", "addr", addr, "failures", failures, "err", err)
		if failures < w.Failures {
			continue
		}
		if failures == w.Failures {
			w.Log.Warn("public ip unreachable, triggering sync", "addr", addr)
		}
		if now := time.Now(); now.After(nextTrigger) {
			w.Trigger()
			nextTrigger = now.Add(backoff)
			backoff = min(backoff*2, w.MaxBackoff)
		}
	}
}
