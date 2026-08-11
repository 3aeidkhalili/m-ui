package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/services"
)

func (s *Server) mountLogs(r chi.Router) {
	r.Get("/api/logs", s.listLogs)
	r.Delete("/api/logs", s.clearLogs)
}

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 300
	}
	logs := services.LogsList(s.db, category, limit)
	out := make([]map[string]any, 0, len(logs))
	for i := range logs {
		out = append(out, map[string]any{
			"id":         logs[i].ID,
			"created_at": isoPtr(logs[i].CreatedAt),
			"category":   logs[i].Category,
			"level":      logs[i].Level,
			"actor":      logs[i].Actor,
			"message":    logs[i].Message,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) clearLogs(w http.ResponseWriter, r *http.Request) {
	services.LogsClear(s.db)
	services.AuditLog(s.db, services.LogSystem, "warn", "ادمین", "لاگ سیستم پاک شد")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
