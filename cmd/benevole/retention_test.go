package main

import (
	"context"
	"strings"
	"testing"

	"benevole-ape/internal/model"
	"benevole-ape/internal/store"
)

func TestRetentionFromEnv(t *testing.T) {
	for v, want := range map[string]int{"": 60, "0": 0, "off": 0, " Never ": 0, "forever": 0, "90": 90} {
		t.Setenv("BENEVOLE_RETENTION_DAYS", v)
		if got, err := retentionFromEnv(); err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", v, got, err, want)
		}
	}
	for _, v := range []string{"-1", "sixty", "60d", "1.5"} {
		t.Setenv("BENEVOLE_RETENTION_DAYS", v)
		if _, err := retentionFromEnv(); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
}

func TestEventsCommandRejectsBadRetention(t *testing.T) {
	t.Setenv("BENEVOLE_RETENTION_DAYS", "jamais")
	var out, errOut strings.Builder
	if code := runEvents([]string{"list"}, strings.NewReader(""), &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "BENEVOLE_RETENTION_DAYS") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

func TestEventsListKeptForever(t *testing.T) {
	db := t.TempDir() + "/x.db"
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateEvent(context.Background(), &model.Event{Title: "Kermesse", Date: "2026-06-20"}); err != nil {
		t.Fatal(err)
	}
	st.Close()

	t.Setenv("BENEVOLE_DB", db)
	t.Setenv("BENEVOLE_RETENTION_DAYS", "off")
	var out, errOut strings.Builder
	if code := runEvents([]string{"list"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Kermesse") || !strings.Contains(out.String(), "never") {
		t.Errorf("expected the event with EXPIRES never:\n%s", out.String())
	}
}
