package web

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"benevole-ape/internal/model"
	"benevole-ape/internal/store"
)

// ---- home / event creation ----

type homePage struct {
	Common
	NeedPassword bool
	Error        string
	Form         model.Event
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home", homePage{
		Common:       s.common(w, r, "Bénévoles APE"),
		NeedPassword: s.cfg.CreatePassword != "",
	})
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	ev := model.Event{
		Title:       clean(r.FormValue("title"), 120, false),
		Location:    clean(r.FormValue("location"), 120, false),
		Description: clean(r.FormValue("description"), 2000, true),
	}
	date, dateErr := parseDate(r.FormValue("date"))
	ev.Date = date

	fail := func(msg string) {
		s.render(w, r, http.StatusBadRequest, "home", homePage{
			Common: Common{Title: "Bénévoles APE"}, NeedPassword: s.cfg.CreatePassword != "", Error: msg, Form: ev,
		})
	}
	if s.cfg.CreatePassword != "" &&
		subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(s.cfg.CreatePassword)) != 1 {
		fail("Mot de passe de création incorrect.")
		return
	}
	if ev.Title == "" {
		fail("Le nom de l'événement est obligatoire.")
		return
	}
	if dateErr != nil {
		fail(dateErr.Error())
		return
	}
	if err := s.store.CreateEvent(r.Context(), &ev); err != nil {
		s.serverError(w, r, err)
		return
	}
	setFlash(w, "success", "Événement créé ! Ce lien d'administration est votre seule clé pour le gérer : ajoutez-le à vos favoris et ne le partagez qu'avec les organisateurs.")
	redirect(w, r, "/a/"+ev.AdminToken)
}

// ---- event loading helpers ----

func (s *Server) withEvent(h func(http.ResponseWriter, *http.Request, *model.Event)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ev, err := s.store.EventBySlug(r.Context(), r.PathValue("slug"))
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Cet événement n'existe pas (ou plus). Vérifiez le lien.")
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		h(w, r, ev)
	}
}

func (s *Server) withVolunteer(h func(http.ResponseWriter, *http.Request, *model.Event, *model.Volunteer)) http.HandlerFunc {
	return s.withEvent(func(w http.ResponseWriter, r *http.Request, ev *model.Event) {
		w.Header().Set("Cache-Control", "no-store")
		vol, err := s.store.VolunteerByToken(r.Context(), ev.ID, r.PathValue("token"))
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Ce lien personnel n'est pas (ou plus) valide.")
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		h(w, r, ev, vol)
	})
}

func eventURL(ev *model.Event) string { return "/e/" + ev.Slug }

func meURL(ev *model.Event, v *model.Volunteer) string {
	return eventURL(ev) + "/me/" + v.EditToken
}

func volunteerCookie(ev *model.Event) string { return "v" + strconv.FormatInt(ev.ID, 10) }

func setVolunteerCookie(w http.ResponseWriter, ev *model.Event, v *model.Volunteer) {
	http.SetCookie(w, &http.Cookie{
		Name: volunteerCookie(ev), Value: v.EditToken, Path: eventURL(ev),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 365 * 24 * 3600,
	})
}

// cookieVolunteer returns the volunteer remembered by this browser, if any.
func (s *Server) cookieVolunteer(r *http.Request, ev *model.Event) *model.Volunteer {
	c, err := r.Cookie(volunteerCookie(ev))
	if err != nil {
		return nil
	}
	v, err := s.store.VolunteerByToken(r.Context(), ev.ID, c.Value)
	if err != nil {
		return nil
	}
	return v
}

func (s *Server) planning(ctx context.Context, ev *model.Event, myID int64) (*model.Planning, error) {
	stands, err := s.store.ListStands(ctx, ev.ID)
	if err != nil {
		return nil, err
	}
	slots, err := s.store.ListSlots(ctx, ev.ID)
	if err != nil {
		return nil, err
	}
	signups, err := s.store.ListSignups(ctx, ev.ID)
	if err != nil {
		return nil, err
	}
	return model.BuildPlanning(stands, slots, signups, myID), nil
}

