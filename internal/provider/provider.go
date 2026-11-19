// Package provider defines the DNS provider abstraction and a registry that
// provider implementations add themselves to via init().
package provider

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"sync"
)

// Record identifies a single DNS record to keep in sync.
type Record struct {
	Zone string // e.g. example.com
	Name string // e.g. "home" or "@"
	Type string // A or AAAA
	TTL  int
}

// Provider creates or updates DNS records. Implementations must be idempotent:
// Upsert creates the record if missing, updates it if the value differs and
// does nothing otherwise. This is how providers without native DynDNS
// (e.g. Hetzner) are supported.
type Provider interface {
	// Upsert reports whether it wrote anything. changed may be true together
	// with an error if a later step of a multi-step update failed.
	Upsert(ctx context.Context, rec Record, addr netip.Addr) (changed bool, err error)
}

// Factory builds a Provider from its settings block in the config.
type Factory func(settings map[string]string) (Provider, error)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register makes a provider type available. Called from implementations' init().
func Register(typ string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[typ]; dup {
		panic("provider: duplicate registration of " + typ)
	}
	factories[typ] = f
}

func New(typ string, settings map[string]string) (Provider, error) {
	mu.RLock()
	f, ok := factories[typ]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown provider type %q (available: %v)", typ, Types())
	}
	return f(settings)
}

func Types() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for t := range factories {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
