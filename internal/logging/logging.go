// Package logging builds the slog logger and makes sure secrets never reach
// the log output.
//
// Redaction works on two levels:
//   - attributes whose key looks sensitive (token, password, authorization, ...)
//     are replaced entirely;
//   - registered secret values are scrubbed from every string attribute,
//     error and message, so a token embedded in e.g. an error text is masked.
package logging

import (
	"io"
	"log/slog"
	"strings"
	"sync"
)

const Redacted = "[REDACTED]"

// minSecretLen avoids scrubbing trivially short values that would mangle
// unrelated output.
const minSecretLen = 4

var sensitiveKeys = []string{"token", "secret", "password", "passwd", "authorization", "apikey", "api_key", "credential", "private_key"}

// IsSensitiveKey reports whether a key name likely holds a secret.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Redactor holds secret values that must never be logged.
type Redactor struct {
	mu      sync.RWMutex
	secrets []string
}

func (r *Redactor) Add(secret string) {
	if len(secret) < minSecretLen {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.secrets = append(r.secrets, secret)
}

// Scrub replaces all registered secrets in s.
func (r *Redactor) Scrub(s string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, Redacted)
	}
	return s
}

func (r *Redactor) replaceAttr(_ []string, a slog.Attr) slog.Attr {
	if IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, r.Scrub(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, r.Scrub(err.Error()))
		}
	}
	return a
}

// New returns a text logger writing to w whose level is controlled by level.
func New(w io.Writer, level slog.Leveler, r *Redactor) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: r.replaceAttr,
	}))
}

// ParseLevel accepts debug, info, warn and error (case-insensitive).
func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	err := l.UnmarshalText([]byte(s))
	return l, err
}
