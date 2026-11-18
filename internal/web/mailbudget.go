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

	// The page that e-mails the admin links again has its own limits. They
	// cap the share of the hourly budget that this page can use. These mails
	// also count in PerHour, so the limit of the whole site still holds.
	// Anyone can ask for the mail of any address, so a client that sends many
	// requests could use up the quota of RecoverPerAddressPerDay and keep the
	// organiser from getting a mail. RecoverPerAddressPerClientPerDay stops
	// that: one client can use only that many of the daily mails of an address.
	RecoverPerAddressPerDay          int // recovery mails to one address, per day
	RecoverPerAddressPerClientPerDay int // recovery mails to one address, from one client, per day
	RecoverPerHour                   int // recovery mails, all addresses, per hour
	RecoverPerIPPerHour              int // requests to the page from one client, per hour
}

// DefaultMailLimits are used when Config.MailLimits is nil.
func DefaultMailLimits() MailLimits {
	return MailLimits{
		CreatedPerAddressPerDay: 3, CreatedPerHour: 30, SignupsPerEventPerHour: 20, PerHour: 300,
		RecoverPerAddressPerDay: 3, RecoverPerAddressPerClientPerDay: 1, RecoverPerHour: 30, RecoverPerIPPerHour: 10,
	}
}

// windowCounter counts hits per key over fixed time windows. Its memory is
// bounded: when it tracks too many keys, it drops the key with the oldest
// window to make room. Refusing new keys instead would let someone who fills
// the counter stop every new visitor.
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

// window returns the current window of key, and starts a new one if the
// last one ended. The caller must hold c.mu.
func (c *windowCounter) window(key string, now time.Time) *window {
	w, ok := c.keys[key]
	if ok && now.Sub(w.start) >= c.per {
		w.start, w.n = now, 0
	}
	if ok {
		return w
	}
	if len(c.keys) >= maxCounterKeys {
		var oldestKey string
		var oldest time.Time
		found := false
		for k, w := range c.keys {
			if now.Sub(w.start) >= c.per {
				delete(c.keys, k)
			} else if !found || w.start.Before(oldest) {
				oldest, oldestKey, found = w.start, k, true
			}
		}
		if len(c.keys) >= maxCounterKeys {
			delete(c.keys, oldestKey)
		}
	}
	w = &window{start: now}
	c.keys[key] = w
	return w
}

// hit records one hit for key and returns how many hits the key has had in
// the current window, this one included, and when that window ends.
func (c *windowCounter) hit(key string, now time.Time) (int, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.window(key, now)
	w.n++
	return w.n, w.start.Add(c.per)
}

// allow records a hit and reports whether it is within max (0: no limit).
// A refused hit is recorded too: use take to refuse without recording.
func (c *windowCounter) allow(key string, max int, now time.Time) bool {
	if max <= 0 {
		return true
	}
	n, _ := c.hit(key, now)
	return n <= max
}

// take records a hit for key if the current window has room for it (max 0:
// no limit, nothing is recorded). A refused request leaves the counter as it
// was. The returned function gives the hit back, for a request that another
// limit refuses later. It does nothing if the window of the hit has ended or
// the key was dropped, because the hit is then no longer counted.
func (c *windowCounter) take(key string, max int, now time.Time) (release func(), ok bool) {
	if max <= 0 {
		return func() {}, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.window(key, now)
	if w.n >= max {
		return nil, false
	}
	w.n++
	start := w.start
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if w, ok := c.keys[key]; ok && w.start.Equal(start) && w.n > 0 {
			w.n--
		}
	}, true
}

// mailBudget holds the limits and their counters.
type mailBudget struct {
	limits        MailLimits
	createdTo     *windowCounter // per recipient, per day
	created       *windowCounter // all "created" mails, per hour
	signups       *windowCounter // per event, per hour
	all           *windowCounter // all mails, per hour
	recoverTo     *windowCounter // recovery mails per recipient, per day
	recoverToFrom *windowCounter // recovery mails per recipient and client, per day
	recover       *windowCounter // all recovery mails, per hour
	recoverIP     *windowCounter // requests to the recovery page per client, per hour
}

func newMailBudget(limits MailLimits) *mailBudget {
	return &mailBudget{
		limits:        limits,
		createdTo:     newWindowCounter(24 * time.Hour),
		created:       newWindowCounter(time.Hour),
		signups:       newWindowCounter(time.Hour),
		all:           newWindowCounter(time.Hour),
		recoverTo:     newWindowCounter(24 * time.Hour),
		recoverToFrom: newWindowCounter(24 * time.Hour),
		recover:       newWindowCounter(time.Hour),
		recoverIP:     newWindowCounter(time.Hour),
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
	case count <= max:
		return noticeSend, until
	case count == max+1:
		return noticePause, until
	}
	return noticeSkip, until
}
