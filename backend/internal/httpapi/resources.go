package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/models"
	"multivpn/internal/services"
)

func (s *Server) mountResources(r chi.Router) {
	r.Route("/api/resources", func(r chi.Router) {
		r.Get("/", s.listResources)
		r.Post("/", s.createResource)
		r.Patch("/{id}", s.updateResource)
		r.Delete("/{id}", s.deleteResource)
	})
}

func resourceOut(r *models.Resource) map[string]any {
	return map[string]any{
		"id": r.ID, "kind": r.Kind, "title": r.Title, "description": r.Description,
		"url": r.URL, "icon": r.Icon, "platform": r.Platform,
		"sort_order": r.SortOrder, "enabled": r.Enabled,
	}
}

func (s *Server) listResources(w http.ResponseWriter, r *http.Request) {
	rows := services.ResourcesList(s.db, false)
	out := make([]map[string]any, 0, len(rows))
	for i := range rows {
		out = append(out, resourceOut(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createResource(w http.ResponseWriter, r *http.Request) {
	var data map[string]any
	if err := decodeJSON(r, &data); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	row := services.ResourceCreate(s.db, data)
	writeJSON(w, http.StatusCreated, resourceOut(row))
}

func (s *Server) updateResource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		httpError(w, http.StatusNotFound, "Resource not found")
		return
	}
	var data map[string]any
	if err := decodeJSON(r, &data); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	row := services.ResourceUpdate(s.db, id, data)
	if row == nil {
		httpError(w, http.StatusNotFound, "Resource not found")
		return
	}
	writeJSON(w, http.StatusOK, resourceOut(row))
}

func (s *Server) deleteResource(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || !services.ResourceDelete(s.db, id) {
		httpError(w, http.StatusNotFound, "Resource not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
