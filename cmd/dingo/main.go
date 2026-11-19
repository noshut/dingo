package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/noshut/dingo/internal/config"
	"github.com/noshut/dingo/internal/health"
	"github.com/noshut/dingo/internal/ipdetect"
	"github.com/noshut/dingo/internal/logging"
	"github.com/noshut/dingo/internal/provider"
	"github.com/noshut/dingo/internal/updater"
	"github.com/noshut/dingo/internal/watch"

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
	healthcheck := flag.Bool("healthcheck", false, "query the health endpoint of a running dingo and exit 0 if healthy (for Docker HEALTHCHECK)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *healthcheck {
		os.Exit(runHealthcheck(*cfgPath))
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
	if cfg.HealthEnabled() {
		go func() {
			if err := health.Serve(ctx, cfg.Health.Listen, u.Status, cfg.Health.FailAfter, log); err != nil {
				log.Error("health endpoint failed", "addr", cfg.Health.Listen, "err", err)
			}
		}()
	}
	trigger := make(chan struct{}, 1)
	if cfg.Watch.Enabled {
		startWatcher(ctx, cfg.Watch, u, trigger, log)
	}
	log.Info("dingo started", "version", version, "interval", cfg.Interval, "records", len(u.Targets), "watch", cfg.Watch.Enabled)
	u.Run(ctx, trigger)
	log.Info("dingo stopped")
}

func startWatcher(ctx context.Context, wc config.Watch, u *updater.Updater, trigger chan struct{}, log *slog.Logger) {
	pinger := &watch.ICMPPinger{Timeout: 2 * time.Second}
	if err := pinger.Probe(); err != nil {
		log.Warn("ip watch disabled", "err", err)
		return
	}
	w := &watch.Watcher{
		Interval:    wc.Interval,
		Failures:    wc.Failures,
		MaxBackoff:  u.Interval,
		Pinger:      pinger,
		CurrentAddr: func() netip.Addr { return u.CurrentAddr(ipdetect.IPv4) },
		Trigger: func() {
			select {
			case trigger <- struct{}{}:
			default: // a sync is already pending
			}
		},
		Log: log,
	}
	log.Debug("ip watch started", "interval", wc.Interval, "failures", wc.Failures)
	go w.Run(ctx)
}

// runHealthcheck is the client side of the Docker HEALTHCHECK. It prefers the
// address published by the running daemon and only falls back to the config,
// so it works even if the daemon was started with a different -config.
func runHealthcheck(cfgPath string) int {
	addr, ok := health.PublishedAddr()
	if !ok {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck: no running daemon found and config invalid:", err)
			return 1
		}
		if !cfg.HealthEnabled() {
			fmt.Fprintln(os.Stderr, "healthcheck: health endpoint disabled, skipping")
			return 0
		}
		addr = cfg.Health.Listen
	}
	if err := health.Check(context.Background(), addr); err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	return 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
