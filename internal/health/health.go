// Package health serves /healthz and implements the client side used by
// `dingo -healthcheck` (Docker HEALTHCHECK).
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/noshut/dingo/internal/updater"
)

const (
	StatusOK      = "ok"
	StatusFailing = "failing" // a record has not synced successfully for FailAfter
	StatusStalled = "stalled" // the sync loop has not completed a cycle in time
)

// stallSlack is added to 2×interval to allow for slow detections and
// provider calls (timeouts, action polling) before declaring a stall.
const stallSlack = 2 * time.Minute

type Report struct {
	Status    string                 `json:"status"`
	LastCycle time.Time              `json:"last_cycle,omitzero"`
	Records   []updater.RecordStatus `json:"records"`
}

// Evaluate derives the health report from an updater snapshot.
func Evaluate(s updater.Status, failAfter time.Duration, now time.Time) Report {
	r := Report{Status: StatusOK, LastCycle: s.LastCycle, Records: s.Records}

	// Started is set when the first cycle begins; before that dingo is still
	// starting up and counts as healthy.
	ref := s.LastCycle
	if ref.IsZero() {
		ref = s.Started
	}
	if !ref.IsZero() && now.Sub(ref) > 2*s.Interval+stallSlack {
		r.Status = StatusStalled
		return r
	}

	for _, rec := range s.Records {
		if !rec.Failing {
			continue
		}
		since := rec.LastOK
		if since.IsZero() {
			since = s.Started
		}
		if now.Sub(since) > failAfter {
			r.Status = StatusFailing
			break
		}
	}
	return r
}

// Serve runs the health HTTP server on addr until ctx is done.
func Serve(ctx context.Context, addr string, status func() updater.Status, failAfter time.Duration, log *slog.Logger) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		rep := Evaluate(status(), failAfter, time.Now())
		w.Header().Set("Content-Type", "application/json")
		if rep.Status != StatusOK {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(rep)
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Debug("health endpoint listening", "addr", ln.Addr().String())
	// Publish the actual address so `dingo -healthcheck` works regardless of
	// how the daemon was started (e.g. a -config flag the HEALTHCHECK doesn't see).
	if err := os.WriteFile(AddrFile(), []byte(ln.Addr().String()), 0o644); err != nil {
		log.Debug("could not write health address file", "path", AddrFile(), "err", err)
	} else {
		defer func() { _ = os.Remove(AddrFile()) }()
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// AddrFile is where a running daemon publishes its health listen address.
func AddrFile() string { return filepath.Join(os.TempDir(), "dingo-health.addr") }

// PublishedAddr returns the address written by a running daemon, if any.
func PublishedAddr() (string, bool) {
	b, err := os.ReadFile(AddrFile())
	if err != nil || len(b) == 0 {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

// Check queries /healthz on listen (wildcard hosts are replaced by loopback)
// and returns nil if dingo reports healthy.
func Check(ctx context.Context, listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy (%d): %s", resp.StatusCode, body)
	}
	return nil
}
