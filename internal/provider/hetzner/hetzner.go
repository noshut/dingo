// Package hetzner implements the Hetzner DNS provider.
package hetzner

import (
	"context"
	"errors"
	"net/netip"

	"github.com/noshut/dingo/internal/provider"
)

func init() {
	provider.Register("hetzner", New)
}

type Hetzner struct {
	token string
}

func New(settings map[string]string) (provider.Provider, error) {
	token := settings["token"]
	if token == "" {
		return nil, errors.New("hetzner: settings.token is required")
	}
	return &Hetzner{token: token}, nil
}

// Upsert looks up the zone and record and creates or updates it.
// TODO: implement against the Hetzner DNS API.
func (h *Hetzner) Upsert(ctx context.Context, rec provider.Record, addr netip.Addr) error {
	return errors.New("hetzner: not implemented yet")
}
