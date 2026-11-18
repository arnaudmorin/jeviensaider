package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"benevole-ape/internal/model"
	"benevole-ape/internal/slotgen"
	"benevole-ape/internal/store"
)

func (s *Server) withAdmin(h func(http.ResponseWriter, *http.Request, *model.Event)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ev, err := s.store.EventByAdminToken(r.Context(), r.PathValue("token"))
		if errors.Is(err, store.ErrNotFound) {
			s.renderError(w, r, http.StatusNotFound, "Ce lien d'administration n'est pas valide.")
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		h(w, r, ev)
	}
}

func adminURL(ev *model.Event) string { return "/a/" + ev.AdminToken }

// backToStand redirects to the admin page, scrolled to a stand.
func backToStand(w http.ResponseWriter, r *http.Request, ev *model.Event, standID int64) {
	u := adminURL(ev)
	if standID != 0 {
		u += "#stand-" + strconv.FormatInt(standID, 10)
	}
	redirect(w, r, u)
}

func adminFail(w http.ResponseWriter, r *http.Request, ev *model.Event, standID int64, msg string) {
	setFlash(w, "error", msg)
	backToStand(w, r, ev, standID)
}

// genDefaults pre-fills a stand's slot generator.
type genDefaults struct {
	Date string
	From string
	To   string
}

type adminPage struct {
	Common
	Event      *model.Event
	P          *model.Planning
	PublicURL  string
	AdminURL   string
	AdminPath  string
	EventURL   string
	Volunteers int
	Gen        map[int64]genDefaults
	NextColor  string
	Statuses   []string
	ExpiresOn  string // "YYYY-MM-DD", empty if events are kept forever
	Retention  int
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	p, err := s.planning(r.Context(), ev, 0)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	nvol, err := s.store.CountVolunteers(r.Context(), ev.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var expires string
	if s.cfg.RetentionDays > 0 {
		last, err := s.store.EventLastDay(r.Context(), ev.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		expires = store.ExpiresOn(last, s.cfg.RetentionDays).Format(model.DateLayout)
	}
	date := ev.Date
	if date == "" {
		date = time.Now().Format(model.DateLayout)
	}
	gen := map[int64]genDefaults{}
	for _, st := range p.Stands {
		d := genDefaults{Date: date, From: "14:00", To: "17:00"}
		if n := len(st.Slots); n > 0 {
			last := st.Slots[n-1]
			d.Date = last.EndsAt.Format(model.DateLayout)
			d.From = last.EndsAt.Format("15:04")
			d.To = last.EndsAt.Add(last.EndsAt.Sub(last.StartsAt)).Format("15:04")
			if d.To <= d.From { // would cross midnight
				d.To = "23:59"
			}
		}
		gen[st.ID] = d
	}
	s.render(w, r, http.StatusOK, "admin", adminPage{
		Common:     s.common(w, r, "Admin · "+ev.Title),
		Event:      ev,
		P:          p,
		PublicURL:  s.absURL(r, eventURL(ev)),
		AdminURL:   s.absURL(r, adminURL(ev)),
		AdminPath:  adminURL(ev),
		EventURL:   eventURL(ev),
		Volunteers: nvol,
		Gen:        gen,
		NextColor:  palette[len(p.Stands)%len(palette)],
		Statuses:   []string{model.StatusDraft, model.StatusOpen, model.StatusClosed},
		ExpiresOn:  expires,
		Retention:  s.cfg.RetentionDays,
	})
}

func (s *Server) adminUpdateEvent(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	title := clean(r.FormValue("title"), 120, false)
	if title == "" {
		adminFail(w, r, ev, 0, "Le nom de l'événement est obligatoire.")
		return
	}
	date, err := s.parseDate(r.FormValue("date"))
	if err != nil {
		adminFail(w, r, ev, 0, err.Error())
		return
	}
	email := clean(r.FormValue("admin_email"), 120, false)
	if err := checkAdminEmail(email); err != nil {
		adminFail(w, r, ev, 0, err.Error())
		return
	}
	ev.Title, ev.Date, ev.AdminEmail = title, date, email
	ev.NotifySignups = r.FormValue("notify") == "1"
	ev.Location = clean(r.FormValue("location"), 120, false)
	ev.Description = clean(r.FormValue("description"), 2000, true)
	if s.storeError(w, r, s.store.UpdateEvent(r.Context(), ev)) {
		return
	}
	setFlash(w, "success", "Événement mis à jour.")
	redirect(w, r, adminURL(ev))
}

func (s *Server) adminSetStatus(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	st := r.FormValue("status")
	if st != model.StatusDraft && st != model.StatusOpen && st != model.StatusClosed {
		adminFail(w, r, ev, 0, "Statut inconnu.")
		return
	}
	ev.Status = st
	if s.storeError(w, r, s.store.UpdateEvent(r.Context(), ev)) {
		return
	}
	setFlash(w, "success", "Statut : "+eventStatusLabel(st)+".")
	redirect(w, r, adminURL(ev))
}

func readStand(r *http.Request, ev *model.Event) (*model.Stand, error) {
	st := &model.Stand{
		EventID:     ev.ID,
		Name:        clean(r.FormValue("name"), 80, false),
		Description: clean(r.FormValue("description"), 1000, true),
		Color:       r.FormValue("color"),
	}
	if st.Name == "" {
		return nil, errors.New("Le nom du stand est obligatoire.")
	}
	if !validColor(st.Color) {
		st.Color = defaultColor
	}
	return st, nil
}

func (s *Server) adminCreateStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	st, err := readStand(r, ev)
	if err != nil {
		adminFail(w, r, ev, 0, err.Error())
		return
	}
	err = s.store.CreateStand(r.Context(), st)
	if errors.Is(err, store.ErrTooManyStands) {
		adminFail(w, r, ev, 0, capitalize(err.Error())+".")
		return
	}
	if s.storeError(w, r, err) {
		return
	}
	setFlash(w, "success", fmt.Sprintf("Stand « %s » ajouté. Ajoutez maintenant ses créneaux.", st.Name))
	backToStand(w, r, ev, st.ID)
}

func (s *Server) adminUpdateStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, _ := pathID(r, "id")
	st, err := readStand(r, ev)
	if err != nil {
		adminFail(w, r, ev, id, err.Error())
		return
	}
	st.ID = id
	if s.storeError(w, r, s.store.UpdateStand(r.Context(), st)) {
		return
	}
	setFlash(w, "success", "Stand mis à jour.")
	backToStand(w, r, ev, id)
}

