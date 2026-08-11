package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"multivpn/internal/services"
	"multivpn/internal/store"
)

func (s *Server) mountBackup(r chi.Router) {
	r.Get("/api/backup", s.downloadBackup)
	r.Post("/api/restore", s.uploadRestore)
}

// GET /api/backup — stream a consistent snapshot of the panel database.
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	path, err := services.BackupDB(s.db)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "backup failed: "+err.Error())
		return
	}
	defer os.Remove(path)
	f, err := os.Open(path)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "backup read failed")
		return
	}
	defer f.Close()
	name := fmt.Sprintf("multivpn-backup-%s.db", time.Now().Format("2006-01-02-1504"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, f)
	services.AuditLog(s.db, services.LogSystem, "info", "ادمین", "بکاپِ دیتابیس دانلود شد")
}

// POST /api/restore — accept an uploaded DB, validate it, stage it, and restart
// so the swap + reconcile happen at boot (no DB handle open).
func (s *Server) uploadRestore(w http.ResponseWriter, r *http.Request) {
	// cap the upload at 200 MiB
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		httpError(w, http.StatusBadRequest, "فرمِ آپلود نامعتبر است")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		httpError(w, http.StatusBadRequest, "فایلی ارسال نشد")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "multivpn-restore-*.db")
	if err != nil {
		httpError(w, http.StatusInternalServerError, "ساخت فایل موقت ناموفق")
		return
	}
	tmpPath := tmp.Name()
	_, copyErr := io.Copy(tmp, file)
	tmp.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		httpError(w, http.StatusInternalServerError, "دریافت فایل ناموفق")
		return
	}
	defer os.Remove(tmpPath)

	if err := store.ValidateSQLite(tmpPath); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	livePath := s.cfg.SQLitePath()
	if livePath == "" {
		httpError(w, http.StatusInternalServerError, "مسیر دیتابیس نامشخص است")
		return
	}
	// stage next to the live DB; the swap happens at next boot
	if err := copyFileTo(tmpPath, livePath+".restore"); err != nil {
		httpError(w, http.StatusInternalServerError, "استیجِ بازیابی ناموفق: "+err.Error())
		return
	}

	services.AuditLog(s.db, services.LogSystem, "critical", "ادمین", "بازیابیِ بکاپ درخواست شد؛ پنل در حال ری‌استارت")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "بکاپ تأیید شد. پنل در حال ری‌استارت و بازیابی است؛ چند ثانیه بعد دوباره وارد شوید.",
	})
	// restart so the DB is swapped + reconciled at boot (systemd Restart=always)
	go func() {
		time.Sleep(600 * time.Millisecond)
		os.Exit(0)
	}()
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
