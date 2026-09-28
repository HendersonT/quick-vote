// Package server implements the quick-vote HTTP API and static-asset
// serving.
package server

import (
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/HendersonT/quick-vote/internal/store"
)

// maxRequestBody caps the size of a JSON request body. Every legitimate
// payload (a title <=200 chars, a name <=50, a ballot over a bounded option
// set) fits comfortably; the cap stops an anonymous client from streaming a
// huge body that would be fully buffered in memory before validation.
const maxRequestBody = 64 << 10 // 64 KiB

// maxBytes returns middleware that caps each request body at n bytes via
// http.MaxBytesReader, so an oversized body is truncated (and Decode errors)
// instead of being read entirely into memory.
func maxBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// Config holds deployment-specific options. The zero value is a safe default
// for a server exposed directly (no trusted proxy, same-origin WebSockets).
type Config struct {
	// TrustedIPHeader names a header set by a trusted reverse proxy carrying
	// the real client IP (e.g. "CF-Connecting-IP", "X-Forwarded-For"). Leave
	// empty unless the server is reachable only through that proxy.
	TrustedIPHeader string
	// AllowedOrigins lists extra origins (scheme://host[:port]) permitted to
	// open WebSockets, beyond the page's own origin.
	AllowedOrigins []string
}

// Server wires the chi router to a Store and (optionally) a static asset
// filesystem for the built SPA. It implements http.Handler.
type Server struct {
	store     *store.Store
	static    fs.FS
	router    chi.Router
	scheduler *Scheduler
	onChange  func(slug string)
	hub       *hub
	cfg       Config

	// writeLimit throttles every mutating request per client IP; createLimit
	// and createGlobal additionally throttle vote creation, the one endpoint
	// that needs no existing link.
	writeLimit   *rateLimiter
	createLimit  *rateLimiter
	createGlobal *rateLimiter

	// slugMu guards slugLocks; each per-slug mutex serializes phase-affecting
	// mutations for one vote so read-modify-write transitions (ballot +
	// auto-advance, advance, revote, timer firing) are atomic per room and
	// can't double-score or drop a ballot under concurrent requests.
	slugMu    sync.Mutex
	slugLocks map[string]*sync.Mutex
}

// lockSlug acquires the per-slug mutation lock for slug and returns its unlock
// function (call via defer). Store operations acquire their own lock strictly
// inside this one, so the ordering is fixed and deadlock-free.
func (s *Server) lockSlug(slug string) func() {
	s.slugMu.Lock()
	mu, ok := s.slugLocks[slug]
	if !ok {
		mu = &sync.Mutex{}
		s.slugLocks[slug] = mu
	}
	s.slugMu.Unlock()

	mu.Lock()
	return mu.Unlock
}

// New builds a Server with the default Config. staticFS may be nil (e.g. in
// tests) in which case no static/SPA routes are registered — only /api.
func New(st *store.Store, staticFS fs.FS) *Server {
	return NewWithConfig(st, staticFS, Config{})
}

// NewWithConfig builds a Server with explicit deployment options.
func NewWithConfig(st *store.Store, staticFS fs.FS, cfg Config) *Server {
	s := &Server{
		store:     st,
		static:    staticFS,
		hub:       newHub(),
		cfg:       cfg,
		slugLocks: map[string]*sync.Mutex{},
		// Generous enough for a whole party behind one NAT'd IP.
		writeLimit:   newRateLimiter(120, 500*time.Millisecond),
		createLimit:  newRateLimiter(10, time.Minute),
		createGlobal: newRateLimiter(100, 10*time.Second),
	}
	s.scheduler = NewScheduler(func(slug string) { s.timerFired(slug) })
	s.router = s.routes()
	s.onChange = s.broadcast
	return s
}

// SetOnChange registers a hook invoked with a vote's slug whenever that vote's
// room state changes. The WebSocket hub (Task 8) uses it to broadcast fresh
// snapshots.
func (s *Server) SetOnChange(fn func(slug string)) {
	s.onChange = fn
}

// changed notifies the onChange hook (if any) that slug's state changed.
func (s *Server) changed(slug string) {
	if s.onChange != nil {
		s.onChange(slug)
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)

	// write = body cap + per-IP throttle, applied to every mutating route.
	write := chi.Chain(maxBytes(maxRequestBody), s.limit(s.writeLimit, nil))

	r.Route("/api/votes", func(r chi.Router) {
		r.With(write...).With(s.limit(s.createLimit, s.createGlobal)).Post("/", s.handleCreateVote)
		r.Route("/{slug}", func(r chi.Router) {
			r.Get("/", s.handleGetVote)
			r.With(write...).Post("/join", s.handleJoin)
			r.With(write...).Post("/suggestions", s.handleCreateSuggestion)
			r.With(write...).Delete("/suggestions/{id}", s.handleDeleteSuggestion)
			r.With(write...).Post("/done-suggesting", s.handleDoneSuggesting)
			r.With(write...).Put("/ballot", s.handlePutBallot)
			r.With(write...).Post("/advance", s.handleAdvance)
			r.With(write...).Post("/revote", s.handleRevote)
			r.With(s.limit(s.writeLimit, nil)).Get("/ws", s.handleWS)
		})
	})

	if s.static != nil {
		r.NotFound(s.handleStatic)
	}

	return r
}

// handleStatic serves files out of the embedded SPA build, falling back to
// index.html for any path that isn't a real static file (client-side
// routing) — except paths under /api, which stay 404.
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	fileServer := http.FileServer(http.FS(s.static))

	upath := strings.TrimPrefix(r.URL.Path, "/")
	if upath == "" {
		upath = "index.html"
	}
	if f, err := s.static.Open(upath); err == nil {
		_ = f.Close()
		fileServer.ServeHTTP(w, r)
		return
	}

	r2 := r.Clone(r.Context())
	r2.URL.Path = "/"
	r2.URL.RawPath = ""
	fileServer.ServeHTTP(w, r2)
}
