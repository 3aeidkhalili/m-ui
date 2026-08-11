package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/services"
)

func (s *Server) mountProtocols(r chi.Router) {
	r.Get("/api/protocols", s.getProtocols)
	r.Patch("/api/protocols", s.updateProtocols)
}

func (s *Server) getProtocols(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"fields": services.ProtocolFields,
		"values": services.ProtocolsGetAll(s.db),
	})
}

func (s *Server) updateProtocols(w http.ResponseWriter, r *http.Request) {
	var data map[string]any
	if err := decodeJSON(r, &data); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fields": services.ProtocolFields,
		"values": services.ProtocolsUpdate(s.db, data),
	})
}
