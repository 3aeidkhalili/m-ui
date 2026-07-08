import React, { useEffect, useState } from "react";
import { api } from "../api";

interface Endpoint {
  m: "GET" | "POST" | "PATCH" | "PUT" | "DELETE";
  path: string;
  auth: boolean;
  desc: string;
  body?: string;
  ex: string;
}
interface Group {
  title: string;
  icon: string;
  endpoints: Endpoint[];
}

const BASE = typeof window !== "undefined" ? window.location.origin : "https://SERVER";
const H = `-H "Authorization: Bearer $TOKEN"`;

const GROUPS: Group[] = [
  {
    title: "احراز هویت",
    icon: "🔑",
    endpoints: [
      { m: "POST", path: "/api/auth/login", auth: false, desc: "ورود ادمین و دریافت توکن (JWT، اعتبار ۸ ساعت)", body: "form: username, password", ex: `curl -sk -X POST ${BASE}/api/auth/login \\\n  -d 'username=admin&password=YOUR_PASS'\n# → {"access_token":"eyJ...","token_type":"bearer"}\nTOKEN=eyJ...` },
      { m: "GET", path: "/api/auth/me", auth: true, desc: "اطلاعات ادمینِ واردشده", ex: `curl -sk ${BASE}/api/auth/me ${H}` },
      { m: "POST", path: "/api/auth/change-password", auth: true, desc: "تغییر رمز ادمین (همه‌ی توکن‌های قبلی باطل می‌شوند)", body: "{current_password, new_password}", ex: `curl -sk -X POST ${BASE}/api/auth/change-password ${H} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"current_password":"old","new_password":"NewStrongPass1"}'` },
    ],
  },
  {
    title: "سیستم و مانیتور",
    icon: "📊",
    endpoints: [
      { m: "GET", path: "/api/health", auth: false, desc: "بررسی سلامت سرویس", ex: `curl -sk ${BASE}/api/health` },
      { m: "GET", path: "/api/system/status", auth: true, desc: "شمار کاربران فعال/کل، مصرف کل، وضعیت سرویس‌ها", ex: `curl -sk ${BASE}/api/system/status ${H}` },
      { m: "GET", path: "/api/system/protocols", auth: true, desc: "آمار زندهٔ هر پروتکل (آنلاین/سرعت/حجم) + خروج Xray", ex: `curl -sk ${BASE}/api/system/protocols ${H}` },
    ],
  },
  {
    title: "کاربران",
    icon: "👤",
    endpoints: [
      { m: "GET", path: "/api/users", auth: true, desc: "لیست کاربران با مصرف/حد IP/سرعت/وضعیت", ex: `curl -sk ${BASE}/api/users ${H}` },
      { m: "POST", path: "/api/users", auth: true, desc: "ساخت کاربر روی هر سه پروتکل", body: "{username, quota_gb, expires_days, note, ip_limit, bandwidth_mbps}", ex: `curl -sk -X POST ${BASE}/api/users ${H} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"username":"ali","quota_gb":50,"expires_days":30,"ip_limit":2,"bandwidth_mbps":20,"note":""}'` },
      { m: "GET", path: "/api/users/{id}", auth: true, desc: "جزئیات یک کاربر", ex: `curl -sk ${BASE}/api/users/1 ${H}` },
      { m: "PATCH", path: "/api/users/{id}", auth: true, desc: "ویرایش (هر فیلد اختیاری)", body: "{quota_gb?, expires_days?, enabled?, note?, ip_limit?, bandwidth_mbps?}", ex: `curl -sk -X PATCH ${BASE}/api/users/1 ${H} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"quota_gb":100,"bandwidth_mbps":50}'` },
      { m: "DELETE", path: "/api/users/{id}", auth: true, desc: "حذف کاربر از همه‌ی پروتکل‌ها", ex: `curl -sk -X DELETE ${BASE}/api/users/1 ${H}` },
      { m: "POST", path: "/api/users/{id}/reset", auth: true, desc: "صفرکردن مصرف + پاک‌کردن هشدارها و strikeها", ex: `curl -sk -X POST ${BASE}/api/users/1/reset ${H}` },
      { m: "POST", path: "/api/users/{id}/rotate-sub", auth: true, desc: "چرخش توکن اشتراک (لینک قبلی باطل)", ex: `curl -sk -X POST ${BASE}/api/users/1/rotate-sub ${H}` },
      { m: "GET", path: "/api/users/{id}/configs", auth: true, desc: "کانفیگ‌های کلاینت (ovpn/wg/l2tp)", ex: `curl -sk ${BASE}/api/users/1/configs ${H}` },
    ],
  },
  {
    title: "تنظیمات و پروتکل‌ها",
    icon: "⚙️",
    endpoints: [
      { m: "GET", path: "/api/settings", auth: true, desc: "تنظیمات سایت (fields + values)", ex: `curl -sk ${BASE}/api/settings ${H}` },
      { m: "PATCH", path: "/api/settings", auth: true, desc: "به‌روزرسانی تنظیمات (فقط کلیدهای مجاز)", body: "{key: value, ...}", ex: `curl -sk -X PATCH ${BASE}/api/settings ${H} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"default_ip_limit":"2","default_bandwidth_mbps":"20"}'` },
      { m: "GET", path: "/api/protocols", auth: true, desc: "تنظیمات پروتکل‌ها (remote/port/cipher/…)", ex: `curl -sk ${BASE}/api/protocols ${H}` },
      { m: "PATCH", path: "/api/protocols", auth: true, desc: "به‌روزرسانی تنظیمات پروتکل", body: "{key: value, ...}", ex: `curl -sk -X PATCH ${BASE}/api/protocols ${H} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"ovpn_proto":"udp","ovpn_port":"2096"}'` },
    ],
  },
  {
    title: "لوکیشن‌ها (اوتباند)",
    icon: "🌍",
    endpoints: [
      { m: "GET", path: "/api/outbounds", auth: true, desc: "لیست اوتباندها + گروه فعال", ex: `curl -sk ${BASE}/api/outbounds ${H}` },
      { m: "POST", path: "/api/outbounds", auth: true, desc: "افزودن اوتباند از لینک/کانفیگ", body: "{name, config}", ex: `curl -sk -X POST ${BASE}/api/outbounds ${H} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"name":"DE","config":"vless://..."}'` },
      { m: "POST", path: "/api/outbounds/parse", auth: true, desc: "تجزیهٔ کانفیگ بدون ذخیره", body: "{config}", ex: `curl -sk -X POST ${BASE}/api/outbounds/parse ${H} -d '{"config":"vmess://..."}'` },
      { m: "PATCH", path: "/api/outbounds/{id}", auth: true, desc: "ویرایش نام/کانفیگ", ex: `curl -sk -X PATCH ${BASE}/api/outbounds/1 ${H} -d '{"name":"NL"}'` },
      { m: "POST", path: "/api/outbounds/{id}/test", auth: true, desc: "تست اتصال + ژئولوکیشن خروجی", ex: `curl -sk -X POST ${BASE}/api/outbounds/1/test ${H}` },
      { m: "POST", path: "/api/outbounds/{id}/activate", auth: true, desc: "فعال‌سازی یک اوتباند", ex: `curl -sk -X POST ${BASE}/api/outbounds/1/activate ${H}` },
      { m: "POST", path: "/api/outbounds/activate-all", auth: true, desc: "فعال‌سازی همه (بالانسر round-robin)", ex: `curl -sk -X POST ${BASE}/api/outbounds/activate-all ${H}` },
      { m: "POST", path: "/api/outbounds/direct", auth: true, desc: "خروج مستقیم (بدون رله)", ex: `curl -sk -X POST ${BASE}/api/outbounds/direct ${H}` },
      { m: "DELETE", path: "/api/outbounds/{id}", auth: true, desc: "حذف اوتباند", ex: `curl -sk -X DELETE ${BASE}/api/outbounds/1 ${H}` },
    ],
  },
  {
    title: "آموزش/دانلود و لاگ",
    icon: "📚",
    endpoints: [
      { m: "GET", path: "/api/resources", auth: true, desc: "لیست منابع (راهنما/دانلود)", ex: `curl -sk ${BASE}/api/resources ${H}` },
      { m: "POST", path: "/api/resources", auth: true, desc: "افزودن منبع", ex: `curl -sk -X POST ${BASE}/api/resources ${H} -d '{"title":"...","url":"https://..."}'` },
      { m: "PATCH", path: "/api/resources/{id}", auth: true, desc: "ویرایش منبع", ex: `curl -sk -X PATCH ${BASE}/api/resources/1 ${H} -d '{"enabled":false}'` },
      { m: "DELETE", path: "/api/resources/{id}", auth: true, desc: "حذف منبع", ex: `curl -sk -X DELETE ${BASE}/api/resources/1 ${H}` },
      { m: "GET", path: "/api/logs", auth: true, desc: "لاگ سیستم (category=all|auth|user|location|connection|ip_limit|tarpit|system, limit)", ex: `curl -sk "${BASE}/api/logs?category=all&limit=100" ${H}` },
      { m: "DELETE", path: "/api/logs", auth: true, desc: "پاک‌کردن کل لاگ", ex: `curl -sk -X DELETE ${BASE}/api/logs ${H}` },
    ],
  },
  {
    title: "اشتراک (عمومی، token-gated)",
    icon: "🔗",
    endpoints: [
      { m: "GET", path: "/sub/{token}", auth: false, desc: "صفحهٔ اشتراک کاربر (HTML)", ex: `${BASE}/sub/USER_SUB_TOKEN` },
      { m: "GET", path: "/api/sub/{token}", auth: false, desc: "وضعیت اشتراک (JSON): مصرف/حجم/کانفیگ‌ها/لوکیشن", ex: `curl -sk ${BASE}/api/sub/USER_SUB_TOKEN` },
      { m: "GET", path: "/api/sub/{token}/locations", auth: false, desc: "لوکیشن‌های در دسترس + انتخاب فعلی", ex: `curl -sk ${BASE}/api/sub/USER_SUB_TOKEN/locations` },
      { m: "POST", path: "/api/sub/{token}/location", auth: false, desc: "انتخاب لوکیشن خروج توسط کاربر", body: "{outbound_id: number|0}", ex: `curl -sk -X POST ${BASE}/api/sub/USER_SUB_TOKEN/location \\\n  -H 'Content-Type: application/json' -d '{"outbound_id":2}'` },
      { m: "GET", path: "/sub/{token}/config/{proto}", auth: false, desc: "دانلود کانفیگ (proto: openvpn|wireguard|l2tp)", ex: `curl -sk ${BASE}/sub/USER_SUB_TOKEN/config/wireguard` },
    ],
  },
];

