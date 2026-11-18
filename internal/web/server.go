// Package web serves the HTML interface: public sign-up pages and the
// secret-link admin pages.
package web

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"benevole-ape/internal/store"
)

// Config tunes the server.
type Config struct {
	// BaseURL is the public origin (e.g. "https://benevoles.ape-ecole.fr"),
	// used to display shareable links. Empty: derived from each request.
	BaseURL string
	// CreatePassword, if set, is required to create a new event.
	CreatePassword string
	// TrustProxy makes the server honour X-Forwarded-For / X-Forwarded-Proto.
	TrustProxy bool
	// PostsPerMinute is the per-IP rate limit on POST requests (0: default 60).
	PostsPerMinute int
	// RetentionDays is how long events are kept after their last day of
	// activity, shown on the admin page (0: kept forever). The deletion itself
	// is run by the caller with store.Cleanup.
	RetentionDays int
	Logger        *slog.Logger
}

// Server holds the dependencies of the HTTP handlers.
type Server struct {
	store        *store.Store
	cfg          Config
	log          *slog.Logger
	pages        map[string]*template.Template
	assetVersion string
	limiter      *limiter
}

// New builds a Server and parses its templates.
func New(st *store.Store, cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PostsPerMinute <= 0 {
		cfg.PostsPerMinute = 60
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	s := &Server{store: st, cfg: cfg, log: cfg.Logger, limiter: newLimiter(cfg.PostsPerMinute)}
	if err := s.loadTemplates(); err != nil {
		return nil, err
	}
	return s, nil
}

// Handler returns the full HTTP handler with middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("POST /events", s.createEvent)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.Handle("GET /static/", s.staticHandler())

	// Public, per event.
	mux.HandleFunc("GET /e/{slug}", s.withEvent(s.eventPage))
	mux.HandleFunc("GET /e/{slug}/slots/{id}/signup", s.withEvent(s.signupForm))
	mux.HandleFunc("POST /e/{slug}/slots/{id}/signup", s.withEvent(s.signupSubmit))
	mux.HandleFunc("POST /e/{slug}/forget", s.withEvent(s.forget))
	mux.HandleFunc("GET /e/{slug}/me/{token}", s.withVolunteer(s.mePage))
	mux.HandleFunc("POST /e/{slug}/me/{token}", s.withVolunteer(s.meUpdate))
	mux.HandleFunc("POST /e/{slug}/me/{token}/signups/{id}/delete", s.withVolunteer(s.meCancel))

	// Admin, the token is the credential.
	mux.HandleFunc("GET /a/{token}", s.withAdmin(s.adminPage))
	mux.HandleFunc("POST /a/{token}/event", s.withAdmin(s.adminUpdateEvent))
	mux.HandleFunc("POST /a/{token}/status", s.withAdmin(s.adminSetStatus))
	mux.HandleFunc("POST /a/{token}/stands", s.withAdmin(s.adminCreateStand))
	mux.HandleFunc("POST /a/{token}/stands/{id}", s.withAdmin(s.adminUpdateStand))
	mux.HandleFunc("POST /a/{token}/stands/{id}/delete", s.withAdmin(s.adminDeleteStand))
	mux.HandleFunc("POST /a/{token}/stands/{id}/move", s.withAdmin(s.adminMoveStand))
	mux.HandleFunc("POST /a/{token}/stands/{id}/slots", s.withAdmin(s.adminGenerateSlots))
	mux.HandleFunc("POST /a/{token}/slots/{id}", s.withAdmin(s.adminUpdateSlot))
	mux.HandleFunc("POST /a/{token}/slots/{id}/delete", s.withAdmin(s.adminDeleteSlot))
	mux.HandleFunc("POST /a/{token}/signups", s.withAdmin(s.adminAddSignup))
	mux.HandleFunc("POST /a/{token}/signups/{id}/delete", s.withAdmin(s.adminDeleteSignup))
	mux.HandleFunc("GET /a/{token}/export.csv", s.withAdmin(s.adminExportCSV))
	mux.HandleFunc("GET /a/{token}/print", s.withAdmin(s.adminPrint))
	mux.HandleFunc("POST /a/{token}/purge", s.withAdmin(s.adminPurge))
	mux.HandleFunc("POST /a/{token}/duplicate", s.withAdmin(s.adminDuplicate))
	mux.HandleFunc("POST /a/{token}/delete", s.withAdmin(s.adminDeleteEvent))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.renderError(w, r, http.StatusNotFound, "Cette page n'existe pas.")
	})

	var h http.Handler = mux
	h = s.rateLimit(h)
	h = s.csrf(h)
	h = securityHeaders(h)
	h = s.logRequests(h)
	h = s.recoverPanics(h)
	return h
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "db: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

// redirect sends a 303 See Other (POST/redirect/GET).
func redirect(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// serverError logs err and renders a generic error page.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal error", "path", redactPath(r.URL.Path), "err", err)
	s.renderError(w, r, http.StatusInternalServerError, "Une erreur inattendue s'est produite. Réessayez dans un instant.")
}

// storeError maps store errors to HTTP responses; returns true if handled.
func (s *Server) storeError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		s.renderError(w, r, http.StatusNotFound, "Élément introuvable (il a peut-être été supprimé).")
		return true
	}
	s.serverError(w, r, err)
	return true
}
