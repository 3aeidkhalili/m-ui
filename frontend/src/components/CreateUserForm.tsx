import React, { useState } from "react";
import { api } from "../api";

interface CreateUserFormProps {
  onCreated: () => void;
}

export default function CreateUserForm({ onCreated }: CreateUserFormProps) {
  const [open, setOpen] = useState(false);
  const [username, setUsername] = useState("");
  const [quotaGb, setQuotaGb] = useState<string | number>(50);
  const [expiresDays, setExpiresDays] = useState<string | number>(30);
  const [ipLimit, setIpLimit] = useState<string | number>(0);
  const [bandwidth, setBandwidth] = useState<string | number>(0);
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await api.createUser({
        username: username.trim(),
        quota_gb: Number(quotaGb) || 0,
        expires_days: Number(expiresDays) || null,
        ip_limit: Number(ipLimit) || 0,
        bandwidth_mbps: Number(bandwidth) || 0,
        note: note.trim(),
      });
      setUsername("");
      setNote("");
      setOpen(false);
      onCreated();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  if (!open) {
    return (
      <button className="btn primary add-btn" onClick={() => setOpen(true)}>
        + کاربر جدید
      </button>
    );
  }

  return (
    <form className="card create-form" onSubmit={submit}>
      <h2>کاربر جدید</h2>
      <p className="muted sm">
        روی هر سه پروتکل (OpenVPN / WireGuard / L2TP) با یک حجم واحد ساخته می‌شود.
      </p>
      {error && <div className="alert">{error}</div>}
      <div className="form-grid">
        <div>
          <label>👤 نام کاربری</label>
          <input
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            pattern="[A-Za-z0-9_.\-]+"
            title="فقط حروف انگلیسی، اعداد و _ . -"
            required
          />
        </div>
        <div>
          <label>📦 حجم (GB) — صفر=نامحدود</label>
          <input type="number" min="0" step="1" value={quotaGb} onChange={(e) => setQuotaGb(e.target.value)} />
        </div>
        <div>
          <label>⏳ انقضا (روز) — صفر=بدون انقضا</label>
          <input type="number" min="0" step="1" value={expiresDays} onChange={(e) => setExpiresDays(e.target.value)} />
        </div>
        <div>
          <label>🔒 حد IP هم‌زمان — صفر=نامحدود</label>
          <input type="number" min="0" step="1" value={ipLimit} onChange={(e) => setIpLimit(e.target.value)} />
        </div>
        <div>
          <label>⚡ محدودیت سرعت (Mbps) — صفر=نامحدود</label>
          <input type="number" min="0" step="1" value={bandwidth} onChange={(e) => setBandwidth(e.target.value)} />
        </div>
        <div>
          <label>📝 یادداشت</label>
          <input value={note} onChange={(e) => setNote(e.target.value)} />
        </div>
      </div>
      <div className="form-actions">
        <button className="btn primary" disabled={busy}>
          {busy ? "در حال ساخت…" : "ساخت"}
        </button>
        <button type="button" className="btn ghost" onClick={() => setOpen(false)}>
          انصراف
        </button>
      </div>
    </form>
  );
}