const methodClass: Record<string, string> = {
  GET: "m-get", POST: "m-post", PATCH: "m-patch", PUT: "m-patch", DELETE: "m-del",
};

export default function ApiDocsModal({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState<string | null>(null);
  const [notes, setNotes] = useState("");
  const [savedNote, setSavedNote] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api.getApiNotes().then((d) => { setNotes(d.notes || ""); setSavedNote(d.notes || ""); }).catch(() => {});
  }, []);

  async function saveNotes() {
    setSaving(true);
    try {
      const d = await api.setApiNotes(notes);
      setSavedNote(d.notes);
    } catch { /* ignore */ } finally { setSaving(false); }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card api-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>🧩 مستندات API</h2>
          <button className="btn ghost" onClick={onClose}>✕</button>
        </div>
        <p className="muted sm">
          همهٔ endpointهای دارای 🔒 به هدر <code>Authorization: Bearer &lt;token&gt;</code> نیاز دارند.
          ابتدا از <code>/api/auth/login</code> توکن بگیرید. پاسخ‌ها JSON هستند.
        </p>

        <div className="api-groups">
          {GROUPS.map((g) => (
            <div key={g.title} className="api-group">
              <h3>{g.icon} {g.title}</h3>
              {g.endpoints.map((e) => {
                const id = e.m + e.path;
                const isOpen = open === id;
                return (
                  <div key={id} className={`api-ep ${isOpen ? "open" : ""}`}>
                    <div className="api-ep-row" onClick={() => setOpen(isOpen ? null : id)}>
                      <span className={`api-m ${methodClass[e.m]}`}>{e.m}</span>
                      <code className="api-path">{e.path}</code>
                      <span className="api-lock">{e.auth ? "🔒" : "🌐"}</span>
                      <span className="api-desc">{e.desc}</span>
                    </div>
                    {isOpen && (
                      <div className="api-ex">
                        {e.body && <div className="api-body">بدنه: <code>{e.body}</code></div>}
                        <pre>{e.ex}</pre>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>

        <div className="api-ideas">
          <h3>💡 ایده‌ها برای افزایش امنیت API</h3>
          <p className="muted sm">یادداشت‌های امنیتیِ شما اینجا ذخیره می‌شود (فقط برای ادمین).</p>
          <textarea
            rows={4}
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            placeholder="مثلاً: محدودیت per-account برای لاگین، mTLS برای API، چرخش دوره‌ای SECRET_KEY، IP allowlist برای پنل ادمین…"
          />
          <div className="form-actions">
            <button className="btn primary" disabled={saving || notes === savedNote} onClick={saveNotes}>
              {saving ? "در حال ذخیره…" : notes === savedNote ? "ذخیره‌شده ✓" : "ذخیره ایده‌ها"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
