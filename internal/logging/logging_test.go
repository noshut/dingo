package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	r := &Redactor{}
	r.Add("s3cr3t-token")
	log := New(&buf, slog.LevelDebug, r)

	log.Debug("calling api with s3cr3t-token",
		"token", "anything",
		"Authorization", "Bearer xyz",
		"url", "https://example.com/?key=s3cr3t-token",
		"err", errors.New("bad token s3cr3t-token"),
	)

	out := buf.String()
	if strings.Contains(out, "s3cr3t-token") || strings.Contains(out, "anything") || strings.Contains(out, "xyz") {
		t.Fatalf("secret leaked: %s", out)
	}
	if strings.Count(out, Redacted) != 5 {
		t.Fatalf("expected 5 redactions: %s", out)
	}
}

func TestShortSecretsIgnored(t *testing.T) {
	r := &Redactor{}
	r.Add("ab")
	if got := r.Scrub("abc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"debug": slog.LevelDebug, "INFO": slog.LevelInfo, "warn": slog.LevelWarn, "error": slog.LevelError} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Error("expected error for unknown level")
	}
}
