// Package ical writes iCalendar files (RFC 5545), so that a volunteer can add
// their slots to a calendar application.
package ical

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Event is one calendar entry.
type Event struct {
	// UID identifies the entry. If a calendar imports the same UID twice, it
	// updates the entry instead of adding a second one.
	UID         string
	Summary     string
	Description string
	Location    string
	URL         string
	// Start and End are wall-clock times. The file writes them without a
	// time zone ("floating"), so each calendar shows the same clock time. The
	// time zone of the value is ignored.
	Start, End time.Time
	// Alarms are the reminders, each one a duration before Start.
	Alarms []time.Duration
}

const (
	timeLayout  = "20060102T150405"
	stampLayout = "20060102T150405Z"
	// maxLineOctets is the longest line RFC 5545 allows, without the CRLF.
	maxLineOctets = 75
)

// Build returns a calendar with the given events. stamp is the time the
// file was made. Every line ends with CRLF, as the RFC requires.
//
// RFC 5545 wants at least one event in a calendar, so a calendar without
// events is not valid. Build still writes the frame, and the caller must not
// send it.
func Build(prodID string, stamp time.Time, events []Event) []byte {
	var b strings.Builder
	line := func(s string) {
		b.WriteString(fold(s))
		b.WriteString("\r\n")
	}
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:" + escapeText(prodID))
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	for _, e := range events {
		line("BEGIN:VEVENT")
		line("UID:" + escapeText(e.UID))
		line("DTSTAMP:" + stamp.UTC().Format(stampLayout))
		line("DTSTART:" + e.Start.Format(timeLayout))
		line("DTEND:" + e.End.Format(timeLayout))
		line("SUMMARY:" + escapeText(e.Summary))
		if e.Description != "" {
			line("DESCRIPTION:" + escapeText(e.Description))
		}
		if e.Location != "" {
			line("LOCATION:" + escapeText(e.Location))
		}
		if e.URL != "" {
			// A URL value is not text: it takes no escaping, but it must not
			// hold a line break.
			line("URL:" + stripControl(e.URL))
		}
		for _, d := range e.Alarms {
			line("BEGIN:VALARM")
			line("ACTION:DISPLAY")
			line("DESCRIPTION:Rappel")
			line("TRIGGER:" + trigger(d))
			line("END:VALARM")
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return []byte(b.String())
}

// isLineBreak says whether r ends a line in some programs, besides CR and LF:
// NEL, the line separator and the paragraph separator of Unicode.
func isLineBreak(r rune) bool {
	return r == '\n' || r == '\u0085' || r == '\u2028' || r == '\u2029'
}

// escapeText escapes a TEXT value. Each kind of line break becomes "\n", and
// the other control characters are removed, so that text typed by a person
// cannot start a new property.
func escapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == ';':
			b.WriteString(`\;`)
		case r == ',':
			b.WriteString(`\,`)
		case r == '\r':
			// "\r\n" is one line break: skip the "\r" and let "\n" write it.
			if i < len(s) && s[i] == '\n' {
				continue
			}
			b.WriteString(`\n`)
		case isLineBreak(r):
			b.WriteString(`\n`)
		case r == '\t' || (r >= 0x20 && r != 0x7f):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripControl removes control characters and line breaks from a value that
// is not escaped.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || isLineBreak(r) {
			return -1
		}
		return r
	}, s)
}

// fold splits a content line that is longer than 75 octets. Each extra line
// starts with one space. It never cuts inside a UTF-8 character.
func fold(s string) string {
	if len(s) <= maxLineOctets {
		return s
	}
	var b strings.Builder
	limit := maxLineOctets
	used := 0
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if used+size > limit {
			b.WriteString("\r\n ")
			used = 0
			limit = maxLineOctets - 1 // the leading space takes one octet
		}
		b.WriteString(s[i : i+size])
		used += size
		i += size
	}
	return b.String()
}

// trigger formats an alarm that fires d before the start of the event.
func trigger(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	minutes := int(d / time.Minute)
	switch {
	case minutes > 0 && minutes%(24*60) == 0:
		return fmt.Sprintf("-P%dD", minutes/(24*60))
	case minutes > 0 && minutes%60 == 0:
		return fmt.Sprintf("-PT%dH", minutes/60)
	default:
		return fmt.Sprintf("-PT%dM", minutes)
	}
}
