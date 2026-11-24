package web

import (
	"strings"
	"time"
	"unicode/utf8"

	"jeviensaider/internal/model"
)

// maxShareDescription is the longest link preview description, in runes.
const maxShareDescription = 200

// shareCard is the data of the link preview tags of a public event page.
type shareCard struct {
	Description string
	URL         string
}

// shareDescription is the one line that a messaging app shows under the
// title of the event link: the date, the place and where the sign-ups
// stand. A part that is empty is left out. The description that the
// organizer wrote is never in the line, because it can be long and can hold
// a phone number. The place is in the line as the organizer typed it, so it
// is as public as the event page.
//
// The state of the sign-ups is left out when the event date is before today,
// because a card for a past event must not invite people to sign up.
func shareDescription(ev *model.Event, p *model.Planning, today time.Time) string {
	var parts []string
	if ev.Date != "" {
		parts = append(parts, dateFR(ev.Date))
	}
	if ev.Location != "" {
		parts = append(parts, ev.Location)
	}
	past := ev.Date != "" && ev.Date < today.Format(model.DateLayout)
	switch {
	case !ev.IsOpen():
		parts = append(parts, "Inscriptions fermées")
	case past:
		// The event is over: no count of places.
	case p.Needed == 0:
		// Only pledge lists, or nothing yet: no count to show.
	case p.Missing > 0:
		parts = append(parts, placesLeft(p.Missing))
	case !listsLackItems(p):
		parts = append(parts, "Équipe complète")
	}
	// One line: fold every run of white space, line breaks included.
	s := strings.Join(strings.Fields(strings.Join(parts, " · ")), " ")
	if utf8.RuneCountInString(s) > maxShareDescription {
		// Cut before the last rune, trim the separator that the cut can
		// leave at the end, and close with the ellipsis.
		kept := string([]rune(s)[:maxShareDescription-1])
		s = strings.TrimRight(kept, " ·") + "…"
	}
	return s
}

// listsLackItems says if a pledge list is still under its minimum. The team
// is not complete while a list still needs items.
func listsLackItems(p *model.Planning) bool {
	for _, l := range p.Pledges {
		if l.Missing > 0 {
			return true
		}
	}
	return false
}
