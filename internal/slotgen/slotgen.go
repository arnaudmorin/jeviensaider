// Package slotgen splits a time range into consecutive volunteer slots.
package slotgen

import (
	"errors"
	"time"

	"benevole-ape/internal/model"
)

var (
	ErrRange   = errors.New("l'heure de fin doit être après l'heure de début")
	ErrMin     = errors.New("le nombre minimum de personnes doit être au moins 1")
	ErrMax     = errors.New("le maximum doit être supérieur ou égal au minimum")
	ErrTooMany = errors.New("trop de créneaux générés (maximum 96)")
)

// MaxSlots bounds a single generation, to catch typos like a 1-minute step.
const MaxSlots = 96

// Generate splits [start, end) into slots of length step. A step of 0 (or one
// longer than the range) yields a single slot. If the range is not a multiple
// of step, the last slot is shorter. max may be 0 (no maximum).
func Generate(standID int64, start, end time.Time, step time.Duration, min, max int) ([]model.Slot, error) {
	if !end.After(start) {
		return nil, ErrRange
	}
	if min < 1 {
		return nil, ErrMin
	}
	if max != 0 && max < min {
		return nil, ErrMax
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
