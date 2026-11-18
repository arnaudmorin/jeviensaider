package web

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"text/template"

	"benevole-ape/internal/mail"
	"benevole-ape/internal/model"
)

// Mailer queues e-mails for background delivery.
type Mailer interface {
	Enqueue(mail.Message)
}

var mailTemplates = template.Must(template.New("mail").Funcs(template.FuncMap{
	"day":    dayFR,
	"date":   dateFR,
	"span":   func(sv *model.SlotView) string { return hm(sv.StartsAt) + " – " + hm(sv.EndsAt) },
	"plural": func(n int, one, many string) string { return map[bool]string{true: one, false: many}[n <= 1] },
}).Parse(`
{{define "created"}}Bonjour,

Votre événement « {{.Event.Title}} »{{with .Event.Date}} du {{date .}}{{end}} a bien été créé sur Je viens aider.

Lien d'administration, à garder précieusement :
{{.AdminURL}}

C'est la seule clé pour gérer l'événement : conservez ce message et ne partagez ce lien qu'avec les autres organisateurs.

Prochaines étapes :
  1. Ajoutez vos stands et leurs créneaux.
  2. Passez l'événement en « Ouvert ».
  3. Partagez le lien public avec les parents :
     {{.PublicURL}}
{{if .Event.NotifySignups}}
Vous recevrez un e-mail à chaque inscription ou désinscription. Vous pouvez les désactiver depuis la page d'administration, dans « Modifier l'événement ».
{{end}}
Si vous n'avez pas créé cet événement, ignorez simplement ce message.

-- 
Je viens aider
{{end}}

{{define "signup"}}Bonjour,

{{.Name}} {{if .Cancel}}s'est désinscrit·e de{{else}}s'est inscrit·e sur{{end}} « {{.Slot.Stand.Name}} », {{day .Slot.StartsAt}}, {{span .Slot}}.
{{if not .Cancel}}
Téléphone : {{or .Phone "non renseigné"}}{{with .Comment}}
Commentaire : {{.}}{{end}}
{{end}}
Ce créneau : {{.Slot.Count}}/{{.Slot.MinPeople}}{{if .Slot.Missing}}, il manque {{.Slot.Missing}} {{plural .Slot.Missing "personne" "personnes"}}{{else}}, complet{{end}}.
L'événement : {{.P.Count}}/{{.P.Needed}} places couvertes{{if .P.Missing}}, il manque {{.P.Missing}} {{plural .P.Missing "personne" "personnes"}}{{end}}.

Voir le planning et les coordonnées :
{{.AdminURL}}

Pour ne plus recevoir ces e-mails : page d'administration, « Modifier l'événement ».

-- 
Je viens aider
{{end}}

{{define "paused"}}Bonjour,

Plus de {{.Limit}} inscriptions ou désinscriptions ont eu lieu en une heure sur votre événement. Pour ne pas encombrer votre boîte, les notifications sont en pause jusqu'à {{.Until}}.

Le planning à jour est toujours sur la page d'administration :
{{.AdminURL}}

-- 
Je viens aider
{{end}}
`))

func renderMail(name string, data any) string {
	var b bytes.Buffer
	if err := mailTemplates.ExecuteTemplate(&b, name, data); err != nil {
		return "" // templates are tested; never send a half-written message
	}
	return strings.TrimLeft(b.String(), "\n")
}

// sendMail queues m within the overall hourly limit and reports whether it
// was queued.
func (s *Server) sendMail(m mail.Message) bool {
	if s.cfg.Mailer == nil || m.To == "" || m.Body == "" {
		return false
	}
	if !s.mails.all.allow("", mailsPerHour, now()) {
		s.log.Warn("mail not sent: hourly limit of the site reached", "limit", mailsPerHour, "to", m.To, "subject", m.Subject)
		return false
	}
	s.cfg.Mailer.Enqueue(m)
	return true
}

// notifyCreated sends the organiser the admin link of a new event, and
// reports whether the mail was queued. The address was typed by the
// visitor, so mails to one address and in total are limited.
func (s *Server) notifyCreated(r *http.Request, ev *model.Event) bool {
	if s.cfg.Mailer == nil || ev.AdminEmail == "" {
		return false
	}
	t := now()
	if !s.mails.createdTo.allow(strings.ToLower(ev.AdminEmail), createdPerRecipientPerDay, t) {
		s.log.Warn("creation mail not sent: too many for this address today", "limit", createdPerRecipientPerDay, "to", ev.AdminEmail)
		return false
	}
	if !s.mails.created.allow("", createdPerHour, t) {
		s.log.Warn("creation mail not sent: too many events created this hour", "limit", createdPerHour, "to", ev.AdminEmail)
		return false
	}
	return s.sendMail(mail.Message{
		To:      ev.AdminEmail,
		Subject: "Votre événement « " + ev.Title + " » est créé",
		Body: renderMail("created", map[string]any{
			"Event":     ev,
			"AdminURL":  s.absURL(r, adminURL(ev)),
			"PublicURL": s.absURL(r, eventURL(ev)),
		}),
	})
}

// notifySignup tells the organiser that someone signed up for, or cancelled,
// a slot, if they asked for these messages.
func (s *Server) notifySignup(ctx context.Context, r *http.Request, ev *model.Event, vol *model.Volunteer, slotID int64, comment string, cancel bool) {
	if !ev.NotifySignups || ev.AdminEmail == "" || s.cfg.Mailer == nil {
		return
	}
	p, err := s.planning(ctx, ev, 0)
	if err != nil {
		s.log.Error("notify: load planning", "err", err)
		return
	}
	var sv *model.SlotView
	for _, st := range p.Stands {
		for _, v := range st.Slots {
			if v.ID == slotID {
				sv = v
			}
		}
	}
	if sv == nil {
		return
	}
	n, until := s.mails.signups.hit(strconv.FormatInt(ev.ID, 10), now())
	switch {
	case n == signupMailsPerEventHour+1:
		// Once over the limit (a busy hour, or someone signing up and
		// cancelling in a loop), say so once, then stay quiet.
		s.sendMail(mail.Message{
			To:      ev.AdminEmail,
			Subject: "Beaucoup d'inscriptions sur « " + ev.Title + " » : notifications en pause",
			Body: renderMail("paused", map[string]any{
				"Limit": signupMailsPerEventHour, "Until": hm(until.Local()), "AdminURL": s.absURL(r, adminURL(ev)),
			}),
		})
		return
	case n < 1 || n > signupMailsPerEventHour:
		return
	}
	verb := "Inscription"
	if cancel {
		verb = "Désinscription"
	}
	s.sendMail(mail.Message{
		To:      ev.AdminEmail,
		Subject: verb + " : " + vol.Name + " – " + sv.Stand.Name + ", " + hm(sv.StartsAt) + " – " + hm(sv.EndsAt),
		Body: renderMail("signup", map[string]any{
			"Name":     vol.Name,
			"Phone":    vol.Phone,
			"Comment":  comment,
			"Cancel":   cancel,
			"Slot":     sv,
			"P":        p,
			"AdminURL": s.absURL(r, adminURL(ev)),
		}),
	})
}
