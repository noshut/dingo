package ipdetect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRedactURL(t *testing.T) {
	got := RedactURL("https://user:pass@ip.example.com/v1?apikey=secret&format=text#frag")
	for _, leak := range []string{"user", "pass", "secret", "text", "frag"} {
		if strings.Contains(got, leak) {
			t.Fatalf("%q leaked in %s", leak, got)
		}
	}
	if !strings.HasPrefix(got, "https://REDACTED@ip.example.com/v1?") {
		t.Fatalf("unexpected %s", got)
	}
}

func TestDetectErrorDoesNotContainQuery(t *testing.T) {
	_, err := New(IPv4, []string{"http://127.0.0.1:1/?token=secret"}).Detect(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected redacted error, got %v", err)
	}
}
