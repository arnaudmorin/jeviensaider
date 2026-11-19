package store

import "fmt"

// CeilingRetentionDays is the longest number of days that the server or an
// organiser can ever set (ten years). It keeps dates computed by SQLite
// valid, and the database refuses longer values (migration 004).
const CeilingRetentionDays = 3650

// DefaultMaxRetentionDays is the maximum when the operator sets none.
const DefaultMaxRetentionDays = 180

// Retention is the deletion policy of the server. Build it with NewRetention.
type Retention struct {
	// DefaultDays applies to an event whose organiser chose no duration.
	// 0 turns automatic deletion off: every event is kept forever, whatever
	// duration it stores.
	DefaultDays int
	// MaxDays is the longest duration that an event can keep. A duration
	// stored above it is cut at deletion time. If MaxDays is below
	// DefaultDays, DefaultDays is the limit.
	MaxDays int
}

// NewRetention builds the policy from the default number of days (0: events
// are kept forever) and the maximum (0: not set).
//
// When the maximum is not set, it is DefaultMaxRetentionDays, or the default
// number of days if that is longer. So a server that already runs with a long
// default still starts after an upgrade. The two values are refused only when
// they conflict, or when one is negative or above CeilingRetentionDays.
func NewRetention(days, maxDays int) (Retention, error) {
	switch {
	case days < 0 || days > CeilingRetentionDays:
		return Retention{}, fmt.Errorf("retention of %d days: use 0 (keep events forever) up to %d days", days, CeilingRetentionDays)
	case maxDays < 0 || maxDays > CeilingRetentionDays:
		return Retention{}, fmt.Errorf("maximum retention of %d days: use 0 (the default maximum) up to %d days", maxDays, CeilingRetentionDays)
	case maxDays == 0:
		maxDays = max(DefaultMaxRetentionDays, days)
	case days > maxDays:
		return Retention{}, fmt.Errorf("retention of %d days is above the maximum of %d days: raise BENEVOLE_RETENTION_MAX_DAYS (-retention-max-days) or lower BENEVOLE_RETENTION_DAYS (-retention-days)", days, maxDays)
	}
	return Retention{DefaultDays: days, MaxDays: maxDays}, nil
}

// Enabled reports whether events are deleted automatically.
func (r Retention) Enabled() bool { return r.DefaultDays > 0 }

// Max returns the longest duration that an organiser can choose. It is never
// below DefaultDays.
func (r Retention) Max() int {
	if r.MaxDays < r.DefaultDays {
		return r.DefaultDays
	}
	return r.MaxDays
}

// Days returns the number of days that an event is kept, given the days it
// stores (0: none). It returns 0 when automatic deletion is off.
func (r Retention) Days(eventDays int) int {
	if !r.Enabled() {
		return 0
	}
	if eventDays <= 0 {
		eventDays = r.DefaultDays
	}
	return min(eventDays, r.Max())
}
