package health

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noshut/dingo/internal/updater"
)

var now = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func status(lastCycle time.Time, recs ...updater.RecordStatus) updater.Status {
	return updater.Status{Interval: 5 * time.Minute, Started: now.Add(-time.Hour), LastCycle: lastCycle, Records: recs}
}

func TestEvaluate(t *testing.T) {
	failAfter := 15 * time.Minute
	tests := []struct {
		name string
		s    updater.Status
		want string
	}{
		{"starting up", updater.Status{Interval: 5 * time.Minute}, StatusOK},
		{"healthy", status(now.Add(-time.Minute), updater.RecordStatus{LastOK: now.Add(-time.Minute)}), StatusOK},
		{"stalled loop", status(now.Add(-13 * time.Minute)), StatusStalled},
		{"recent failure tolerated", status(now.Add(-time.Minute), updater.RecordStatus{Failing: true, LastOK: now.Add(-6 * time.Minute)}), StatusOK},
		{"persistent failure", status(now.Add(-time.Minute), updater.RecordStatus{Failing: true, LastOK: now.Add(-20 * time.Minute)}), StatusFailing},
		{"never succeeded", status(now.Add(-time.Minute), updater.RecordStatus{Failing: true}), StatusFailing},
	}
	for _, tt := range tests {
		if got := Evaluate(tt.s, failAfter, now).Status; got != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}

func TestServeAndCheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	var healthy atomic.Bool
	healthy.Store(true)
	snap := func() updater.Status {
		s := updater.Status{Interval: time.Minute, Started: time.Now(), LastCycle: time.Now()}
		if !healthy.Load() {
			s.Records = []updater.RecordStatus{{Name: "home", Failing: true}}
			s.Started = time.Now().Add(-time.Hour)
		}
		return s
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Serve(ctx, addr, snap, 3*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	var checkErr error
	for range 50 { // wait for the listener
		if checkErr = Check(context.Background(), addr); checkErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if checkErr != nil {
		t.Fatalf("expected healthy: %v", checkErr)
	}
	if got, ok := PublishedAddr(); !ok || got != addr {
		t.Fatalf("published addr = %q, %v; want %q", got, ok, addr)
	}

	healthy.Store(false)
	err = Check(context.Background(), addr)
	if err == nil || !strings.Contains(err.Error(), `"status":"failing"`) {
		t.Fatalf("expected failing, got %v", err)
	}
}
