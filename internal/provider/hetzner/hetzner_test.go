package hetzner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noshut/dingo/internal/provider"
)

// fakeAPI emulates the RRSet endpoints of the Hetzner Cloud API in memory.
type fakeAPI struct {
	mu       sync.Mutex
	rrsets   map[string]rrset // key: "<name>/<type>"
	calls    []string
	failNext string // action status to return for the next write ("error")
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer tok" {
		writeJSON(w, 401, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "bad token"}})
		return
	}
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	body, _ := io.ReadAll(r.Body)
	p := strings.TrimPrefix(r.URL.Path, "/zones/example.com/rrsets")

	switch {
	case strings.HasPrefix(r.URL.Path, "/zones/actions/"):
		writeJSON(w, 200, map[string]any{"action": action{ID: 1, Status: "success"}})
	case r.Method == http.MethodPost && p == "":
		var rs rrset
		_ = json.Unmarshal(body, &rs)
		f.rrsets[rs.Name+"/"+rs.Type] = rs
		f.writeAction(w)
	case r.Method == http.MethodGet:
		rs, ok := f.rrsets[strings.TrimPrefix(p, "/")]
		if !ok {
			writeJSON(w, 404, map[string]any{"error": map[string]string{"code": "not_found", "message": "rrset not found"}})
			return
		}
		writeJSON(w, 200, map[string]any{"rrset": rs})
	case strings.HasSuffix(p, "/actions/set_records"):
		key := strings.TrimSuffix(strings.TrimPrefix(p, "/"), "/actions/set_records")
		rs := f.rrsets[key]
		var in struct{ Records []record }
		_ = json.Unmarshal(body, &in)
		rs.Records = in.Records
		f.rrsets[key] = rs
		f.writeAction(w)
	case strings.HasSuffix(p, "/actions/change_ttl"):
		key := strings.TrimSuffix(strings.TrimPrefix(p, "/"), "/actions/change_ttl")
		rs := f.rrsets[key]
		var in struct{ TTL int }
		_ = json.Unmarshal(body, &in)
		rs.TTL = &in.TTL
		f.rrsets[key] = rs
		f.writeAction(w)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) writeAction(w http.ResponseWriter) {
	a := action{ID: 1, Status: "running"}
	if f.failNext != "" {
		a.Status = f.failNext
		a.Error = &struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{"boom", "failed"}
		f.failNext = ""
	}
	writeJSON(w, 201, map[string]any{"action": a})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func setup(t *testing.T) (*Hetzner, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{rrsets: map[string]rrset{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	p, err := New(map[string]string{"token": "tok", "endpoint": srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	h := p.(*Hetzner)
	h.pollInterval = time.Millisecond
	return h, f
}

var rec = provider.Record{Zone: "Example.com", Name: "home", Type: "A", TTL: 300}

func TestUpsertCreatesMissingRRSet(t *testing.T) {
	h, f := setup(t)
	if err := h.Upsert(context.Background(), rec, netip.MustParseAddr("203.0.113.1")); err != nil {
		t.Fatal(err)
	}
	got := f.rrsets["home/A"]
	if len(got.Records) != 1 || got.Records[0].Value != "203.0.113.1" || *got.TTL != 300 {
		t.Fatalf("unexpected rrset: %+v", got)
	}
}

func TestUpsertUpdatesChangedRecordsAndTTL(t *testing.T) {
	h, f := setup(t)
	ttl := 60
	f.rrsets["home/A"] = rrset{Name: "home", Type: "A", TTL: &ttl, Records: []record{{Value: "198.51.100.1"}}}

	if err := h.Upsert(context.Background(), rec, netip.MustParseAddr("203.0.113.1")); err != nil {
		t.Fatal(err)
	}
	got := f.rrsets["home/A"]
	if got.Records[0].Value != "203.0.113.1" || *got.TTL != 300 {
		t.Fatalf("unexpected rrset: %+v", got)
	}
}

func TestUpsertNoopWhenUnchanged(t *testing.T) {
	h, f := setup(t)
	ttl := 300
	f.rrsets["home/A"] = rrset{Name: "home", Type: "A", TTL: &ttl, Records: []record{{Value: "203.0.113.1"}}}

	if err := h.Upsert(context.Background(), rec, netip.MustParseAddr("203.0.113.1")); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected only the GET, got %v", f.calls)
	}
}

func TestUpsertReportsFailedAction(t *testing.T) {
	h, f := setup(t)
	f.failNext = "error"
	err := h.Upsert(context.Background(), rec, netip.MustParseAddr("203.0.113.1"))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected action error, got %v", err)
	}
}

func TestUpsertReturnsAPIError(t *testing.T) {
	h, _ := setup(t)
	h.token = "wrong"
	err := h.Upsert(context.Background(), rec, netip.MustParseAddr("203.0.113.1"))
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}
