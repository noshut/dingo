// Package config loads and validates the dingo YAML configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	LogLevel  string                    `yaml:"log_level"` // debug, info, warn, error
	Interval  time.Duration             `yaml:"interval"`
	IPSources IPSources                 `yaml:"ip_sources"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Records   []Record                  `yaml:"records"`
	Watch     Watch                     `yaml:"watch"`
	Health    Health                    `yaml:"health"`
}

// Health configures the /healthz endpoint used by `dingo -healthcheck`.
type Health struct {
	Enabled   *bool         `yaml:"enabled"`    // default true
	Listen    string        `yaml:"listen"`     // default 127.0.0.1:9180
	FailAfter time.Duration `yaml:"fail_after"` // default 3x interval
}

// HealthEnabled reports whether the health endpoint should run.
func (c *Config) HealthEnabled() bool { return c.Health.Enabled == nil || *c.Health.Enabled }

// Watch pings the current public IPv4 address to notice an outage or IP
// change early and trigger an immediate sync.
type Watch struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"` // default 15s
	Failures int           `yaml:"failures"` // consecutive failed pings, default 2
}

type IPSources struct {
	IPv4 []string `yaml:"ipv4"`
	IPv6 []string `yaml:"ipv6"`
}

// ProviderConfig is a named provider instance. Type selects the implementation
// (e.g. "hetzner"); Settings is passed through to the provider factory.
type ProviderConfig struct {
	Type     string            `yaml:"type"`
	Settings map[string]string `yaml:"settings"`
}

type Record struct {
	Provider string `yaml:"provider"` // key in Config.Providers
	Zone     string `yaml:"zone"`     // e.g. example.com
	Name     string `yaml:"name"`     // e.g. "home" or "@"
	Type     string `yaml:"type"`     // A or AAAA
	TTL      int    `yaml:"ttl"`
}

var quotedValue = regexp.MustCompile("`[^`]*`")

var (
	defaultIPv4 = []string{"https://api.ipify.org", "https://ipv4.icanhazip.com"}
	defaultIPv6 = []string{"https://api6.ipify.org", "https://ipv6.icanhazip.com"}
)

// Load reads the file at path, expands ${ENV} references and applies defaults.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal([]byte(os.ExpandEnv(string(raw))), &c); err != nil {
		// yaml errors quote (a prefix of) the offending value in backticks,
		// which may be an expanded secret such as an API token.
		return nil, fmt.Errorf("parse %s: %s", path, quotedValue.ReplaceAllString(err.Error(), "`[REDACTED]`"))
	}
	c.applyDefaults()
	return &c, c.Validate()
}

func (c *Config) applyDefaults() {
	if c.Interval == 0 {
		c.Interval = 5 * time.Minute
	}
	if c.Health.Listen == "" {
		c.Health.Listen = "127.0.0.1:9180"
	}
	if c.Health.FailAfter == 0 {
		c.Health.FailAfter = 3 * c.Interval
	}
	if c.Watch.Interval == 0 {
		c.Watch.Interval = 15 * time.Second
	}
	if c.Watch.Failures == 0 {
		c.Watch.Failures = 2
	}
	if len(c.IPSources.IPv4) == 0 {
		c.IPSources.IPv4 = defaultIPv4
	}
	if len(c.IPSources.IPv6) == 0 {
		c.IPSources.IPv6 = defaultIPv6
	}
	for i := range c.Records {
		if c.Records[i].TTL == 0 {
			c.Records[i].TTL = 300
		}
	}
}

func (c *Config) Validate() error {
	var errs []error
	if c.LogLevel != "" {
		var l slog.Level
		if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
			errs = append(errs, fmt.Errorf("log_level: %w", err))
		}
	}
	if len(c.Records) == 0 {
		errs = append(errs, errors.New("no records configured"))
	}
	for i, r := range c.Records {
		if _, ok := c.Providers[r.Provider]; !ok {
			errs = append(errs, fmt.Errorf("records[%d]: unknown provider %q", i, r.Provider))
		}
		if r.Type != "A" && r.Type != "AAAA" {
			errs = append(errs, fmt.Errorf("records[%d]: type must be A or AAAA, got %q", i, r.Type))
		}
		if r.Zone == "" || r.Name == "" {
			errs = append(errs, fmt.Errorf("records[%d]: zone and name are required", i))
		}
	}
	return errors.Join(errs...)
}
