import React, { useState } from "react";
import { api } from "../api";
import type { User } from "../api";

const GIB = 1024 ** 3;

interface EditUserModalProps {
  user: User;
  onClose: () => void;
  onSaved: () => void;
}

export default function EditUserModal({ user, onClose, onSaved }: EditUserModalProps) {
  const [quotaGb, setQuotaGb] = useState<string | number>(
    user.quota_bytes > 0 ? Math.round(user.quota_bytes / GIB) : 0,
  );
  const [expiresDays, setExpiresDays] = useState<string | number>("");
  const [note, setNote] = useState(user.note || "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const currentExpiry = user.expires_at
    ? new Date(user.expires_at).toLocaleDateString("fa-IR")
    : "بدون انقضا";

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const payload: Partial<User> & { quota_gb?: number; expires_days?: number } = {
        quota_gb: Number(quotaGb) || 0,
        note: note.trim(),
      };
      // Only touch expiry when the admin typed a value (empty = keep as-is).
      if (String(expiresDays).trim() !== "") {
        payload.expires_days = Math.max(0, Math.floor(Number(expiresDays)) || 0);
      }
      await api.updateUser(user.id, payload);
      onSaved();
      onClose();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <form className="modal card" onClick={(e) => e.stopPropagation()} onSubmit={save}>
        <div className="modal-head">
          <h2>ویرایش «{user.username}»</h2>
          <button type="button" className="btn ghost" onClick={onClose}>✕</button>
        </div>
        {error && <div className="alert">{error}</div>}
        <div className="form-grid">
          <div>
            <label>📦 حجم (GB) — صفر=نامحدود</label>
            <input
              type="number"
              min="0"
              step="1"
              value={quotaGb}
              onChange={(e) => setQuotaGb(e.target.value)}
              autoFocus
            />
          </div>
          <div>
            <label>⏳ انقضا (روز از حالا) — خالی=بدون تغییر</label>
            <input
              type="number"
              min="0"
              step="1"
              placeholder={`فعلی: ${currentExpiry}`}
              value={expiresDays}
              onChange={(e) => setExpiresDays(e.target.value)}
            />
          </div>
          <div style={{ gridColumn: "1 / -1" }}>
            <label>📝 یادداشت</label>
            <input value={note} onChange={(e) => setNote(e.target.value)} maxLength={255} />
          </div>
        </div>
        <p className="muted sm">
          مصرف فعلی: {Math.round((user.used_bytes / GIB) * 10) / 10} GB · انقضای فعلی: {currentExpiry}
        </p>
        <div className="form-actions">
          <button className="btn primary" disabled={busy}>
            {busy ? "در حال ذخیره…" : "ذخیره"}
          </button>
          <button type="button" className="btn ghost" onClick={onClose}>
            انصراف
          </button>
        </div>
      </form>
    </div>
  );
}
