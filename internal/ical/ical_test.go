package ical

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var stamp = time.Date(2026, 6, 1, 8, 30, 0, 0, time.UTC)

func TestBuildOneEvent(t *testing.T) {
	out := string(Build("-//test//FR", stamp, []Event{{
		UID:         "fete-12-3@jeviensaider",
		Summary:     "Pêche aux canards, Fête de l'école",
		Description: "Ligne 1\nLigne 2; suite",
		Location:    "Cour",
		URL:         "https://example.org/e/fete",
		Start:       time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC),
		End:         time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC),
		Alarms:      []time.Duration{24 * time.Hour, time.Hour},
	}}))
	want := strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//test//FR",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"BEGIN:VEVENT",
		"UID:fete-12-3@jeviensaider",
		"DTSTAMP:20260601T083000Z",
		"DTSTART:20260620T140000",
		"DTEND:20260620T150000",
		"SUMMARY:Pêche aux canards\\, Fête de l'école",
		"DESCRIPTION:Ligne 1\\nLigne 2\\; suite",
		"LOCATION:Cour",
		"URL:https://example.org/e/fete",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Rappel",
		"TRIGGER:-P1D",
		"END:VALARM",
		"BEGIN:VALARM",
		"ACTION:DISPLAY",
		"DESCRIPTION:Rappel",
		"TRIGGER:-PT1H",
		"END:VALARM",
		"END:VEVENT",
		"END:VCALENDAR",
		"",
	}, "\r\n")
	if out != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
}

func TestBuildOmitsEmptyOptionalFields(t *testing.T) {
	out := string(Build("-//test//FR", stamp, []Event{{
		UID: "u", Summary: "S",
		Start: time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC),
	}}))
	for _, field := range []string{"DESCRIPTION", "LOCATION", "URL", "VALARM"} {
		if strings.Contains(out, field) {
			t.Errorf("output contains %s for an event without it:\n%s", field, out)
		}
	}
}

// A calendar without any event is not valid RFC 5545, which wants at least one
// component. Build still writes the frame, so the caller must not send it.
func TestBuildWithoutEvents(t *testing.T) {
	out := string(Build("-//test//FR", stamp, nil))
	if !strings.HasPrefix(out, "BEGIN:VCALENDAR\r\n") || !strings.HasSuffix(out, "END:VCALENDAR\r\n") {
		t.Errorf("bad calendar:\n%q", out)
	}
	if strings.Contains(out, "VEVENT") {
		t.Errorf("unexpected event:\n%q", out)
	}
}

func TestEscapeText(t *testing.T) {
	for in, want := range map[string]string{
		`a\b`:        `a\\b`,
		"a;b,c":      `a\;b\,c`,
		"a\r\nb\nc":  `a\nb\nc`,
		"a\rb":       `a\nb`,
		"tab\there":  "tab\there",
		"bell\x07":   "bell",
		"del\x7f":    "del",
		"émoji 🙂 ok": "émoji 🙂 ok",
		// Other line breaks of Unicode count as a line break here too.
		"a\u0085b": `a\nb`,
		"a\u2028b": `a\nb`,
		"a\u2029b": `a\nb`,
	} {
		if got := escapeText(in); got != want {
			t.Errorf("escapeText(%q) = %q, want %q", in, got, want)
		}
	}
}

// Text typed by an organiser (stand, event, location) must not be able to
// add a calendar property, whatever the field.
func TestInjectionIsNeutralised(t *testing.T) {
	const evil = "x\r\nATTENDEE:mailto:evil@example.org"
	for _, breaks := range []string{"\r\n", "\n", "\r", "\u0085", "\u2028", "\u2029"} {
		in := strings.Replace(evil, "\r\n", breaks, 1)
		for name, ev := range map[string]Event{
			"UID":         {UID: in, Summary: "S"},
			"Summary":     {UID: "u", Summary: in},
			"Description": {UID: "u", Summary: "S", Description: in},
			"Location":    {UID: "u", Summary: "S", Location: in},
			"URL":         {UID: "u", Summary: "S", URL: in},
		} {
			ev.Start = time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC)
			ev.End = time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC)
			out := string(Build("-//test//FR", stamp, []Event{ev}))
			for _, line := range strings.Split(out, "\r\n") {
				if strings.HasPrefix(line, "ATTENDEE") {
					t.Errorf("%s with %q: injected property:\n%s", name, breaks, out)
				}
			}
		}
	}
}

