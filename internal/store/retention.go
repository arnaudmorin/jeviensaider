package store

// Retention is the deletion policy of the server.
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

// Enabled reports whether events are deleted automatically.
func (r Retention) Enabled() bool { return r.DefaultDays > 0 }

// Max returns the longest duration that an organiser can choose. It is never
// below DefaultDays, so a zero MaxDays means "the default and no more".
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
