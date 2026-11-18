package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"benevole-ape/internal/model"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

var pageNames = []string{"home", "event", "signup", "stand", "admin", "error", "recover"}

func (s *Server) loadTemplates() error {
	sum := sha256.New()
	for _, f := range []string{"static/app.css", "static/app.js", "static/htmx.min.js"} {
		b, err := staticFS.ReadFile(f)
		if err != nil {
			return err
		}
		sum.Write(b)
	}
	s.assetVersion = hex.EncodeToString(sum.Sum(nil))[:10]

	funcs := templateFuncs()
	funcs["asset"] = func(name string) string { return "/static/" + name + "?v=" + s.assetVersion }

	base, err := template.New("base").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/partials.html")
	if err != nil {
		return err
	}
	s.pages = map[string]*template.Template{}
	for _, name := range pageNames {
		t, err := base.Clone()
		if err != nil {
			return err
		}
		if _, err := t.ParseFS(templateFS, "templates/"+name+".html"); err != nil {
			return fmt.Errorf("template %s: %w", name, err)
		}
		s.pages[name] = t
	}
	return nil
}

func (s *Server) staticHandler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	fsrv := http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		fsrv.ServeHTTP(w, r)
	})
}

// Common is embedded in every page's data.
type Common struct {
	Title string
	Flash *Flash
}

// Flash is a one-shot message carried by a cookie across a redirect.
type Flash struct {
	Kind    string // success, error, warning, info
	Message string
}

const flashCookie = "flash"

func setFlash(w http.ResponseWriter, kind, msg string) {
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(kind + "|" + msg)),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 60,
	})
}

func popFlash(w http.ResponseWriter, r *http.Request) *Flash {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1})
	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	kind, msg, ok := strings.Cut(string(b), "|")
	if !ok {
		return nil
	}
	switch kind {
	case "success", "error", "warning", "info":
	default:
		kind = "info"
	}
	return &Flash{Kind: kind, Message: msg}
}

func (s *Server) common(w http.ResponseWriter, r *http.Request, title string) Common {
	return Common{Title: title, Flash: popFlash(w, r)}
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// render executes the "layout" template of a page.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	s.renderTemplate(w, r, status, page, "layout", data)
}

// renderTemplate executes a named template of a page's template set.
func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, status int, page, name string, data any) {
	t, ok := s.pages[page]
	if !ok {
		s.log.Error("unknown page", "page", page)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		s.log.Error("template", "page", page, "name", name, "err", err)
		http.Error(w, "Erreur interne", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

type errorPage struct {
	Common
	Status  int
	Message string
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	s.render(w, r, status, "error", errorPage{Common: Common{Title: "Oups"}, Status: status, Message: msg})
}

// absURL turns a path into an absolute URL for display and sharing.
func (s *Server) absURL(r *http.Request, path string) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL + path
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.cfg.TrustProxy {
		if p := r.Header.Get("X-Forwarded-Proto"); p == "http" || p == "https" {
			scheme = p
		}
	}
	return scheme + "://" + r.Host + path
}

var (
	frDays   = []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	frMonths = []string{"", "janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
)

// hm formats a time as "14h" or "14h30".
func hm(t time.Time) string {
	if t.Minute() == 0 {
		return fmt.Sprintf("%dh", t.Hour())
	}
	return fmt.Sprintf("%dh%02d", t.Hour(), t.Minute())
}

func dayFR(t time.Time) string {
	return fmt.Sprintf("%s %d %s", frDays[t.Weekday()], t.Day(), frMonths[t.Month()])
}

func dateFR(s string) string {
	t, err := time.Parse(model.DateLayout, s)
	if err != nil {
		return s
	}
	return dayFR(t) + " " + strconv.Itoa(t.Year())
}

// fillLabel is the French name of a slot's fill status (model.Fill*).
func fillLabel(status string) string {
	switch status {
	case model.FillEmpty:
		return "personne"
	case model.FillPartial:
		return "à compléter"
	case model.FillOK:
		return "OK"
	case model.FillFull:
		return "complet"
	}
	return status
}

// eventStatusLabel is the French name of an event status.
func eventStatusLabel(status string) string {
	switch status {
	case model.StatusDraft:
		return "Brouillon"
	case model.StatusOpen:
		return "Ouvert"
	case model.StatusClosed:
		return "Clos"
	}
	return status
}

// todoURL is the event page, showing only the slots still to fill if todo.
func todoURL(base string, todo bool) string {
	if todo {
		return base + "?todo=1"
	}
	return base
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"hm":        hm,
		"day":       dayFR,
		"date":      dateFR,
		"span":      func(a, b time.Time) string { return hm(a) + " – " + hm(b) },
		"dur":       durationFR,
		"pub":       model.DisplayName,
		"fillLabel": fillLabel,
		"evlabel":   eventStatusLabel,
		"todoURL":   todoURL,
		"timeVal":   func(t time.Time) string { return t.Format("15:04") },
		"dateVal":   func(t time.Time) string { return t.Format(model.DateLayout) },
		"pct": func(a, b int) int {
			if b <= 0 {
				return 100
			}
			if a >= b {
				return 100
			}
			return a * 100 / b
		},
		"plural": func(n int, one, many string) string {
			if n > 1 {
				return many
			}
			return one
		},
		// dict builds a map for passing several values to a sub-template.
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, fmt.Errorf("dict: odd number of arguments")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				k, ok := kv[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict: key %v is not a string", kv[i])
				}
				m[k] = kv[i+1]
			}
			return m, nil
		},
		"css": func(s string) template.CSS {
			if validColor(s) {
				return template.CSS(s)
			}
			return template.CSS(defaultColor)
		},
	}
}

// durationFR formats a duration the French way: "45 min", "2 h", "2 h 30".
func durationFR(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	switch {
	case m < 60:
		return strconv.Itoa(m) + " min"
	case m%60 == 0:
		return strconv.Itoa(m/60) + " h"
	default:
		return fmt.Sprintf("%d h %02d", m/60, m%60)
	}
}
