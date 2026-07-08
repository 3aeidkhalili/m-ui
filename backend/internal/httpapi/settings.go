package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/services"
)

func (s *Server) mountSettings(r chi.Router) {
	r.Get("/api/settings", s.getSettings)
	r.Patch("/api/settings", s.updateSettings)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"fields": services.SettingsFields,
		"values": services.SettingsGetAll(s.db),
	})
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var data map[string]any
	if err := decodeJSON(r, &data); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	values := services.SettingsUpdate(s.db, data)
	writeJSON(w, http.StatusOK, map[string]any{"fields": services.SettingsFields, "values": values})
}
