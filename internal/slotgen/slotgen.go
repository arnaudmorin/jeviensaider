// Package slotgen splits a time range into consecutive volunteer slots.
package slotgen

import (
	"errors"
	"time"

	"benevole-ape/internal/model"
)

// ErrTooMany is returned when a generation would exceed MaxSlots.
var ErrTooMany = errors.New("too many slots generated")

// MaxSlots bounds a single generation, to catch typos like a 1-minute step.
const MaxSlots = 96

// Generate splits [start, end) into slots of length step. A step of 0 (or one
// longer than the range) yields a single slot. If the range is not a multiple
// of step, the last slot is shorter. max may be 0 (no maximum). Invalid
// times or capacities return the error of model.Slot.Check.
func Generate(standID int64, start, end time.Time, step time.Duration, min, max int) ([]model.Slot, error) {
	if err := (model.Slot{StartsAt: start, EndsAt: end, MinPeople: min, MaxPeople: max}).Check(); err != nil {
		return nil, err
	}
	if step <= 0 {
		step = end.Sub(start)
	}
	var slots []model.Slot
	for t := start; t.Before(end); t = t.Add(step) {
		e := t.Add(step)
		if e.After(end) {
			e = end
		}
		slots = append(slots, model.Slot{StandID: standID, StartsAt: t, EndsAt: e, MinPeople: min, MaxPeople: max})
		if len(slots) > MaxSlots {
			return nil, ErrTooMany
		}
	}
	return slots, nil
}
