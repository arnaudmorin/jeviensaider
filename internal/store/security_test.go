package store

import (
	"errors"
	"testing"
	"time"

	"benevole-ape/internal/model"
)

// A browser that remembers Mallory (her personal link was shared) must not
// turn Alice's sign-up into an update of Mallory's record.
func TestSignupWithSomeoneElsesToken(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 0)

	m, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Mallory", Phone: "0700000000"})
	if err != nil {
		t.Fatal(err)
	}
	tok := m.Volunteer.EditToken

	a, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[1].ID, EditToken: tok, Name: "Alice Victim", Phone: "0601020304"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.NewVolunteer || a.Volunteer.ID == m.Volunteer.ID || a.Volunteer.EditToken == tok {
		t.Fatalf("expected a new volunteer for Alice, got %+v", a)
	}
	mal, err := s.VolunteerByToken(ctx, ev.ID, tok)
	if err != nil || mal.Name != "Mallory" || mal.Phone != "0700000000" {
		t.Fatalf("Mallory's record changed: %+v %v", mal, err)
	}

	// The same person (same name, other case and spacing) only updates
	// their own details.
	b, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[2].ID, EditToken: a.Volunteer.EditToken, Name: "  alice   VICTIM ", Phone: "0611111111"})
	if err != nil {
		t.Fatal(err)
	}
	if b.NewVolunteer || b.Volunteer.ID != a.Volunteer.ID || b.Volunteer.Phone != "0611111111" {
		t.Fatalf("expected Alice's record to be reused and updated, got %+v", b)
	}
}

func TestEventLimits(t *testing.T) {
	s := openTest(t)
	ev, st, _ := fixture(t, s, 0) // 1 stand, 3 slots

	for i := 1; i < MaxStands; i++ {
		if err := s.CreateStand(ctx, &model.Stand{EventID: ev.ID, Name: "Stand"}); err != nil {
			t.Fatalf("stand %d: %v", i, err)
		}
	}
	if err := s.CreateStand(ctx, &model.Stand{EventID: ev.ID, Name: "One too many"}); !errors.Is(err, ErrTooManyStands) {
		t.Fatalf("want ErrTooManyStands, got %v", err)
	}

	slot := func(i int) model.Slot {
		start := at("2026-06-21 00:00").Add(time.Duration(i) * time.Minute)
		return model.Slot{StartsAt: start, EndsAt: start.Add(time.Minute), MinPeople: 1}
	}
	var batch []model.Slot
	for i := 0; i < MaxSlots-3; i++ {
		batch = append(batch, slot(i))
	}
	if err := s.CreateSlots(ctx, ev.ID, st.ID, batch); err != nil {
		t.Fatalf("filling up to the limit: %v", err)
	}
	if err := s.CreateSlots(ctx, ev.ID, st.ID, []model.Slot{slot(9999)}); !errors.Is(err, ErrTooManySlots) {
		t.Fatalf("want ErrTooManySlots, got %v", err)
	}
	// A refused batch adds nothing.
	all, _ := s.ListSlots(ctx, ev.ID)
	if len(all) != MaxSlots {
		t.Fatalf("slots: %d, want %d", len(all), MaxSlots)
	}
}

func TestRenewVolunteerToken(t *testing.T) {
	s := openTest(t)
	ev, _, slots := fixture(t, s, 0)
	res, err := s.SignUp(ctx, SignupRequest{EventID: ev.ID, SlotID: slots[0].ID, Name: "Marie"})
	if err != nil {
		t.Fatal(err)
	}
	v := res.Volunteer
	old := v.EditToken
	if err := s.RenewVolunteerToken(ctx, v); err != nil || v.EditToken == old || len(v.EditToken) < 40 {
		t.Fatalf("renew: %v, token %q", err, v.EditToken)
	}
	if _, err := s.VolunteerByToken(ctx, ev.ID, old); !errors.Is(err, ErrNotFound) {
		t.Errorf("old token still works: %v", err)
	}
	got, err := s.VolunteerByToken(ctx, ev.ID, v.EditToken)
	if err != nil || got.ID != v.ID {
		t.Errorf("new token: %+v %v", got, err)
	}
	v.ID = 999999
	if err := s.RenewVolunteerToken(ctx, v); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown volunteer: %v", err)
	}
}
