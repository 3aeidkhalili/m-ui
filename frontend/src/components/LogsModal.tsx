import React, { useEffect, useState } from "react";
import { api } from "../api";
import type { LogEvent } from "../api";

const CATS: { key: string; label: string; icon: string }[] = [
  { key: "all", label: "همه", icon: "📋" },
  { key: "auth", label: "ورود", icon: "🔑" },
  { key: "user", label: "کاربران", icon: "👤" },
  { key: "location", label: "لوکیشن", icon: "🌍" },
  { key: "connection", label: "اتصال", icon: "🛰️" },
  { key: "ip_limit", label: "محدودیت IP", icon: "🛡️" },
  { key: "tarpit", label: "مسدودیت", icon: "⛔" },
  { key: "system", label: "سیستم", icon: "⚙️" },
];

const CAT_META: Record<string, { label: string; icon: string }> = Object.fromEntries(
  CATS.map((c) => [c.key, { label: c.label, icon: c.icon }]),
);

function levelClass(level: string): string {
  if (level === "critical") return "log-lv crit";
  if (level === "warn") return "log-lv warn";
  return "log-lv info";
}

export default function LogsModal({ onClose }: { onClose: () => void }) {
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [cat, setCat] = useState("all");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");

  async function doBackup() {
    setBusy("backup");
    setError("");
    try {
      await api.downloadBackup();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy("");
    }
  }

  async function doRestore(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    if (!confirm(
      `⚠️ بازیابیِ بکاپ «${file.name}»؟\n\n` +
      `کلِ داده‌های فعلیِ پنل با این فایل جایگزین و پنل ری‌استارت می‌شود. این عمل حساس است.`,
    )) return;
    setBusy("restore");
    setError("");
    try {
      const d = await api.restoreBackup(file);
      alert(d.message || "بازیابی شروع شد؛ چند ثانیه بعد دوباره وارد شوید.");
    } catch (err) {
      setError((err as Error).message);
      setBusy("");
    }
  }

  async function load(c = cat) {
    setLoading(true);
    setError("");
    try {
      setLogs(await api.getLogs(c, 400));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load(cat);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cat]);

  async function clearAll() {
    if (!confirm("کل لاگ سیستم پاک شود؟ این عمل قابل بازگشت نیست.")) return;
    try {
      await api.clearLogs();
      await load(cat);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card logs-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>📜 لاگ سیستم</h2>
          <div style={{ display: "flex", gap: 8 }}>
            <button className="btn xs" onClick={() => load(cat)}>↻ تازه‌سازی</button>
            <button className="btn xs danger" onClick={clearAll}>پاک‌کردن همه</button>
            <button className="btn ghost" onClick={onClose}>✕</button>
          </div>
        </div>
        {error && <div className="alert">{error}</div>}

        <div className="backup-box">
          <div className="backup-info">
            <b>💾 بکاپ و بازیابی</b>
            <span className="muted sm">دانلودِ کاملِ دیتابیس (کاربران، تنظیمات، کانفیگ‌ها) یا بازیابی از فایلِ بکاپ.</span>
          </div>
          <div className="backup-actions">
            <button className="btn xs" disabled={busy !== ""} onClick={doBackup}>
              {busy === "backup" ? "در حال دانلود…" : "⬇️ دانلود بکاپ"}
            </button>
            <label className={`btn xs ${busy !== "" ? "off" : ""}`} style={{ cursor: busy ? "default" : "pointer" }}>
              {busy === "restore" ? "در حال بازیابی…" : "♻️ بازیابی"}
              <input type="file" accept=".db,.sqlite,application/octet-stream" hidden disabled={busy !== ""} onChange={doRestore} />
            </label>
          </div>
        </div>

        <div className="log-cats">
          {CATS.map((c) => (
            <button
              key={c.key}
              className={`log-cat ${cat === c.key ? "on" : ""}`}
              onClick={() => setCat(c.key)}
            >
              <span>{c.icon}</span> {c.label}
            </button>
          ))}
        </div>
        <div className="log-list">
          {loading ? (
            <div className="muted sm" style={{ padding: 16 }}>در حال بارگذاری…</div>
          ) : logs.length === 0 ? (
            <div className="muted sm" style={{ padding: 16 }}>لاگی ثبت نشده است.</div>
          ) : (
            logs.map((l) => {
              const m = CAT_META[l.category] || { label: l.category, icon: "•" };
              return (
                <div key={l.id} className="log-row">
                  <span className={levelClass(l.level)} />
                  <span className="log-cat-tag">{m.icon} {m.label}</span>
                  <span className="log-msg">{l.message}</span>
                  <span className="log-actor">{l.actor}</span>
                  <span className="log-time" dir="ltr">
                    {l.created_at ? new Date(l.created_at).toLocaleString("fa-IR") : ""}
                  </span>
                </div>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}
