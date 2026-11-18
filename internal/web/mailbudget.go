package web

import (
	"sync"
	"time"
)

// Limits on outgoing e-mails. Creating an event sends a mail to an address
// typed by the visitor, and every sign-up mails the organiser: without
// limits, the site could be used to flood an inbox or to send spam, which
// would also get its domain blacklisted.
const (
	createdPerRecipientPerDay = 3   // "event created" mails to one address
	createdPerHour            = 30  // "event created" mails, all addresses
	signupMailsPerEventHour   = 20  // sign-up notifications for one event
	mailsPerHour              = 300 // all mails
)

// windowCounter counts hits per key over fixed time windows. Its memory is
// bounded: when it tracks too many keys, new keys are refused.
type windowCounter struct {
	mu   sync.Mutex
	per  time.Duration
	keys map[string]*window
}

type window struct {
	start time.Time
	n     int
}

const maxCounterKeys = 10000

func newWindowCounter(per time.Duration) *windowCounter {
	return &windowCounter{per: per, keys: map[string]*window{}}
}

// hit records one hit for key and returns how many hits the key has had in
// the current window, this one included, and when that window ends. It
// returns 0 when it cannot track the key.
func (c *windowCounter) hit(key string, now time.Time) (int, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.keys[key]
	if ok && now.Sub(w.start) >= c.per {
		w.start, w.n = now, 0
	}
	if !ok {
		if len(c.keys) >= maxCounterKeys {
			for k, w := range c.keys {
				if now.Sub(w.start) >= c.per {
					delete(c.keys, k)
				}
			}
			if len(c.keys) >= maxCounterKeys {
				return 0, now
			}
		}
		w = &window{start: now}
		c.keys[key] = w
	}
	w.n++
	return w.n, w.start.Add(c.per)
}

// allow records a hit and reports whether it is within max.
func (c *windowCounter) allow(key string, max int, now time.Time) bool {
	n, _ := c.hit(key, now)
	return n >= 1 && n <= max
}

// mailBudget holds the counters of the limits above.
type mailBudget struct {
	createdTo *windowCounter // per recipient, per day
	created   *windowCounter // all "created" mails, per hour
	signups   *windowCounter // per event, per hour
	all       *windowCounter // all mails, per hour
}

func newMailBudget() *mailBudget {
	return &mailBudget{
		createdTo: newWindowCounter(24 * time.Hour),
		created:   newWindowCounter(time.Hour),
		signups:   newWindowCounter(time.Hour),
		all:       newWindowCounter(time.Hour),
	}
}
