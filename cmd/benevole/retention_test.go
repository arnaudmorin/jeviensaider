package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"benevole-ape/internal/model"
	"benevole-ape/internal/store"
)

func TestRetentionMaxFromEnv(t *testing.T) {
	// Empty and 0 mean the default maximum: compose.yaml passes an empty
	// string when the variable is missing from .env, and the flag takes 0.
	for v, want := range map[string]int{"": 0, "  ": 0, "0": 0, "90": 90, " 365 ": 365} {
		t.Setenv("BENEVOLE_RETENTION_MAX_DAYS", v)
		if got, err := retentionMaxFromEnv(); err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", v, got, err, want)
		}
	}
	// "off" is refused: a maximum that allows anything is no maximum.
	for _, v := range []string{"off", "forever", "-1", "abc", "1.5"} {
		t.Setenv("BENEVOLE_RETENTION_MAX_DAYS", v)
		if _, err := retentionMaxFromEnv(); err == nil || !strings.Contains(err.Error(), "BENEVOLE_RETENTION_MAX_DAYS") {
			t.Errorf("%q accepted or the error does not name the variable: %v", v, err)
		}
	}
}

func TestEventsCommandUsesTheRetentionOfEachEvent(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/x.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	slugs := map[int]string{}
	for _, days := range []int{0, 10, 400} {
		ev := &model.Event{Title: "Fête", Date: "2026-06-20", RetentionDays: days}
		if err := st.CreateEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
		slugs[days] = ev.Slug
	}
	st.Close()
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE events SET created_at = '2026-01-01 10:00:00'`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	t.Setenv("BENEVOLE_DB", dbPath)
	t.Setenv("BENEVOLE_RETENTION_DAYS", "60")
	t.Setenv("BENEVOLE_RETENTION_MAX_DAYS", "180")
	run := func(args ...string) (int, string, string) {
		var out, errOut strings.Builder
		code := runEvents(args, strings.NewReader(""), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	line := func(out, slug string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, slug) {
				return l
			}
		}
		t.Fatalf("no line for %s in:\n%s", slug, out)
		return ""
	}

	code, out, errOut := run("list")
	if code != 0 {
		t.Fatalf("list: exit %d: %s", code, errOut)
	}
	for days, want := range map[int]string{0: "2026-08-20", 10: "2026-07-01", 400: "2026-12-18"} {
		if l := line(out, slugs[days]); !strings.Contains(l, want) {
			t.Errorf("event with %d days: want expiry %s in %q", days, want, l)
		}
	}

	for days, want := range map[int]string{
		0:   "60 days (server default)",
		10:  "10 days",
		400: "180 days (cut from 400 by the maximum)",
	} {
		code, out, errOut := run("show", slugs[days])
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("show, event with %d days: exit %d, want %q in:\n%s%s", days, code, want, out, errOut)
		}
	}

	// A flag overrides the environment: the event that chose 400 days now
	// goes at 70 days.
	code, out, errOut = run("list", "-retention-max-days", "70")
	if code != 0 {
		t.Fatalf("list -retention-max-days: exit %d: %s", code, errOut)
	}
	if l := line(out, slugs[400]); !strings.Contains(l, "2026-08-30") {
		t.Errorf("with a maximum of 70 days: %q", l)
	}
}

func TestEventsCommandRejectsConflictingRetention(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"a default above the maximum": {"BENEVOLE_RETENTION_DAYS": "90", "BENEVOLE_RETENTION_MAX_DAYS": "30"},
		"a bad maximum":               {"BENEVOLE_RETENTION_MAX_DAYS": "off"},
	} {
		for k, v := range env {
			t.Setenv(k, v)
		}
		var out, errOut strings.Builder
		code := runEvents([]string{"list"}, strings.NewReader(""), &out, &errOut)
		if code != 2 || !strings.Contains(errOut.String(), "MAX_DAYS") && !strings.Contains(errOut.String(), "maximum") {
			t.Errorf("%s: exit %d, stderr %q", name, code, errOut.String())
		}
		t.Setenv("BENEVOLE_RETENTION_DAYS", "")
		t.Setenv("BENEVOLE_RETENTION_MAX_DAYS", "")
	}
}

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
