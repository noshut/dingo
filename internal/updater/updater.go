// Package updater runs the detect-and-update loop.
package updater

import (
	"context"
	"log/slog"
	"net/netip"
	"time"

	"github.com/noshut/dingo/internal/ipdetect"
	"github.com/noshut/dingo/internal/provider"
)

type Target struct {
	Provider     provider.Provider
	ProviderName string
	Record       provider.Record
}

type Updater struct {
	Interval  time.Duration
	Detectors map[ipdetect.Family]*ipdetect.Detector
	Targets   []Target
	Log       *slog.Logger

	// last successfully published address per target index; avoids API calls
	// when the IP is unchanged.
	last map[int]netip.Addr
}

// Run executes one sync immediately and then every Interval until ctx is done.
func (u *Updater) Run(ctx context.Context) {
	ticker := time.NewTicker(u.Interval)
	defer ticker.Stop()
	for {
		u.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunOnce detects the current addresses and upserts every record whose
// address changed since the last successful update.
func (u *Updater) RunOnce(ctx context.Context) {
	if u.last == nil {
		u.last = map[int]netip.Addr{}
	}
	addrs := map[ipdetect.Family]netip.Addr{}
	for i, t := range u.Targets {
		fam := familyOf(t.Record.Type)
		addr, ok := addrs[fam]
		if !ok {
			var err error
			addr, err = u.Detectors[fam].Detect(ctx)
			if err != nil {
				u.Log.Error("ip detection failed", "family", fam, "err", err)
			} else {
				u.Log.Debug("public ip detected", "family", fam, "addr", addr)
			}
			addrs[fam] = addr // cache invalid addr too, so we don't retry per record
		}
		if !addr.IsValid() {
			continue
		}
		log := u.Log.With("provider", t.ProviderName, "zone", t.Record.Zone, "name", t.Record.Name, "type", t.Record.Type)
		if u.last[i] == addr {
			log.Debug("address unchanged, skipping", "addr", addr)
			continue
		}
		changed, err := t.Provider.Upsert(ctx, t.Record, addr)
		if err != nil {
			log.Error("update failed", "addr", addr, "partially_applied", changed, "err", err)
			continue
		}
		u.last[i] = addr
		if changed {
			log.Info("record updated", "addr", addr)
		} else {
			log.Debug("record already up to date", "addr", addr)
		}
	}
}

func familyOf(recordType string) ipdetect.Family {
	if recordType == "AAAA" {
		return ipdetect.IPv6
	}
	return ipdetect.IPv4
}