// ---- event page (overview grid / stand tabs / my slots) ----

type eventPage struct {
	Common
	Event    *model.Event
	P        *model.Planning
	Tab      string // grid, stand, me
	Stand    *model.StandView
	TodoOnly bool
	EventURL string
	Me       *model.Volunteer
	MeURL    string
	MeAbsURL string
	Welcome  bool
}

func (s *Server) eventPage(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	s.showEvent(w, r, ev, s.cookieVolunteer(r, ev), false)
}

func (s *Server) mePage(w http.ResponseWriter, r *http.Request, ev *model.Event, vol *model.Volunteer) {
	setVolunteerCookie(w, ev, vol)
	s.showEvent(w, r, ev, vol, true)
}

func (s *Server) showEvent(w http.ResponseWriter, r *http.Request, ev *model.Event, me *model.Volunteer, meTab bool) {
	var myID int64
	if me != nil {
		myID = me.ID
	}
	p, err := s.planning(r.Context(), ev, myID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := eventPage{
		Common:   s.common(w, r, ev.Title),
		Event:    ev,
		P:        p,
		Tab:      "grid",
		TodoOnly: r.URL.Query().Get("todo") == "1",
		EventURL: eventURL(ev),
		Me:       me,
	}
	if me != nil {
		data.MeURL = meURL(ev, me)
		data.MeAbsURL = s.absURL(r, data.MeURL)
		data.Welcome = r.URL.Query().Get("welcome") == "1"
	}
	if meTab {
		data.Tab = "me"
	} else if id, err := strconv.ParseInt(r.URL.Query().Get("stand"), 10, 64); err == nil {
		if st := p.Stand(id); st != nil {
			data.Tab, data.Stand = "stand", st
		}
	}
	s.render(w, r, http.StatusOK, "event", data)
}

// ---- sign-up ----

type signupPage struct {
	Common
	Event    *model.Event
	EventURL string
	Slot     *model.SlotView
	Me       *model.Volunteer
	Form     volunteerForm
	Error    string
}

func (s *Server) loadSlotView(ctx context.Context, ev *model.Event, slotID, myID int64) (*model.SlotView, error) {
	p, err := s.planning(ctx, ev, myID)
	if err != nil {
		return nil, err
	}
	for _, st := range p.Stands {
		for _, sl := range st.Slots {
			if sl.ID == slotID {
				return sl, nil
			}
		}
	}
	return nil, store.ErrNotFound
}

func (s *Server) signupForm(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := pathID(r, "id")
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "Créneau introuvable.")
		return
	}
	me := s.cookieVolunteer(r, ev)
	var myID int64
	data := signupPage{Common: s.common(w, r, "Inscription"), Event: ev, EventURL: eventURL(ev), Me: me}
	if me != nil {
		myID = me.ID
		data.Form = volunteerForm{Name: me.Name, Email: me.Email, Phone: me.Phone}
	}
	sv, err := s.loadSlotView(r.Context(), ev, id, myID)
	if s.storeError(w, r, err) {
		return
	}
	data.Slot = sv
	s.renderSignup(w, r, http.StatusOK, data)
}

func (s *Server) renderSignup(w http.ResponseWriter, r *http.Request, status int, data signupPage) {
	if isHTMX(r) {
		s.renderTemplate(w, r, status, "signup", "signup_dialog", data)
		return
	}
	s.render(w, r, status, "signup", data)
}

