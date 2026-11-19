package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadExpandsEnvAndAppliesDefaults(t *testing.T) {
	t.Setenv("TEST_TOKEN", "secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	err := os.WriteFile(path, []byte(`
providers:
  hz:
    type: hetzner
    settings:
      token: ${TEST_TOKEN}
records:
  - {provider: hz, zone: example.com, name: home, type: A}
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Providers["hz"].Settings["token"] != "secret" {
		t.Error("env var not expanded")
	}
	if c.Interval != 5*time.Minute || c.Records[0].TTL != 300 || len(c.IPSources.IPv4) == 0 {
		t.Errorf("defaults not applied: %+v", c)
	}
}

func TestValidate(t *testing.T) {
	c := &Config{Records: []Record{{Provider: "missing", Zone: "example.com", Name: "x", Type: "MX"}}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadRedactsExpandedSecretsInParseErrors(t *testing.T) {
	t.Setenv("TEST_TOKEN", "super-secret-value")
	path := filepath.Join(t.TempDir(), "config.yaml")
	// token expands into a position where a duration is expected
	if err := os.WriteFile(path, []byte("interval: ${TEST_TOKEN}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if strings.Contains(err.Error(), "super-s") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}
