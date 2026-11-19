package updater

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/noshut/dingo/internal/ipdetect"
	"github.com/noshut/dingo/internal/provider"
)

type fakeProvider struct {
	changed bool
	calls   int
}

func (f *fakeProvider) Upsert(context.Context, provider.Record, netip.Addr) (bool, error) {
	f.calls++
	return f.changed, nil
}

func newUpdater(t *testing.T, p provider.Provider, buf *bytes.Buffer) *Updater {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "203.0.113.1")
	}))
	t.Cleanup(srv.Close)
	return &Updater{
		Detectors: map[ipdetect.Family]*ipdetect.Detector{ipdetect.IPv4: ipdetect.New(ipdetect.IPv4, []string{srv.URL})},
		Targets:   []Target{{Provider: p, ProviderName: "fake", Record: provider.Record{Zone: "example.com", Name: "home", Type: "A"}}},
		Log:       slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
}

func TestRunOnceLogsUpdateOnlyWhenChanged(t *testing.T) {
	for _, changed := range []bool{true, false} {
		var buf bytes.Buffer
		p := &fakeProvider{changed: changed}
		newUpdater(t, p, &buf).RunOnce(context.Background())
		if got := strings.Contains(buf.String(), "record updated"); got != changed {
			t.Errorf("changed=%v: 'record updated' logged=%v\n%s", changed, got, buf.String())
		}
	}
}

func TestRunOnceSkipsUnchangedAddress(t *testing.T) {
	var buf bytes.Buffer
	p := &fakeProvider{changed: true}
	u := newUpdater(t, p, &buf)
	u.RunOnce(context.Background())
	u.RunOnce(context.Background())
	if p.calls != 1 {
		t.Fatalf("expected 1 upsert, got %d", p.calls)
	}
}
