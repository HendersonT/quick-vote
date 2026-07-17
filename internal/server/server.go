// Package server implements the quick-vote HTTP API and static-asset
// serving.
package server

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/quickvote/quickvote/internal/store"
)

// Server wires the chi router to a Store and (optionally) a static asset
// filesystem for the built SPA. It implements http.Handler.
type Server struct {
	store  *store.Store
	static fs.FS
	router chi.Router
}

// New builds a Server. staticFS may be nil (e.g. in tests) in which case no
// static/SPA routes are registered — only /api.
func New(st *store.Store, staticFS fs.FS) *Server {
	s := &Server{store: st, static: staticFS}
	s.router = s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api/votes", func(r chi.Router) {
		r.Post("/", s.handleCreateVote)
		r.Route("/{slug}", func(r chi.Router) {
			r.Get("/", s.handleGetVote)
			r.Post("/join", s.handleJoin)
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