func (s *Server) signupSubmit(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, ok := pathID(r, "id")
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "Créneau introuvable.")
		return
	}
	// Honeypot: bots fill every field, humans never see this one.
	if r.FormValue("website") != "" {
		redirect(w, r, eventURL(ev))
		return
	}
	me := s.cookieVolunteer(r, ev)
	form := readVolunteerForm(r)

	fail := func(status int, msg string) {
		var myID int64
		if me != nil {
			myID = me.ID
		}
		sv, err := s.loadSlotView(r.Context(), ev, id, myID)
		if s.storeError(w, r, err) {
			return
		}
		// A failed POST is always rendered as a full page (the form is
		// submitted without htmx).
		r.Header.Del("HX-Request")
		s.renderSignup(w, r, status, signupPage{
			Common: Common{Title: "Inscription"}, Event: ev, EventURL: eventURL(ev), Slot: sv, Me: me, Form: form, Error: msg,
		})
	}
	if err := form.validate(true); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	req := store.SignupRequest{
		EventID: ev.ID, SlotID: id, Name: form.Name, Email: form.Email, Phone: form.Phone, Comment: form.Comment,
	}
	if me != nil {
		req.EditToken = me.EditToken
	}
	res, err := s.store.SignUp(r.Context(), req)
	switch {
	case errors.Is(err, store.ErrSlotFull), errors.Is(err, store.ErrAlreadySigned), errors.Is(err, store.ErrEventNotOpen):
		fail(http.StatusConflict, capitalize(err.Error())+".")
		return
	case s.storeError(w, r, err):
		return
	}

	setVolunteerCookie(w, ev, res.Volunteer)
	slot, _ := s.store.GetSlot(r.Context(), ev.ID, id)
	if res.NewVolunteer {
		redirect(w, r, meURL(ev, res.Volunteer)+"?welcome=1")
		return
	}
	msg := fmt.Sprintf("C'est noté ! Vous êtes inscrit·e de %s à %s. Merci !", hm(slot.StartsAt), hm(slot.EndsAt))
	kind := "success"
	if len(res.Overlapping) > 0 {
		kind = "warning"
		msg += fmt.Sprintf(" Attention : ce créneau chevauche un autre de vos créneaux (%s – %s).",
			hm(res.Overlapping[0].StartsAt), hm(res.Overlapping[0].EndsAt))
	}
	setFlash(w, kind, msg)
	redirect(w, r, tabURL(eventURL(ev), slot.StandID, false))
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// ---- volunteer self-service ----

func (s *Server) meUpdate(w http.ResponseWriter, r *http.Request, ev *model.Event, vol *model.Volunteer) {
	form := readVolunteerForm(r)
	if err := form.validate(true); err != nil {
		setFlash(w, "error", err.Error())
		redirect(w, r, meURL(ev, vol))
		return
	}
	vol.Name, vol.Email, vol.Phone = form.Name, form.Email, form.Phone
	if s.storeError(w, r, s.store.UpdateVolunteer(r.Context(), vol)) {
		return
	}
	setFlash(w, "success", "Vos coordonnées ont été mises à jour.")
	redirect(w, r, meURL(ev, vol))
}

func (s *Server) meCancel(w http.ResponseWriter, r *http.Request, ev *model.Event, vol *model.Volunteer) {
	id, ok := pathID(r, "id")
	if !ok {
		s.renderError(w, r, http.StatusNotFound, "Inscription introuvable.")
		return
	}
	if !ev.IsOpen() {
		setFlash(w, "error", "Les inscriptions sont closes : contactez l'APE pour toute modification.")
		redirect(w, r, meURL(ev, vol))
		return
	}
	if s.storeError(w, r, s.store.CancelSignup(r.Context(), vol.ID, id)) {
		return
	}
	setFlash(w, "info", "Votre inscription a été annulée.")
	redirect(w, r, meURL(ev, vol))
}

// forget clears the volunteer cookie ("ce n'est pas moi" on a shared device).
func (s *Server) forget(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	http.SetCookie(w, &http.Cookie{Name: volunteerCookie(ev), Path: eventURL(ev), MaxAge: -1})
	setFlash(w, "info", "Cet appareil ne vous reconnaît plus. Votre lien personnel reste valable.")
	redirect(w, r, eventURL(ev))
}
