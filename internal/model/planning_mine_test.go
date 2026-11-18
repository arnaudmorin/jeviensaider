package model

import (
	"testing"
	"time"
)

func TestMinePlanning(t *testing.T) {
	at := func(s string) time.Time { v, _ := time.Parse(TimeLayout, "2026-06-20 "+s); return v }
	stands := []Stand{{ID: 1, Name: "Pêche"}, {ID: 2, Name: "Buvette"}, {ID: 3, Name: "Tombola"}}
	slots := []Slot{
		{ID: 10, StandID: 1, StartsAt: at("14:00"), EndsAt: at("15:00"), MinPeople: 2},
		{ID: 11, StandID: 1, StartsAt: at("15:00"), EndsAt: at("16:00"), MinPeople: 2},
		{ID: 20, StandID: 2, StartsAt: at("15:00"), EndsAt: at("16:00"), MinPeople: 2},
		{ID: 21, StandID: 2, StartsAt: at("16:00"), EndsAt: at("17:30"), MinPeople: 2},
		{ID: 30, StandID: 3, StartsAt: at("14:00"), EndsAt: at("15:00"), MinPeople: 1},
	}
	const me, other = 7, 8
	signups := []SignupRow{
		{Signup: Signup{SlotID: 10, VolunteerID: me}},
		{Signup: Signup{SlotID: 11, VolunteerID: me}},
		{Signup: Signup{SlotID: 20, VolunteerID: me}},
		{Signup: Signup{SlotID: 30, VolunteerID: other}},
	}
	p := BuildPlanning(stands, slots, signups, me)

	clash := map[int64]bool{}
	for _, v := range p.MySlots {
		clash[v.ID] = v.Clash
	}
	if clash[10] || !clash[11] || !clash[20] {
		t.Errorf("clash flags: %v", clash)
	}
	if got := p.MyDuration(); got != 3*time.Hour {
		t.Errorf("MyDuration = %v", got)
	}

	m := p.Mine()
	if len(m.Stands) != 2 || m.Stands[0].ID != 1 || m.Stands[1].ID != 2 {
		t.Fatalf("Mine stands: %+v", m.Stands)
	}
	if len(m.Columns) != 3 || len(m.MySlots) != 3 { // 14h, 15h, 16h-17h30 (Tombola left out)
		t.Errorf("Mine: %d columns, %d slots", len(m.Columns), len(m.MySlots))
	}

	// Slot 21 (Buvette 16h) overlaps none of mine; Tombola 14h overlaps slot 10.
	var s21, s30 *SlotView
	for _, st := range p.Stands {
		for _, v := range st.Slots {
			switch v.ID {
			case 21:
				s21 = v
			case 30:
				s30 = v
			}
		}
	}
	if o := p.MyOverlaps(s21); len(o) != 0 {
		t.Errorf("MyOverlaps(21) = %d", len(o))
	}
	if o := p.MyOverlaps(s30); len(o) != 1 || o[0].ID != 10 {
		t.Errorf("MyOverlaps(30) = %+v", o)
	}
}
