package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/noshut/dingo/internal/config"
	"github.com/noshut/dingo/internal/ipdetect"
	"github.com/noshut/dingo/internal/logging"
	"github.com/noshut/dingo/internal/provider"
	"github.com/noshut/dingo/internal/updater"

	// Provider implementations register themselves via init().
	_ "github.com/noshut/dingo/internal/provider/hetzner"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfgPath := flag.String("config", envOr("DINGO_CONFIG", "/etc/dingo/config.yaml"), "path to config file")
	once := flag.Bool("once", false, "run a single update and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	logLevel := flag.String("log-level", os.Getenv("DINGO_LOG_LEVEL"), "log level: debug, info, warn, error (overrides config)")
	debug := flag.Bool("debug", false, "shortcut for -log-level debug")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	level := new(slog.LevelVar)
	redactor := &logging.Redactor{}
	log := logging.New(os.Stdout, level, redactor)
	slog.SetDefault(log)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("invalid config", "err", err)
		os.Exit(1)
	}

	// Precedence: -debug > -log-level / DINGO_LOG_LEVEL > config > info.
	lvl := cfg.LogLevel
	if *logLevel != "" {
		lvl = *logLevel
	}
	if *debug {
		lvl = "debug"
	}
	if lvl != "" {
		l, err := logging.ParseLevel(lvl)
		if err != nil {
			log.Error("invalid log level", "level", lvl, "err", err)
			os.Exit(1)
		}
		level.Set(l)
	}

	providers := map[string]provider.Provider{}
	for name, pc := range cfg.Providers {
		for k, v := range pc.Settings {
			if logging.IsSensitiveKey(k) {
				redactor.Add(v)
			}
		}
		log.Debug("provider configured", "provider", name, "type", pc.Type)
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

	log.Debug("config loaded", "path", *cfgPath, "interval", cfg.Interval, "records", len(cfg.Records), "level", level.Level())
	if *once {
		u.RunOnce(ctx)
		return
	}
	log.Info("dingo started", "version", version, "interval", cfg.Interval, "records", len(u.Targets))
	u.Run(ctx)
	log.Info("dingo stopped")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
