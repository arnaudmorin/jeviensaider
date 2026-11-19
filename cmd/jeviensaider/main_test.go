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
	"strings"
	"testing"
	"time"

	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
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
	path := filepath.Join(t.TempDir(), "jeviensaider.db")
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
		cleanupLoop(ctx, st, store.Retention{DefaultDays: 60, MaxDays: 60}, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

func TestLoadConfig(t *testing.T) {
	for _, k := range []string{"ADDR", "DB", "BASE_URL", "TRUST_PROXY", "RETENTION_DAYS", "RETENTION_MAX_DAYS", "CREATE_PASSWORD", "OPEN_CREATION", "SMTP_HOST",
		"MAIL_CREATED_PER_ADDRESS_PER_DAY", "MAIL_CREATED_PER_HOUR", "MAIL_SIGNUPS_PER_EVENT_PER_HOUR", "MAIL_PER_HOUR"} {
		t.Setenv("JEVIENSAIDER_"+k, "")
	}
	c, err := loadConfig(nil)
	if err != nil || c.addr != ":8080" || c.dbPath != "jeviensaider.db" || c.trustProxy || c.retention.DefaultDays != 60 || c.smtp != nil {
		t.Fatalf("defaults: %+v %v", c, err)
	}

	// Environment, then flags over it.
	t.Setenv("JEVIENSAIDER_TRUST_PROXY", "true")
	t.Setenv("JEVIENSAIDER_BASE_URL", "https://jeviensaider.fr/")
	t.Setenv("JEVIENSAIDER_RETENTION_DAYS", "off")
	c, err = loadConfig([]string{"-addr", "127.0.0.1:9000"})
	if err != nil || !c.trustProxy || c.baseURL != "https://jeviensaider.fr" || c.retention.DefaultDays != 0 || c.addr != "127.0.0.1:9000" {
		t.Fatalf("environment and flags: %+v %v", c, err)
	}

	// Invalid values are errors, never a silent default.
	for key, bad := range map[string]string{
		"JEVIENSAIDER_TRUST_PROXY":        "yes please",
		"JEVIENSAIDER_OPEN_CREATION":      "maybe",
		"JEVIENSAIDER_MAIL_PER_HOUR":      "lots",
		"JEVIENSAIDER_RETENTION_MAX_DAYS": "off",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, bad)
			if key == "JEVIENSAIDER_OPEN_CREATION" {
				t.Setenv("JEVIENSAIDER_SMTP_HOST", "smtp.example.org")
			}
			if _, err := loadConfig(nil); err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("%s=%q: %v", key, bad, err)
			}
		})
	}
}

// The retention policy comes from the two values, and a flag overrides its
// variable.
func TestLoadConfigRetention(t *testing.T) {
	for _, k := range []string{"RETENTION_DAYS", "RETENTION_MAX_DAYS"} {
		t.Setenv("JEVIENSAIDER_"+k, "")
	}
	c, err := loadConfig(nil)
	if err != nil || c.retention != (store.Retention{DefaultDays: 60, MaxDays: 180}) {
		t.Fatalf("defaults: %+v %v", c, err)
	}

	// A long default with no maximum set still starts (upgrade of an old
	// deployment), with a maximum equal to the default.
	t.Setenv("JEVIENSAIDER_RETENTION_DAYS", "365")
	if c, err = loadConfig(nil); err != nil || c.retention != (store.Retention{DefaultDays: 365, MaxDays: 365}) {
		t.Fatalf("long default: %+v %v", c, err)
	}

	// Both values set and in conflict: no start.
	t.Setenv("JEVIENSAIDER_RETENTION_DAYS", "90")
	t.Setenv("JEVIENSAIDER_RETENTION_MAX_DAYS", "30")
	if _, err = loadConfig(nil); err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("a default above the maximum was accepted: %v", err)
	}

	// The flag wins over the variable, and fixes the conflict.
	if c, err = loadConfig([]string{"-retention-max-days", "120"}); err != nil || c.retention != (store.Retention{DefaultDays: 90, MaxDays: 120}) {
		t.Fatalf("flag over environment: %+v %v", c, err)
	}

	// 0 means the default maximum, from the variable as from the flag.
	t.Setenv("JEVIENSAIDER_RETENTION_MAX_DAYS", "0")
	if c, err = loadConfig(nil); err != nil || c.retention != (store.Retention{DefaultDays: 90, MaxDays: 180}) {
		t.Fatalf("maximum 0 from the environment: %+v %v", c, err)
	}
	t.Setenv("JEVIENSAIDER_RETENTION_MAX_DAYS", "30")
	if c, err = loadConfig([]string{"-retention-max-days", "0"}); err != nil || c.retention != (store.Retention{DefaultDays: 90, MaxDays: 180}) {
		t.Fatalf("maximum 0 from the flag: %+v %v", c, err)
	}

	// Beyond ten years: no start.
	if _, err = loadConfig([]string{"-retention-max-days", "3651"}); err == nil {
		t.Fatal("a maximum above 3650 days was accepted")
	}
	t.Setenv("JEVIENSAIDER_RETENTION_MAX_DAYS", "")

	// Deletion off: nothing to cap.
	t.Setenv("JEVIENSAIDER_RETENTION_DAYS", "off")
	if c, err = loadConfig(nil); err != nil || c.retention.Enabled() {
		t.Fatalf("deletion off: %+v %v", c, err)
	}
}
