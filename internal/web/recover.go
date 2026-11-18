package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"benevole-ape/internal/mail"
)

// maxRecoverEvents is how many events one recovery mail lists. A private
// instance that keeps its events forever could have many more.
const maxRecoverEvents = 20

// msgRecoverSent is the answer to every request, whether or not the address
// belongs to an organiser. A different answer would tell a visitor which
// addresses are organisers.
const msgRecoverSent = "Si cette adresse est celle d'un organisateur, un e-mail avec ses liens d'administration vient d'être envoyé. Pensez à regarder vos courriers indésirables et les e-mails reçus plus tôt aujourd'hui."

type recoverPage struct {
	Common
	Error string
	Email string
}

// recoverEnabled says whether the recovery page exists. It needs mails that
// really leave the server, and a public URL. Without the URL, the links in
// the mail would take their host from the request. A visitor controls that
// host, so they could send an organiser a mail with admin links that lead to
// another site. The command line already refuses to send mails without a
// public URL, and this check keeps the page safe for any other caller.
func (s *Server) recoverEnabled() bool {
	return s.cfg.MailEnabled && s.cfg.Mailer != nil && s.cfg.BaseURL != ""
}

func (s *Server) recoverForm(w http.ResponseWriter, r *http.Request) {
	if !s.recoverEnabled() {
		s.renderError(w, r, http.StatusNotFound, "Cette page n'existe pas.")
		return
	}
	s.render(w, r, http.StatusOK, "recover", recoverPage{Common: s.common(w, r, "Retrouver mes liens")})
}

// recoverSubmit e-mails the admin links of the events that an address
// organises, to that address only. It answers the same thing for every valid
// address, and limits the requests of one client, the mails to one address
// and the mails in all.
func (s *Server) recoverSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.recoverEnabled() {
		s.renderError(w, r, http.StatusNotFound, "Cette page n'existe pas.")
		return
	}
	t := s.now()
	client := s.clientIP(r)
	// Every request counts, even one with an invalid address or an address
	// that organises nothing, so that this limit does not depend on the
	// address.
	if !s.mails.recoverIP.allow(client, s.mails.limits.RecoverPerIPPerHour, t) {
		w.Header().Set("Retry-After", "3600")
		s.renderError(w, r, http.StatusTooManyRequests, "Trop de demandes. Réessayez dans une heure.")
		return
	}
	email := clean(r.FormValue("email"), 120, false)
	fail := func(msg string) {
		s.render(w, r, http.StatusBadRequest, "recover", recoverPage{Common: Common{Title: "Retrouver mes liens"}, Error: msg, Email: email})
	}
	if email == "" {
		fail("Indiquez l'adresse e-mail de l'organisateur.")
		return
	}
	if !validEmail(email) {
		fail("Cette adresse e-mail ne semble pas valide.")
		return
	}
	// The look-up and the mail take longer for an address that organises
	// events. They run after the answer, so that the delay of the answer does
	// not tell a visitor which addresses are organisers.
	if !s.background(r.Context(), func(ctx context.Context) { s.sendRecovery(ctx, email, client, t) }) {
		s.log.Warn("recovery request dropped: too many are being served")
	}
	setFlash(w, "success", msgRecoverSent)
	redirect(w, r, "/recover")
}

type recoverItem struct {
	Title, Date, URL string
}

// sendRecovery queues the recovery mail, if the address organises events and
// the limits allow it. It runs in the background, after the answer to the
// visitor, and reports nothing to the visitor: see msgRecoverSent. client is
// the address of the visitor.
func (s *Server) sendRecovery(ctx context.Context, email, client string, t time.Time) {
	events, err := s.store.EventsByAdminEmail(ctx, email, maxRecoverEvents+1)
	if err != nil {
		s.log.Error("recover: load events", "err", err)
		return
	}
	if len(events) == 0 {
		return
	}
	more := len(events) > maxRecoverEvents
	if more {
		events = events[:maxRecoverEvents]
	}
	// The mail goes to the address that the organiser typed when creating the
	// event, never to the text of this form.
	to := events[0].AdminEmail
	limits := s.mails.limits
	key := strings.ToLower(strings.TrimSpace(email))
	// A request that does not end in a queued mail must not use up the quota
	// of its address or of the hour: the deferred call gives each hit back.
	var giveBack []func()
	sent := false
	defer func() {
		if !sent {
			for _, release := range giveBack {
				release()
			}
		}
	}()
	// The limit of one client comes first: a client that asks again does not
	// touch the quota that the organiser needs.
	release, ok := s.mails.recoverToFrom.take(key+"\x00"+client, limits.RecoverPerAddressPerClientPerDay, t)
	if !ok {
		s.log.Warn("recovery mail not sent: this client already asked for this address today", "limit", limits.RecoverPerAddressPerClientPerDay, "to", to)
		return
	}
	giveBack = append(giveBack, release)
	release, ok = s.mails.recoverTo.take(key, limits.RecoverPerAddressPerDay, t)
	if !ok {
		s.log.Warn("recovery mail not sent: too many for this address today", "limit", limits.RecoverPerAddressPerDay, "to", to)
		return
	}
	giveBack = append(giveBack, release)
	release, ok = s.mails.recover.take("", limits.RecoverPerHour, t)
	if !ok {
		s.log.Warn("recovery mail not sent: too many this hour", "limit", limits.RecoverPerHour, "to", to)
		return
	}
	giveBack = append(giveBack, release)
	items := make([]recoverItem, 0, len(events))
	for i := range events {
		// Built from the configured URL only (recoverEnabled makes sure that
		// it is set), never from the request.
		items = append(items, recoverItem{Title: events[i].Title, Date: events[i].Date, URL: s.cfg.BaseURL + adminURL(&events[i])})
	}
	// sendMail refuses the mail if the limit of all mails is reached.
	sent = s.sendMail(mail.Message{
		To:      to,
		Subject: "Vos liens d'administration",
		Body:    renderMail("recover", map[string]any{"Events": items, "More": more, "Max": maxRecoverEvents}),
	})
}
