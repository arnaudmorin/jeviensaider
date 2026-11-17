package web

import (
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; "+
				"frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		// same-origin: secret links never leak to other sites via Referer,
		// while same-origin POSTs still carry a proper Origin header.
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		next.ServeHTTP(w, r)
	})
}

// csrf rejects state-changing requests coming from another site. Modern
// browsers send Sec-Fetch-Site; older ones send Origin. Non-browser clients
// (curl, tests) send neither and are allowed.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ok := true
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
			ok = site == "same-origin" || site == "none"
		} else if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			ok = err == nil && u.Host == r.Host
		}
		if !ok {
			s.renderError(w, r, http.StatusForbidden, "Requête refusée (origine inconnue).")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// limiter is a per-IP token bucket.
type limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(perMinute int) *limiter {
	return &limiter{
		rate:    float64(perMinute) / 60,
		burst:   float64(perMinute) / 2,
		buckets: map[string]*bucket{},
		lastGC:  time.Now(),
	}
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastGC) > 10*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !s.limiter.allow(s.clientIP(r), time.Now()) {
			w.Header().Set("Retry-After", "30")
			s.renderError(w, r, http.StatusTooManyRequests, "Trop de requêtes, patientez quelques secondes avant de réessayer.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

var secretPath = regexp.MustCompile(`^/(a|e/[^/]+/me)/[^/]+`)

// redactPath hides admin and edit tokens so they never end up in logs.
func redactPath(p string) string {
	return secretPath.ReplaceAllString(p, "/$1/***")
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("request", "method", r.Method, "path", redactPath(r.URL.Path),
			"status", rec.status, "dur", time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "path", redactPath(r.URL.Path), "panic", v)
				http.Error(w, "Erreur interne", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