// The line breaks of Unicode and the DEL character do not end up in a URL.
func TestStripControl(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.example/a":      "https://x.example/a",
		"https://x.example/\r\na":  "https://x.example/a",
		"https://x.example/\x7fa":  "https://x.example/a",
		"https://x.example/\u2028": "https://x.example/",
		"https://x.example/\u2029": "https://x.example/",
		"https://x.example/\u0085": "https://x.example/",
	} {
		if got := stripControl(in); got != want {
			t.Errorf("stripControl(%q) = %q, want %q", in, got, want)
		}
	}
}

// The time of the file is in UTC, whatever the zone of the server.
func TestDTSTAMPIsUTC(t *testing.T) {
	local := time.Date(2026, 6, 1, 10, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	out := string(Build("-//test//FR", local, []Event{{
		UID: "u", Summary: "S",
		Start: time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC),
	}}))
	if !strings.Contains(out, "DTSTAMP:20260601T083000Z\r\n") {
		t.Errorf("DTSTAMP is not in UTC:\n%s", out)
	}
}

func TestFoldLines(t *testing.T) {
	long := "SUMMARY:" + strings.Repeat("é", 100) // 2 octets per rune
	folded := fold(long)
	lines := strings.Split(folded, "\r\n")
	if len(lines) < 3 {
		t.Fatalf("expected several lines, got %d", len(lines))
	}
	var back strings.Builder
	for i, l := range lines {
		if len(l) > 75 {
			t.Errorf("line %d has %d octets", i, len(l))
		}
		if !utf8.ValidString(l) {
			t.Errorf("line %d splits a character: %q", i, l)
		}
		if i > 0 {
			if !strings.HasPrefix(l, " ") {
				t.Errorf("line %d is not a continuation: %q", i, l)
			}
			l = l[1:]
		}
		back.WriteString(l)
	}
	if back.String() != long {
		t.Error("unfolding does not give back the original line")
	}
}

// With one-octet characters, every line is exactly 75 octets: the first one,
// and each continuation (a space and 74 octets).
func TestFoldASCIILine(t *testing.T) {
	long := "DESCRIPTION:" + strings.Repeat("a", 300)
	lines := strings.Split(fold(long), "\r\n")
	for i, l := range lines[:len(lines)-1] {
		if len(l) != 75 {
			t.Errorf("line %d has %d octets, want 75", i, len(l))
		}
	}
	if last := lines[len(lines)-1]; len(last) > 75 || len(last) < 2 {
		t.Errorf("last line has %d octets", len(last))
	}
	var back strings.Builder
	for i, l := range lines {
		if i > 0 {
			l = strings.TrimPrefix(l, " ")
		}
		back.WriteString(l)
	}
	if back.String() != long {
		t.Error("unfolding does not give back the original line")
	}
}

func TestFoldKeepsShortLines(t *testing.T) {
	if got := fold("SUMMARY:court"); got != "SUMMARY:court" {
		t.Errorf("got %q", got)
	}
	exact := "X:" + strings.Repeat("a", 73) // exactly 75 octets
	if got := fold(exact); got != exact {
		t.Errorf("75 octets must not be folded, got %q", got)
	}
}

func TestTrigger(t *testing.T) {
	for d, want := range map[time.Duration]string{
		24 * time.Hour:     "-P1D",
		48 * time.Hour:     "-P2D",
		7 * 24 * time.Hour: "-P7D",
		time.Hour:          "-PT1H",
		36 * time.Hour:     "-PT36H",
		90 * time.Minute:   "-PT90M",
		15 * time.Minute:   "-PT15M",
		0:                  "-PT0M",
		-1 * time.Hour:     "-PT1H",
	} {
		if got := trigger(d); got != want {
			t.Errorf("trigger(%v) = %q, want %q", d, got, want)
		}
	}
}
