import React, { useEffect, useState, useCallback, useRef } from "react";
import { api } from "../api";
import type { SystemStatus, User, ProtocolStats, ProtocolStat, EgressInfo, ResourceStats } from "../api";
import { fmtBytes, fmtRate } from "../format";
import ConfigModal from "../components/ConfigModal";
import EditUserModal from "../components/EditUserModal";
import LogsModal from "../components/LogsModal";
import ApiDocsModal from "../components/ApiDocsModal";
import CreateUserForm from "../components/CreateUserForm";
import ChangePasswordModal from "../components/ChangePasswordModal";
import KeyValueSettings from "../components/KeyValueSettings";
import OutboundModal from "../components/OutboundModal";
import ResourcesModal from "../components/ResourcesModal";
import ProtocolModal, { protoMeta } from "../components/ProtocolModal";
import type { RateSample } from "../components/ProtocolModal";

const STATUS_LABEL: Record<string, string> = {
  active: "فعال",
  disabled: "غیرفعال",
  expired: "منقضی",
  limited: "اتمام حجم",
};

type ByRate = Record<string, { rx: number; tx: number }>;

interface DashboardProps {
  onLogout: () => void;
}

export default function Dashboard({ onLogout }: DashboardProps) {
  const [status, setStatus] = useState<SystemStatus | null>(null);
  const [users, setUsers] = useState<User[]>([]);
  const [error, setError] = useState("");
  const [configUser, setConfigUser] = useState<User | null>(null);
  const [editUser, setEditUser] = useState<User | null>(null);
  const [showPwd, setShowPwd] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [showProtocols, setShowProtocols] = useState(false);
  const [showOutbounds, setShowOutbounds] = useState(false);
  const [showResources, setShowResources] = useState(false);
  const [showLogs, setShowLogs] = useState(false);
  const [showApi, setShowApi] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [siteSettings, setSiteSettings] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);

  // مانیتور پروتکل‌ها: آخرین آمار، نرخ لحظه‌ای و تاریخچه‌ی اسپارک‌لاین
  const [proto, setProto] = useState<ProtocolStats | null>(null);
  const [rates, setRates] = useState<Record<string, RateSample>>({});
  const [openProto, setOpenProto] = useState<string | null>(null); // کلید پروتکلِ بازِ مدال
  const prevRef = useRef<{ t: number; by: ByRate }>({ t: 0, by: {} });
  const histRef = useRef<Record<string, RateSample[]>>({});

  // مانیتور منابع سرور
  const [res, setRes] = useState<ResourceStats | null>(null);
  // کنترل‌های جدول کاربران: جستجو، فیلتر وضعیت، صفحه‌بندی، انتخاب گروهی
  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<"all" | "blocked" | "warned" | "limited" | "expired">("all");
  const [pageSize, setPageSize] = useState(10);
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const refresh = useCallback(async () => {
    try {
      const [st, us] = await Promise.all([api.status(), api.listUsers()]);
      setStatus(st);
      setUsers(us);
      setError("");
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshProto = useCallback(async () => {
    api.getResourceStats().then(setRes).catch(() => {});
    try {
      const d = await api.protocolStats();
      const now = Date.now();
      const prev = prevRef.current;
      const nr: Record<string, RateSample> = {};
      const dt = prev.t ? (now - prev.t) / 1000 : 0;
      for (const p of d.protocols) {
        const before = prev.by[p.key];
        if (before && dt > 0.5) {
          const rx = Math.max(0, (p.rx_bytes - before.rx) / dt);
          const tx = Math.max(0, (p.tx_bytes - before.tx) / dt);
          nr[p.key] = { rx, tx, total: rx + tx };
          const h = histRef.current[p.key] || [];
          h.push({ rx, tx, total: rx + tx });
          if (h.length > 40) h.shift();
          histRef.current[p.key] = h;
        }
      }
      prevRef.current = {
        t: now,
        by: Object.fromEntries(d.protocols.map((p) => [p.key, { rx: p.rx_bytes, tx: p.tx_bytes }])) as ByRate,
      };
      if (Object.keys(nr).length) setRates((r) => ({ ...r, ...nr }));
      setProto(d);
    } catch {
      /* آمار پروتکل بحرانی نیست؛ خطا را بی‌صدا رد کن */
    }
  }, []);

  useEffect(() => {
    refresh();
    refreshProto();
    api.getSettings().then((d) => setSiteSettings(d.values || {})).catch(() => {});
    const t = setInterval(refresh, 10000);
    const tp = setInterval(refreshProto, 5000);
    return () => {
      clearInterval(t);
      clearInterval(tp);
    };
  }, [refresh, refreshProto]);

  async function toggle(user: User) {
    try {
      await api.updateUser(user.id, { enabled: !user.enabled });
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  }
  async function reset(user: User) {
    try {
      await api.resetUser(user.id);
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  }
  async function remove(user: User) {
    if (!confirm(`حذف کاربر «${user.username}»؟`)) return;
    try {
      await api.deleteUser(user.id);
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  }
  async function editIpLimit(user: User) {
    const ans = prompt(
      `حد IP هم‌زمان برای «${user.username}» (۰ = نامحدود):`,
      String(user.ip_limit ?? 0),
    );
    if (ans === null) return;
    const n = Math.max(0, Math.floor(Number(ans)));
    if (!Number.isFinite(n)) return;
    try {
      await api.updateUser(user.id, { ip_limit: n });
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  }
  async function editBandwidth(user: User) {
    const ans = prompt(
      `محدودیت سرعت دانلود برای «${user.username}» (Mbps، ۰ = نامحدود):`,
      String(user.bandwidth_mbps ?? 0),
    );
    if (ans === null) return;
    const n = Math.max(0, Math.floor(Number(ans)));
    if (!Number.isFinite(n)) return;
    try {
      await api.updateUser(user.id, { bandwidth_mbps: n });
      await refresh();
    } catch (err) {
      setError((err as Error).message);
    }
  }

  // ---- users: search + status filter + pagination ----
  const filtered = users.filter((u) => {
    const q = search.trim().toLowerCase();
    if (q && !(u.username.toLowerCase().includes(q) || (u.note || "").toLowerCase().includes(q))) return false;
    switch (filter) {
      case "blocked": return !u.enabled;
      case "warned": return (u.strikes ?? 0) > 0;
      case "limited": return u.status === "limited";
      case "expired": return u.status === "expired";
      default: return true;
    }
  });
  const totalPages = Math.max(1, Math.ceil(filtered.length / pageSize));
  const curPage = Math.min(page, totalPages);
  const pageUsers = filtered.slice((curPage - 1) * pageSize, curPage * pageSize);
  const filterCounts = {
    all: users.length,
    blocked: users.filter((u) => !u.enabled).length,
    warned: users.filter((u) => (u.strikes ?? 0) > 0).length,
    limited: users.filter((u) => u.status === "limited").length,
    expired: users.filter((u) => u.status === "expired").length,
  };

  function toggleSelect(id: number) {
    setSelected((s) => {
      const n = new Set(s);
      n.has(id) ? n.delete(id) : n.add(id);
      return n;
    });
  }
  function selectAllOnPage(check: boolean) {
    setSelected((s) => {
      const n = new Set(s);
      pageUsers.forEach((u) => (check ? n.add(u.id) : n.delete(u.id)));
      return n;
    });
  }
  async function bulkSetEnabled(enabled: boolean) {
    const ids = [...selected];
    if (!ids.length) return;
    if (!confirm(`${enabled ? "فعال‌سازی" : "مسدودسازی"} ${ids.length} کاربر؟`)) return;
    try {
      await Promise.all(ids.map((id) => api.updateUser(id, { enabled })));
      setSelected(new Set());
      await refresh();
    } catch (err) { setError((err as Error).message); }
  }
  async function bulkDelete() {
    const ids = [...selected];
    if (!ids.length) return;
    if (!confirm(`حذفِ ${ids.length} کاربر؟ این عمل قابل بازگشت نیست.`)) return;
    try {
      await Promise.all(ids.map((id) => api.deleteUser(id)));
      setSelected(new Set());
      await refresh();
    } catch (err) { setError((err as Error).message); }
  }

  const allOnPageSelected = pageUsers.length > 0 && pageUsers.every((u) => selected.has(u.id));

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">{siteSettings?.panel_title || "MultiVPN"}</div>
        <button
          className="btn ghost menu-toggle"
          onClick={() => setMenuOpen((o) => !o)}
          aria-label="منو"
        >
          ☰
        </button>
        <div
          className={`topbar-actions ${menuOpen ? "open" : ""}`}
          onClick={() => setMenuOpen(false)}
        >
          <button className="btn ghost" onClick={() => setShowOutbounds(true)}>اوتباند</button>
          <button className="btn ghost" onClick={() => setShowResources(true)}>آموزش/دانلود</button>
          <button className="btn ghost" onClick={() => setShowLogs(true)}>📜 لاگ سیستم</button>
          <button className="btn ghost" onClick={() => setShowApi(true)}>🧩 API</button>
          <button className="btn ghost" onClick={() => setShowProtocols(true)}>تنظیمات پروتکل</button>
          <button className="btn ghost" onClick={() => setShowSettings(true)}>تنظیمات</button>
          <button className="btn ghost" onClick={() => setShowPwd(true)}>تغییر رمز</button>
          <button className="btn ghost" onClick={onLogout}>خروج</button>
        </div>
      </header>

      {error && <div className="alert container">{error}</div>}

      <div className="container">
        <ResourceMonitor res={res} />

        <div className="stats-row">
          <StatCard icon="👥" label="کاربران" value={status?.users_total ?? "—"} />
          <StatCard icon="🟢" label="فعال" value={status?.users_active ?? "—"} />
          <StatCard icon="📊" label="مصرف کل" value={fmtBytes(status?.total_used_bytes)} />
          <StatCard icon="🖥️" label="سرور" value={status?.server_ip ?? "—"} small />
        </div>

        <ProtocolMonitor
          proto={proto}
          rates={rates}
          services={status?.services}
          onOpen={(key) => setOpenProto(key)}
        />

        <CreateUserForm onCreated={refresh} />

        <div className="card">
          <div className="users-head">
            <h2>👥 کاربران</h2>
            <input
              className="user-search"
              placeholder="🔍 جستجوی نام کاربری یا یادداشت…"
              value={search}
              onChange={(e) => { setSearch(e.target.value); setPage(1); }}
            />
          </div>

          <div className="user-filters">
            {([
              ["all", "📋 همه", filterCounts.all],
              ["blocked", "⛔ مسدود", filterCounts.blocked],
              ["warned", "⚠️ هشدار", filterCounts.warned],
              ["limited", "📦 اتمام حجم", filterCounts.limited],
              ["expired", "⏳ منقضی", filterCounts.expired],
            ] as const).map(([key, label, count]) => (
              <button
                key={key}
                className={`ufilter ${filter === key ? "on" : ""}`}
                onClick={() => { setFilter(key); setPage(1); }}
              >
                {label} <span className="ufc">{count}</span>
              </button>
            ))}
            <div className="user-pagesize">
              <span className="muted sm">نمایش:</span>
              {[10, 20, 60, 100].map((n) => (
                <button key={n} className={`ps ${pageSize === n ? "on" : ""}`}
                  onClick={() => { setPageSize(n); setPage(1); }}>{n}</button>
              ))}
            </div>
          </div>

          {selected.size > 0 && (
            <div className="bulk-bar">
              <span>✅ {selected.size} کاربر انتخاب شد</span>
              <div className="bulk-actions">
                <button className="btn xs" onClick={() => bulkSetEnabled(true)}>🟢 فعال‌سازی</button>
                <button className="btn xs" onClick={() => bulkSetEnabled(false)}>⛔ مسدودسازی</button>
                <button className="btn xs danger" onClick={bulkDelete}>🗑️ حذف</button>
                <button className="btn xs ghost" onClick={() => setSelected(new Set())}>لغو انتخاب</button>
              </div>
            </div>
          )}

          {loading ? (
            <p className="muted">در حال بارگذاری…</p>
          ) : filtered.length === 0 ? (
            <p className="muted">{users.length === 0 ? "هنوز کاربری ساخته نشده." : "کاربری با این فیلتر یافت نشد."}</p>
          ) : (
            <>
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th className="chk-col">
                        <input type="checkbox" checked={allOnPageSelected}
                          onChange={(e) => selectAllOnPage(e.target.checked)} />
                      </th>
                      <th>#</th>
                      <th>کاربر</th>
                      <th>وضعیت</th>
                      <th>مصرف / سهمیه</th>
                      <th>انقضا</th>
                      <th>عملیات</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageUsers.map((u) => (
                      <UserRow
                        key={u.id}
                        user={u}
                        selected={selected.has(u.id)}
                        onSelect={() => toggleSelect(u.id)}
                        onToggle={() => toggle(u)}
                        onReset={() => reset(u)}
                        onRemove={() => remove(u)}
                        onConfigs={() => setConfigUser(u)}
                        onEdit={() => setEditUser(u)}
                        onEditIpLimit={() => editIpLimit(u)}
                        onEditBandwidth={() => editBandwidth(u)}
                      />
                    ))}
                  </tbody>
                </table>
              </div>

              {totalPages > 1 && (
                <div className="pager">
                  <button className="btn xs" disabled={curPage <= 1} onClick={() => setPage(curPage - 1)}>‹ قبلی</button>
                  <span className="muted sm">صفحه {curPage} از {totalPages} · {filtered.length} کاربر</span>
                  <button className="btn xs" disabled={curPage >= totalPages} onClick={() => setPage(curPage + 1)}>بعدی ›</button>
                </div>
              )}
            </>
          )}
        </div>
      </div>

      {configUser && (
        <ConfigModal
          user={configUser}
          baseUrl={siteSettings?.subscription_base_url}
          onClose={() => setConfigUser(null)}
          onRefresh={refresh}
        />
      )}
      {editUser && (
        <EditUserModal
          user={editUser}
          onClose={() => setEditUser(null)}
          onSaved={refresh}
        />
      )}
      {showPwd && <ChangePasswordModal onClose={() => setShowPwd(false)} />}
      {showSettings && (
        <KeyValueSettings
          title="تنظیمات سایت"
          loader={api.getSettings}
          saver={api.updateSettings}
          onClose={() => setShowSettings(false)}
          onSaved={(v) => setSiteSettings(v)}
        />
      )}
      {showProtocols && (
        <KeyValueSettings
          title="تنظیمات پروتکل"
          hint="تغییر این مقادیر بلافاصله روی کانفیگی که کاربر از صفحه‌ی اشتراک می‌گیرد اعمال می‌شود (بدون نیاز به ساخت مجدد کاربر)."
          loader={api.getProtocols}
          saver={api.updateProtocols}
          onClose={() => setShowProtocols(false)}
        />
      )}
      {showOutbounds && <OutboundModal onClose={() => setShowOutbounds(false)} />}
      {showResources && <ResourcesModal onClose={() => setShowResources(false)} />}
      {showLogs && <LogsModal onClose={() => setShowLogs(false)} />}
      {showApi && <ApiDocsModal onClose={() => setShowApi(false)} />}
      {openProto && proto && (() => {
        const data = proto.protocols.find((p) => p.key === openProto);
        if (!data) return null;
        const denom = proto.protocols
          .filter((p) => p.key !== "xray" && p.key !== "strongswan")
          .reduce((s, p) => s + (p.total_bytes || 0), 0);
        const share =
          data.key === "xray" ? 100 : denom > 0 ? ((data.total_bytes || 0) / denom) * 100 : 0;
        return (
          <ProtocolModal
            data={data}
            rate={rates[data.key]}
            history={histRef.current[data.key]}
            share={share}
            onClose={() => setOpenProto(null)}
          />
        );
      })()}
    </div>
  );
}

interface ProtoCardData {
  key: string;
  service?: string;
  status: string;
  online: number;
  total_bytes: number;
  egress?: EgressInfo;
}

// خلاصه‌ی لوکیشنِ خروج برای کارت Xray (لایه‌ی ترانزیت)
function egressSummary(e: EgressInfo): { flag: React.ReactNode; label: string; sub: string } {
  if (e.mode === "direct")
    return { flag: <span className="pc-flag-emoji">🌐</span>, label: "مستقیم", sub: "بدون رله (freedom)" };
  if (e.mode === "balancer")
    return { flag: <span className="pc-flag-emoji">⚖️</span>, label: "بالانسر", sub: `${e.pool_size} لوکیشن` };
  const loc = e.locations[0];
  const flag = loc?.country_code
    ? <img className="pc-flag" src={loc.flag} alt="" />
    : <span className="pc-flag-emoji">🚩</span>;
  return { flag, label: loc?.country_name || loc?.name || "رله", sub: loc?.egress_ip || loc?.address || "" };
}

interface ProtocolMonitorProps {
  proto: ProtocolStats | null;
  rates: Record<string, RateSample>;
  services?: Record<string, string>;
  onOpen: (key: string) => void;
}

function ProtocolMonitor({ proto, rates, services, onOpen }: ProtocolMonitorProps) {
  // پیش از بارگذاری آمار، از کلیدهای سرویسِ status برای اسکلت استفاده کن
  const list: ProtoCardData[] =
    proto?.protocols ||
    (services
      ? Object.keys(services).map((svc) => ({
          key: svcToKey(svc),
          service: svc,
          status: services[svc],
          online: 0,
          total_bytes: 0,
        }))
      : []);
  if (list.length === 0) return null;

  const aggRate = Object.values(rates).reduce((s, r) => s + (r?.total || 0), 0)
    - (rates.xray?.total || 0); // xray تجمیعی است؛ دوباره نشمار
  const online = proto?.total_online ?? 0;

  return (
    <div className="card proto-monitor">
      <div className="pm-head">
        <h2>مانیتور پروتکل‌ها</h2>
        <div className="pm-summary">
          {proto?.demo && <span className="badge demo-badge">داده‌ی نمایشی</span>}
          <span className="pm-metric"><span className="pulse-dot" /> {online} نشست آنلاین</span>
          <span className="pm-metric live">⇅ {fmtRate(Math.max(0, aggRate))}</span>
        </div>
      </div>
      <p className="muted sm pm-hint">روی هر پروتکل بزنید تا مصرف، سرعت لحظه‌ای و کاربران آنلاین را ببینید.</p>
      <div className="proto-grid">
        {list.map((p) => (
          <ProtoCard key={p.key} p={p} rate={rates[p.key]} onOpen={() => onOpen(p.key)} />
        ))}
      </div>
    </div>
  );
}

function svcToKey(svc: string): string {
  if (svc.startsWith("wg-quick")) return "wireguard";
  if (svc.startsWith("openvpn")) return "openvpn";
  if (svc.startsWith("xl2tpd")) return "l2tp";
  if (svc.startsWith("strongswan")) return "strongswan";
  return "xray";
}

interface ProtoCardProps {
  p: ProtoCardData;
  rate?: RateSample;
  onOpen: () => void;
}

function ProtoCard({ p, rate, onOpen }: ProtoCardProps) {
  const meta = protoMeta(p.key);
  const active = p.status === "active";
  const dotClass = active ? "ok" : p.status === "inactive" || p.status === "n/a" ? "na" : "bad";
  const rt = rate?.total || 0;
  // Xray is the transit layer: its card shows the egress/outbound, not usage.
  const eg = p.egress;
  const es = eg ? egressSummary(eg) : null;
  return (
    <button
      className={`proto-card ${active ? "" : "off"} ${es ? "egress" : ""}`}
      style={{ "--acc": meta.accent } as React.CSSProperties}
      onClick={onOpen}
      title={es ? "لوکیشن‌های خروج (اوتباند)" : "مشاهده‌ی جزئیات"}
    >
      <div className="pc-top">
        <span className="pc-icon">{meta.icon}</span>
        <span className={`svc-dot ${dotClass}`} />
      </div>
      <div className="pc-name">{p.key === "xray" ? "Xray — خروج" : meta.title}</div>
      {es ? (
        <div className="pc-egress">
          {es.flag}
          <div className="pc-egress-txt">
            <b>{es.label}</b>
            <span className="muted sm" dir="ltr">{es.sub}</span>
          </div>
        </div>
      ) : (
        <div className="pc-stats">
          <div className="pc-online">
            <b>{p.online}</b>
            <span className="muted sm">آنلاین</span>
          </div>
          <div className="pc-rate">{active ? fmtRate(rt) : "—"}</div>
        </div>
      )}
      <div className="pc-total sm">
        {p.key === "strongswan"
          ? "لایه‌ی رمزنگاری"
          : es
          ? `عبور کل: ${fmtBytes(p.total_bytes)}`
          : fmtBytes(p.total_bytes)}
      </div>
      {active && rt > 0 && <span className="pc-spark" style={{ width: `${Math.min(100, Math.log10(rt + 1) * 14)}%` }} />}
    </button>
  );
}

interface StatCardProps {
  icon: string;
  label: string;
  value: string | number;
  small?: boolean;
}

function StatCard({ icon, label, value, small }: StatCardProps) {
  return (
    <div className="card stat">
      <div className="stat-ico">{icon}</div>
      <div className="stat-body">
        <div className="stat-label">{label}</div>
        <div className={`stat-value ${small ? "sm" : ""}`}>{value}</div>
      </div>
    </div>
  );
}

function bps(n: number): string {
  const bits = n * 8;
  if (bits >= 1e9) return (bits / 1e9).toFixed(1) + " Gب/ث";
  if (bits >= 1e6) return (bits / 1e6).toFixed(1) + " Mب/ث";
  if (bits >= 1e3) return (bits / 1e3).toFixed(0) + " Kب/ث";
  return Math.round(bits) + " ب/ث";
}
function fmtUptime(sec: number): string {
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d} روز ${h} ساعت`;
  if (h > 0) return `${h} ساعت ${m} دقیقه`;
  return `${m} دقیقه`;
}

function Gauge({ icon, label, pct, sub, hue }: { icon: string; label: string; pct: number; sub: string; hue: number }) {
  const p = Math.max(0, Math.min(100, pct));
  const state = p >= 90 ? "crit" : p >= 70 ? "warn" : "ok";
  return (
    <div className={`res-gauge ${state}`} style={{ "--hue": hue } as React.CSSProperties}>
      <div className="res-ring">
        <svg viewBox="0 0 44 44">
          <circle className="rg-track" cx="22" cy="22" r="18" />
          <circle className="rg-prog" cx="22" cy="22" r="18"
            style={{ strokeDashoffset: 113 - (113 * p) / 100 }} />
        </svg>
        <span className="rg-ico">{icon}</span>
      </div>
      <div className="res-info">
        <div className="res-label">{label}</div>
        <div className="res-pct">{Math.round(p)}<small>٪</small></div>
        <div className="res-sub">{sub}</div>
      </div>
    </div>
  );
}

function ResourceMonitor({ res }: { res: ResourceStats | null }) {
  const cpu = res?.cpu_pct ?? 0;
  const mem = res?.mem_pct ?? 0;
  const disk = res?.disk_pct ?? 0;
  const net = res ? res.net_rx_bps + res.net_tx_bps : 0;
  // network has no natural %, so scale to a soft 100 Mbps reference for the ring
  const netPct = Math.min(100, (net * 8) / 1e8 * 100);
  return (
    <div className="res-monitor">
      <Gauge icon="🧠" label="پردازنده" pct={cpu} hue={205}
        sub={res ? `${res.cores} هسته` : "—"} />
      <Gauge icon="💾" label="حافظه (RAM)" pct={mem} hue={265}
        sub={res ? `${fmtBytes(res.mem_used)} / ${fmtBytes(res.mem_total)}` : "—"} />
      <Gauge icon="🗄️" label="دیسک" pct={disk} hue={150}
        sub={res ? `${fmtBytes(res.disk_used)} / ${fmtBytes(res.disk_total)}` : "—"} />
      <Gauge icon="🌐" label="شبکه" pct={netPct} hue={330}
        sub={res ? `↓${bps(res.net_rx_bps)} ↑${bps(res.net_tx_bps)}` : "—"} />
    </div>
  );
}

interface UserRowProps {
  user: User;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
  onReset: () => void;
  onRemove: () => void;
  onConfigs: () => void;
  onEdit: () => void;
  onEditIpLimit: () => void;
  onEditBandwidth: () => void;
}

function UserRow({ user, selected, onSelect, onToggle, onReset, onRemove, onConfigs, onEdit, onEditIpLimit, onEditBandwidth }: UserRowProps) {
  const quota = Number(user.quota_bytes || 0);
  const used = Number(user.used_bytes || 0);
  const pct = quota > 0 ? Math.min(100, (used / quota) * 100) : 0;
  const barClass = pct >= 90 ? "danger" : pct >= 70 ? "warn" : "ok";

  return (
    <tr className={`${user.is_active ? "" : "dim"} ${selected ? "row-sel" : ""}`}>
      <td className="chk-col"><input type="checkbox" checked={selected} onChange={onSelect} /></td>
      <td data-label="#">{user.index}</td>
      <td data-label="کاربر">
        <strong>{user.username}</strong>
        {user.note && <div className="muted sm">{user.note}</div>}
        <div className="muted sm">
          حد IP: {user.ip_limit > 0 ? user.ip_limit : "∞"}
          {" · "}سرعت: {user.bandwidth_mbps > 0 ? `${user.bandwidth_mbps}Mbps` : "∞"}
          {user.strikes > 0 && (
            <span style={{ color: "#f59e0b" }}> · ⚠️ {user.strikes}/3</span>
          )}
        </div>
      </td>
      <td data-label="وضعیت">
        <span className={`badge ${user.status}`}>
          {STATUS_LABEL[user.status] || user.status}
        </span>
      </td>
      <td className="usage-cell" data-label="مصرف / سهمیه">
        <div className="usage-text">
          <span>{fmtBytes(used)} / {quota > 0 ? fmtBytes(quota) : "∞"}</span>
          {quota > 0 && <span className="pct">{Math.round(pct)}%</span>}
        </div>
        <div className="bar">
          <div
            className={`bar-fill ${barClass}`}
            style={{ width: quota > 0 ? `${pct}%` : "100%" }}
          />
        </div>
      </td>
      <td className="sm" data-label="انقضا">
        {user.expires_at ? new Date(user.expires_at).toLocaleDateString("fa-IR") : "—"}
      </td>
      <td className="actions" data-label="عملیات">
        <button className="btn xs" onClick={onEdit}>ویرایش</button>
        <button className="btn xs" onClick={onConfigs}>کانفیگ</button>
        <button className="btn xs" onClick={onEditIpLimit}>حد IP</button>
        <button className="btn xs" onClick={onEditBandwidth}>سرعت</button>
        <button className="btn xs" onClick={onReset}>ریست</button>
        <button className="btn xs" onClick={onToggle}>
          {user.enabled ? "غیرفعال" : "فعال"}
        </button>
        <button className="btn xs danger" onClick={onRemove}>حذف</button>
      </td>
    </tr>
  );
}
