// Package hetzner implements a provider for the DNS feature of the Hetzner
// Cloud API (https://docs.hetzner.cloud/reference/cloud#tag/zone-rrsets).
//
// Hetzner has no native DynDNS endpoint, so Upsert reads the RRSet and then
// creates it or replaces its records/TTL. All writes return an Action that is
// polled until it finishes.
package hetzner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/noshut/dingo/internal/provider"
)

const defaultEndpoint = "https://api.hetzner.cloud/v1"

func init() {
	provider.Register("hetzner", New)
}

type Hetzner struct {
	token        string
	endpoint     string
	client       *http.Client
	pollInterval time.Duration
	actionWait   time.Duration
}

// New expects settings.token (Cloud project API token with read & write
// permission). settings.endpoint overrides the API base URL (used in tests).
func New(settings map[string]string) (provider.Provider, error) {
	token := settings["token"]
	if token == "" {
		return nil, errors.New("hetzner: settings.token is required")
	}
	endpoint := strings.TrimRight(settings["endpoint"], "/")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	return &Hetzner{
		token:        token,
		endpoint:     endpoint,
		client:       &http.Client{Timeout: 15 * time.Second},
		pollInterval: time.Second,
		actionWait:   time.Minute,
	}, nil
}

type record struct {
	Value string `json:"value"`
}

type rrset struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	TTL     *int     `json:"ttl"`
	Records []record `json:"records"`
}

type action struct {
	ID     int64  `json:"id"`
	Status string `json:"status"` // running, success, error
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type actionResponse struct {
	Action action `json:"action"`
}

// APIError is the error envelope returned by the Hetzner Cloud API.
type APIError struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("hetzner: %d %s: %s", e.Status, e.Code, e.Message)
}

func (h *Hetzner) Upsert(ctx context.Context, rec provider.Record, addr netip.Addr) error {
	zone := strings.ToLower(strings.TrimSuffix(rec.Zone, "."))
	name := strings.ToLower(rec.Name)
	rrPath := fmt.Sprintf("/zones/%s/rrsets/%s/%s", url.PathEscape(zone), url.PathEscape(name), rec.Type)
	want := []record{{Value: addr.String()}}

	var cur struct {
		RRSet rrset `json:"rrset"`
	}
	err := h.do(ctx, http.MethodGet, rrPath, nil, &cur)
	var apiErr *APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Code == "not_found":
		ttl := rec.TTL
		var resp actionResponse
		body := rrset{Name: name, Type: rec.Type, TTL: &ttl, Records: want}
		if err := h.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/rrsets", url.PathEscape(zone)), body, &resp); err != nil {
			return fmt.Errorf("create rrset: %w", err)
		}
		return h.wait(ctx, resp.Action)
	case err != nil:
		return fmt.Errorf("get rrset: %w", err)
	}

	if !sameRecords(cur.RRSet.Records, want) {
		var resp actionResponse
		if err := h.do(ctx, http.MethodPost, rrPath+"/actions/set_records", map[string]any{"records": want}, &resp); err != nil {
			return fmt.Errorf("set records: %w", err)
		}
		if err := h.wait(ctx, resp.Action); err != nil {
			return err
		}
	}
	if cur.RRSet.TTL == nil || *cur.RRSet.TTL != rec.TTL {
		var resp actionResponse
		if err := h.do(ctx, http.MethodPost, rrPath+"/actions/change_ttl", map[string]any{"ttl": rec.TTL}, &resp); err != nil {
			return fmt.Errorf("change ttl: %w", err)
		}
		if err := h.wait(ctx, resp.Action); err != nil {
			return err
		}
	}
	return nil
}

func sameRecords(a, b []record) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

// wait polls the action until it leaves the "running" state.
func (h *Hetzner) wait(ctx context.Context, a action) error {
	ctx, cancel := context.WithTimeout(ctx, h.actionWait)
	defer cancel()
	for a.Status == "running" {
		select {
		case <-ctx.Done():
			return fmt.Errorf("action %d: %w", a.ID, ctx.Err())
		case <-time.After(h.pollInterval):
		}
		var resp actionResponse
		if err := h.do(ctx, http.MethodGet, fmt.Sprintf("/zones/actions/%d", a.ID), nil, &resp); err != nil {
			return fmt.Errorf("poll action %d: %w", a.ID, err)
		}
		a = resp.Action
	}
	if a.Status == "error" {
		if a.Error != nil {
			return fmt.Errorf("action %d failed: %s: %s", a.ID, a.Error.Code, a.Error.Message)
		}
		return fmt.Errorf("action %d failed", a.ID)
	}
	return nil
}

func (h *Hetzner) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var env struct {
			Error APIError `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		env.Error.Status = resp.StatusCode
		return &env.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