func (s *Server) adminDeleteStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, _ := pathID(r, "id")
	if s.storeError(w, r, s.store.DeleteStand(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Stand supprimé (avec ses créneaux et inscriptions).")
	redirect(w, r, adminURL(ev))
}

func (s *Server) adminMoveStand(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, _ := pathID(r, "id")
	delta := 1
	if r.FormValue("dir") == "up" {
		delta = -1
	}
	if s.storeError(w, r, s.store.MoveStand(r.Context(), ev.ID, id, delta)) {
		return
	}
	backToStand(w, r, ev, id)
}

func (s *Server) adminGenerateSlots(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	standID, _ := pathID(r, "id")
	fail := func(msg string) { adminFail(w, r, ev, standID, msg) }

	start, err := s.parseDateTime(r.FormValue("date"), r.FormValue("from"))
	if err != nil {
		fail("Début : " + err.Error())
		return
	}
	end, err := s.parseDateTime(r.FormValue("date"), r.FormValue("to"))
	if err != nil {
		fail("Fin : " + err.Error())
		return
	}
	step, err := formInt(r, "step", 0)
	if err != nil {
		fail("Durée : " + err.Error())
		return
	}
	minP, err := formInt(r, "min", 1)
	if err != nil {
		fail("Minimum : " + err.Error())
		return
	}
	maxP, err := formInt(r, "max", 0)
	if err != nil {
		fail("Maximum : " + err.Error())
		return
	}
	slots, err := slotgen.Generate(standID, start, end, time.Duration(step)*time.Minute, minP, maxP)
	if err != nil {
		fail(capitalize(err.Error()) + ".")
		return
	}
	err = s.store.CreateSlots(r.Context(), ev.ID, standID, slots)
	if errors.Is(err, store.ErrTooManySlots) {
		fail(capitalize(err.Error()) + ".")
		return
	}
	if s.storeError(w, r, err) {
		return
	}
	setFlash(w, "success", fmt.Sprintf("%d créneau(x) ajouté(s).", len(slots)))
	backToStand(w, r, ev, standID)
}

func (s *Server) adminUpdateSlot(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, _ := pathID(r, "id")
	sl, err := s.store.GetSlot(r.Context(), ev.ID, id)
	if s.storeError(w, r, err) {
		return
	}
	fail := func(msg string) { adminFail(w, r, ev, sl.StandID, msg) }
	date := r.FormValue("date")
	if date == "" {
		date = sl.StartsAt.Format(model.DateLayout)
	}
	if sl.StartsAt, err = s.parseDateTime(date, r.FormValue("from")); err != nil {
		fail("Début : " + err.Error())
		return
	}
	if sl.EndsAt, err = s.parseDateTime(date, r.FormValue("to")); err != nil {
		fail("Fin : " + err.Error())
		return
	}
	if !sl.EndsAt.After(sl.StartsAt) {
		fail("L'heure de fin doit être après l'heure de début.")
		return
	}
	if sl.MinPeople, err = formInt(r, "min", 1); err != nil || sl.MinPeople < 1 {
		fail("Le minimum doit être au moins 1.")
		return
	}
	if sl.MaxPeople, err = formInt(r, "max", 0); err != nil || (sl.MaxPeople != 0 && sl.MaxPeople < sl.MinPeople) {
		fail("Le maximum doit être vide ou supérieur ou égal au minimum.")
		return
	}
	if s.storeError(w, r, s.store.UpdateSlot(r.Context(), ev.ID, sl)) {
		return
	}
	setFlash(w, "success", "Créneau mis à jour.")
	backToStand(w, r, ev, sl.StandID)
}

func (s *Server) adminDeleteSlot(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, _ := pathID(r, "id")
	sl, err := s.store.GetSlot(r.Context(), ev.ID, id)
	if s.storeError(w, r, err) {
		return
	}
	if s.storeError(w, r, s.store.DeleteSlot(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Créneau supprimé.")
	backToStand(w, r, ev, sl.StandID)
}

func (s *Server) adminAddSignup(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	slotID, err := strconv.ParseInt(r.FormValue("slot"), 10, 64)
	if err != nil {
		adminFail(w, r, ev, 0, "Choisissez un créneau.")
		return
	}
	form := readVolunteerForm(r)
	if err := form.validate(); err != nil {
		adminFail(w, r, ev, 0, err.Error())
		return
	}
	_, err = s.store.SignUp(r.Context(), store.SignupRequest{
		EventID: ev.ID, SlotID: slotID, Name: form.Name, Email: form.Email, Phone: form.Phone,
		Comment: form.Comment, Admin: true,
	})
	if errors.Is(err, store.ErrSlotFull) || errors.Is(err, store.ErrNotFound) {
		adminFail(w, r, ev, 0, capitalize(err.Error())+".")
		return
	}
	if s.storeError(w, r, err) {
		return
	}
	sl, _ := s.store.GetSlot(r.Context(), ev.ID, slotID)
	setFlash(w, "success", form.Name+" a été inscrit·e.")
	backToStand(w, r, ev, sl.StandID)
}

func (s *Server) adminDeleteSignup(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	id, _ := pathID(r, "id")
	if s.storeError(w, r, s.store.AdminDeleteSignup(r.Context(), ev.ID, id)) {
		return
	}
	setFlash(w, "info", "Inscription retirée.")
	backToStand(w, r, ev, 0)
}

func (s *Server) adminPurge(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	if r.FormValue("confirm") != "yes" {
		adminFail(w, r, ev, 0, "Cochez la case de confirmation pour effacer les données.")
		return
	}
	n, err := s.store.PurgeVolunteers(r.Context(), ev.ID)
	if s.storeError(w, r, err) {
		return
	}
	setFlash(w, "success", fmt.Sprintf("Données personnelles effacées (%d bénévole(s)).", n))
	redirect(w, r, adminURL(ev))
}

func (s *Server) adminDuplicate(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	title := clean(r.FormValue("title"), 120, false)
	if title == "" {
		adminFail(w, r, ev, 0, "Donnez un nom au nouvel événement.")
		return
	}
	date, err := s.parseDate(r.FormValue("date"))
	if err != nil {
		adminFail(w, r, ev, 0, err.Error())
		return
	}
	dup, err := s.store.DuplicateEvent(r.Context(), ev, title, date)
	if s.storeError(w, r, err) {
		return
	}
	s.notifyCreated(r, dup)
	setFlash(w, "success", "Nouvel événement créé à partir de « "+ev.Title+" ». Voici son lien d'administration : conservez-le !")
	redirect(w, r, adminURL(dup))
}

func (s *Server) adminDeleteEvent(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	if r.FormValue("confirm") != "yes" {
		adminFail(w, r, ev, 0, "Cochez la case de confirmation pour supprimer l'événement.")
		return
	}
	if s.storeError(w, r, s.store.DeleteEvent(r.Context(), ev.ID)) {
		return
	}
	setFlash(w, "info", "L'événement « "+ev.Title+" » a été supprimé.")
	redirect(w, r, "/")
}

// ---- print view ----

type printPage struct {
	Event *model.Event
	P     *model.Planning
}

func (s *Server) adminPrint(w http.ResponseWriter, r *http.Request, ev *model.Event) {
	p, err := s.planning(r.Context(), ev, 0)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderTemplate(w, r, http.StatusOK, "print", "print", printPage{Event: ev, P: p})
}
