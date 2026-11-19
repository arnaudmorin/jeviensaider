package slotgen

import (
	"testing"
	"time"

	"jeviensaider/internal/model"
)

func at(s string) time.Time {
	t, err := time.Parse(model.TimeLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestGenerateHourly(t *testing.T) {
	slots, err := Generate(7, at("2026-06-20 14:00"), at("2026-06-20 17:00"), time.Hour, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 3 {
		t.Fatalf("want 3 slots, got %d", len(slots))
	}
	total := 0
	for i, s := range slots {
		if s.StandID != 7 || s.MinPeople != 2 || s.EndsAt.Sub(s.StartsAt) != time.Hour {
			t.Errorf("slot %d unexpected: %+v", i, s)
		}
		total += s.MinPeople
	}
	if total != 6 {
		t.Errorf("want 6 people needed, got %d", total)
	}
	if !slots[2].EndsAt.Equal(at("2026-06-20 17:00")) {
		t.Errorf("last slot ends at %v", slots[2].EndsAt)
	}
}

func TestGenerateShortLastSlot(t *testing.T) {
	slots, err := Generate(1, at("2026-06-20 14:00"), at("2026-06-20 16:30"), time.Hour, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 3 || slots[2].EndsAt.Sub(slots[2].StartsAt) != 30*time.Minute {
		t.Fatalf("unexpected slots: %+v", slots)
	}
	if slots[0].MaxPeople != 3 {
		t.Errorf("max not propagated")
	}
}

func TestGenerateSingle(t *testing.T) {
	slots, err := Generate(1, at("2026-06-20 08:00"), at("2026-06-20 09:30"), 0, 4, 0)
	if err != nil || len(slots) != 1 {
		t.Fatalf("want one slot, got %v %v", slots, err)
	}
}

func TestGenerateErrors(t *testing.T) {
	s, e := at("2026-06-20 14:00"), at("2026-06-20 17:00")
	cases := []struct {
		name     string
		start    time.Time
		end      time.Time
		step     time.Duration
		min, max int
		want     error
	}{
		{"reversed", e, s, time.Hour, 1, 0, model.ErrSlotRange},
		{"empty", s, s, time.Hour, 1, 0, model.ErrSlotRange},
		{"min0", s, e, time.Hour, 0, 0, model.ErrSlotMin},
		{"max<min", s, e, time.Hour, 3, 2, model.ErrSlotMax},
		{"too many", s, e, time.Minute, 1, 0, ErrTooMany},
	}
	for _, c := range cases {
		if _, err := Generate(1, c.start, c.end, c.step, c.min, c.max); err != c.want {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}
