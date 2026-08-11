package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// mountStatic serves the local Vazirmatn fonts and the built frontend, with an
// SPA fallback so deep-links/refreshes return index.html instead of 404
// (fixes the former StaticFiles(html=True) deep-link bug).
func (s *Server) mountStatic(r chi.Router) {
	fontsDir := filepath.Join(s.cfg.AssetsDir, "fonts")
	if st, err := os.Stat(fontsDir); err == nil && st.IsDir() {
		r.Handle("/fonts/*", http.StripPrefix("/fonts/", http.FileServer(http.Dir(fontsDir))))
	}
	r.NotFound(s.staticHandler)
	r.MethodNotAllowed(s.staticHandler)
}

func (s *Server) staticHandler(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	// API paths keep a clean JSON 404 (the admin SPA parses it).
	if strings.HasPrefix(p, "/api/") {
		httpError(w, http.StatusNotFound, "Not found")
		return
	}
	// Other unknown fixed paths are "wrong addresses" -> friendly page + tarpit.
	if strings.HasPrefix(p, "/sub/") || strings.HasPrefix(p, "/fonts/") || strings.HasPrefix(p, "/map/") {
		s.notFound(w, r)
		return
	}

	dir := s.cfg.StaticDir
	if dir == "" {
		s.notFound(w, r)
		return
	}
	clean := filepath.Clean(p)
	full := filepath.Join(dir, clean)
	// prevent path traversal outside the static dir
	if !strings.HasPrefix(full, filepath.Clean(dir)) {
		s.notFound(w, r)
		return
	}
	if st, err := os.Stat(full); err == nil && !st.IsDir() {
		// Vite emits content-hashed asset filenames, so they are safe to cache
		// forever; a new build changes the name and busts the cache.
		if strings.HasPrefix(p, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, full)
		return
	}
	// SPA fallback -> index.html. Never cache it, so a rebuilt frontend (which
	// references a new hashed bundle) is picked up on the next load instead of
	// the browser serving a stale index that points at the old bundle.
	s.serveIndex(w, r, dir)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err == nil {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
		return
	}
	httpError(w, http.StatusNotFound, "Not found")
}
