package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/models"
)

// apiNotesKey stores the admin's free-form "API security ideas" note.
const apiNotesKey = "api_ideas"

func (s *Server) mountApiNotes(r chi.Router) {
	r.Get("/api/api-notes", s.getApiNotes)
	r.Put("/api/api-notes", s.setApiNotes)
}

func (s *Server) getApiNotes(w http.ResponseWriter, r *http.Request) {
	var st models.Setting
	s.db.Where("key = ?", apiNotesKey).First(&st)
	writeJSON(w, http.StatusOK, map[string]string{"notes": st.Value})
}

func (s *Server) setApiNotes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Notes string `json:"notes"`
	}
	if err := decodeJSON(r, &body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.Notes) > 5000 {
		body.Notes = body.Notes[:5000]
	}
	if err := s.db.Save(&models.Setting{Key: apiNotesKey, Value: body.Notes}).Error; err != nil {
		httpError(w, http.StatusInternalServerError, "save failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"notes": body.Notes})
}
