package web

import (
	"bytes"
	"context"
	"net/http"
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
`))

func renderMail(name string, data any) string {
	var b bytes.Buffer
	if err := mailTemplates.ExecuteTemplate(&b, name, data); err != nil {
		return "" // templates are tested; never send a half-written message
	}
	return strings.TrimLeft(b.String(), "\n")
}

func (s *Server) sendMail(m mail.Message) {
	if s.cfg.Mailer == nil || m.To == "" || m.Body == "" {
		return
	}
	s.cfg.Mailer.Enqueue(m)
}

// notifyCreated sends the organiser the admin link of a new event.
func (s *Server) notifyCreated(r *http.Request, ev *model.Event) {
	s.sendMail(mail.Message{
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
