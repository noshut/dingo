package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/noshut/dingo/internal/config"
	"github.com/noshut/dingo/internal/ipdetect"
	"github.com/noshut/dingo/internal/provider"
	"github.com/noshut/dingo/internal/updater"

	// Provider implementations register themselves via init().
	_ "github.com/noshut/dingo/internal/provider/hetzner"
)

func main() {
	cfgPath := flag.String("config", envOr("DINGO_CONFIG", "/etc/dingo/config.yaml"), "path to config file")
	once := flag.Bool("once", false, "run a single update and exit")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("invalid config", "err", err)
		os.Exit(1)
	}

	providers := map[string]provider.Provider{}
	for name, pc := range cfg.Providers {
		p, err := provider.New(pc.Type, pc.Settings)
		if err != nil {
			log.Error("provider init failed", "provider", name, "err", err)
			os.Exit(1)
		}
		providers[name] = p
	}

	u := &updater.Updater{
		Interval: cfg.Interval,
		Detectors: map[ipdetect.Family]*ipdetect.Detector{
			ipdetect.IPv4: ipdetect.New(ipdetect.IPv4, cfg.IPSources.IPv4),
			ipdetect.IPv6: ipdetect.New(ipdetect.IPv6, cfg.IPSources.IPv6),
		},
		Log: log,
	}
	for _, r := range cfg.Records {
		u.Targets = append(u.Targets, updater.Target{
			Provider:     providers[r.Provider],
			ProviderName: r.Provider,
			Record:       provider.Record{Zone: r.Zone, Name: r.Name, Type: r.Type, TTL: r.TTL},
		})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *once {
		u.RunOnce(ctx)
		return
	}
	log.Info("dingo started", "interval", cfg.Interval, "records", len(u.Targets))
	u.Run(ctx)
	log.Info("dingo stopped")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
