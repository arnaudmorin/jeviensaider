package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"benevole-ape/internal/model"
	"benevole-ape/internal/store"
)

func TestCheckHealth(t *testing.T) {
	status := http.StatusOK
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer ts.Close()
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())

	// ":port" (all interfaces, as in -addr :8080) is checked on 127.0.0.1.
	for _, addr := range []string{":" + port, "127.0.0.1:" + port, "0.0.0.0:" + port} {
		if code := checkHealth(addr); code != 0 {
			t.Errorf("checkHealth(%s) = %d, want 0", addr, code)
		}
	}
	status = http.StatusServiceUnavailable
	if code := checkHealth(":" + port); code != 1 {
		t.Errorf("unhealthy: %d, want 1", code)
	}
	ts.Close()
	if code := checkHealth(":" + port); code != 1 {
		t.Errorf("server down: %d, want 1", code)
	}
	if code := checkHealth("no port"); code != 1 {
		t.Errorf("bad address: %d, want 1", code)
	}
}

func TestCleanupLoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benevole.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	old := &model.Event{Title: "Fête 2024", Date: "2024-06-20"}
	recent := &model.Event{Title: "Fête 2026"}
	for _, ev := range []*model.Event{old, recent} {
		if err := st.CreateEvent(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	// Pretend the old event was created two years ago.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE events SET created_at = '2024-01-01 10:00:00' WHERE id = ?`, old.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		cleanupLoop(ctx, st, 60, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	// The first pass runs at once.
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := st.EventBySlug(context.Background(), old.Slug)
		if errors.Is(err, store.ErrNotFound) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expired event not deleted: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := st.EventBySlug(context.Background(), recent.Slug); err != nil {
		t.Errorf("recent event deleted: %v", err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanupLoop did not stop")
	}
}
