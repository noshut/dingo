package ipdetect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDetectFallsBackToNextSource(t *testing.T) {
	bad := serve(t, http.StatusInternalServerError, "")
	good := serve(t, http.StatusOK, "203.0.113.7\n")

	addr, err := New(IPv4, []string{bad, good}).Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if addr.String() != "203.0.113.7" {
		t.Fatalf("got %s", addr)
	}
}

func TestDetectRejectsWrongFamily(t *testing.T) {
	src := serve(t, http.StatusOK, "2001:db8::1")
	if _, err := New(IPv4, []string{src}).Detect(context.Background()); err == nil {
		t.Fatal("expected error for IPv6 answer on IPv4 detector")
	}
}
