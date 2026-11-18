package web

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// MailLimits caps outgoing e-mails. Creating an event sends a mail to an
// address typed by the visitor, and every sign-up mails the organiser:
// without limits, the site could be used to flood an inbox or to send spam,
// which would also get its domain blacklisted. A limit of 0 means no limit.
type MailLimits struct {
	CreatedPerAddressPerDay int // "event created" mails to one address, per day
	CreatedPerHour          int // "event created" mails, all addresses, per hour
	SignupsPerEventPerHour  int // sign-up notifications for one event, per hour
	PerHour                 int // all mails, per hour
}

// DefaultMailLimits are used when Config.MailLimits is nil.
func DefaultMailLimits() MailLimits {
	return MailLimits{CreatedPerAddressPerDay: 3, CreatedPerHour: 30, SignupsPerEventPerHour: 20, PerHour: 300}
}

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

// allow records a hit and reports whether it is within max (0: no limit).
func (c *windowCounter) allow(key string, max int, now time.Time) bool {
	if max <= 0 {
		return true
	}
	n, _ := c.hit(key, now)
	return n >= 1 && n <= max
}

// mailBudget holds the limits and their counters.
type mailBudget struct {
	limits    MailLimits
	createdTo *windowCounter // per recipient, per day
	created   *windowCounter // all "created" mails, per hour
	signups   *windowCounter // per event, per hour
	all       *windowCounter // all mails, per hour
}

func newMailBudget(limits MailLimits) *mailBudget {
	return &mailBudget{
		limits:    limits,
		createdTo: newWindowCounter(24 * time.Hour),
		created:   newWindowCounter(time.Hour),
		signups:   newWindowCounter(time.Hour),
		all:       newWindowCounter(time.Hour),
	}
}

// allowMail records one mail of any kind, and reports whether it is within
// the site's hourly limit.
func (b *mailBudget) allowMail(now time.Time) bool {
	return b.all.allow("", b.limits.PerHour, now)
}

// allowCreated records an "event created" mail to addr, and reports whether
// it is within the limits; if not, why says which one.
func (b *mailBudget) allowCreated(addr string, now time.Time) (ok bool, why string) {
	if !b.createdTo.allow(strings.ToLower(addr), b.limits.CreatedPerAddressPerDay, now) {
		return false, "too many for this address today"
	}
	if !b.created.allow("", b.limits.CreatedPerHour, now) {
		return false, "too many events created this hour"
	}
	return true, ""
}

// notice is what to do with a sign-up notification.
type notice int

const (
	noticeSend  notice = iota // send it
	noticePause               // the limit was just exceeded: send "notifications paused" instead
	noticeSkip                // over the limit, already said: send nothing
)

// signupNotice records a sign-up notification for an event and decides what
// to send. Over the hourly limit (a busy hour, or someone signing up and
// cancelling in a loop), the organiser is told once, then nothing until the
// window ends (until).
func (b *mailBudget) signupNotice(eventID int64, now time.Time) (n notice, until time.Time) {
	max := b.limits.SignupsPerEventPerHour
	if max <= 0 {
		return noticeSend, time.Time{}
	}
	count, until := b.signups.hit(strconv.FormatInt(eventID, 10), now)
	switch {
	case count >= 1 && count <= max:
		return noticeSend, until
	case count == max+1:
		return noticePause, until
	}
	return noticeSkip, until
}
