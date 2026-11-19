package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"jeviensaider/internal/model"
	"jeviensaider/internal/store"
)

func TestEventsCommand(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "test.db")
	t.Setenv("BENEVOLE_DB", db)
	t.Setenv("BENEVOLE_BASE_URL", "https://example.org/")
	t.Setenv("BENEVOLE_RETENTION_DAYS", "60")

	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	var evs []*model.Event
	for _, title := range []string{"Fête de l'école", "Marché de Noël", "Loto"} {
		ev := &model.Event{Title: title, Date: "2026-06-20"}
		if err := st.CreateEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
		evs = append(evs, ev)
	}
	stand := &model.Stand{EventID: evs[0].ID, Name: "Buvette"}
	if err := st.CreateStand(ctx, stand); err != nil {
		t.Fatal(err)
	}
	st.Close()

	run := func(input string, args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := runEvents(args, strings.NewReader(input), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	mustContain := func(s string, subs ...string) {
		t.Helper()
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				t.Errorf("output does not contain %q:\n%s", sub, s)
			}
		}
	}

	if code, out, _ := run("", "help"); code != 0 || !strings.Contains(out, "Commands:") {
		t.Errorf("help: %d %q", code, out)
	}
	if code, _, _ := run("", "frobnicate"); code != 2 {
		t.Errorf("unknown command: exit %d", code)
	}

	code, out, _ := run("", "list")
	if code != 0 {
		t.Fatalf("list: exit %d", code)
	}
	mustContain(out, "SLUG", evs[0].Slug, "Marché de Noël", "Loto", "draft", "EXPIRES")

	code, out, _ = run("", "show", evs[0].Slug)
	if code != 0 {
		t.Fatalf("show: exit %d", code)
	}
	mustContain(out, "Fête de l'école", "1 stands", "https://example.org/e/"+evs[0].Slug, "https://example.org/a/"+evs[0].AdminToken)

	// A typo in any argument deletes nothing.
	if code, _, errOut := run("y\n", "delete", evs[0].Slug, "nope"); code != 1 || !strings.Contains(errOut, `no event "nope"`) {
		t.Errorf("delete with typo: %d %q", code, errOut)
	}
	// Answering no keeps the event.
	if code, out, _ := run("n\n", "delete", evs[0].Slug); code != 0 || !strings.Contains(out, "Cancelled.") {
		t.Errorf("delete cancelled: %d %q", code, out)
	}
	// Answering yes deletes it.
	if code, out, _ := run("oui\n", "delete", evs[0].Slug); code != 0 || !strings.Contains(out, "Deleted "+evs[0].Slug) {
		t.Errorf("delete confirmed: %d %q", code, out)
	}
	// -y after the arguments, events by ID.
	if code, out, _ := run("", "rm", itoa(evs[1].ID), itoa(evs[2].ID), "-y"); code != 0 || strings.Count(out, "Deleted ") != 2 {
		t.Errorf("delete -y: %d %q", code, out)
	}
	if code, out, _ := run("", "list"); code != 0 || !strings.Contains(out, "No events.") {
		t.Errorf("list after delete: %d %q", code, out)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
