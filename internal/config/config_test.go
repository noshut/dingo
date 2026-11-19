package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadExpandsEnvAndAppliesDefaults(t *testing.T) {
	t.Setenv("TEST_TOKEN", "secret")
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(`
providers:
  hz:
    type: hetzner
    settings:
      token: ${TEST_TOKEN}
records:
  - {provider: hz, zone: example.com, name: home, type: A}
`), 0o600)

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
