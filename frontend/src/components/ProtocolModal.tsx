import React from "react";
import { fmtBytes, fmtRate, fmtAgo } from "../format";
import type { ProtocolStat } from "../api";

export interface RateSample {
  rx: number;
  tx: number;
  total: number;
}

export interface ProtoMeta {
  icon: string;
  accent: string;
  title: string;
  sub: string;
}

// متادیتای نمایشی هر پروتکل (آیکون، رنگ لهجه، زیرعنوان)
export const PROTO_META: Record<string, ProtoMeta> = {
  xray: { icon: "⚡", accent: "#6366f1", title: "Xray Core", sub: "هستهٔ مرکزی · شمارندهٔ حجم واحد" },
  wireguard: { icon: "🐉", accent: "#f43f5e", title: "WireGuard", sub: "UDP/51820 · سبک و سریع" },
  openvpn: { icon: "🛡️", accent: "#f59e0b", title: "OpenVPN", sub: "UDP/1194 · سازگاری بالا" },
  l2tp: { icon: "🔗", accent: "#22d3ee", title: "L2TP/IPsec", sub: "xl2tpd · IP ثابت" },
  strongswan: { icon: "🔐", accent: "#a855f7", title: "IPsec (strongSwan)", sub: "لایهٔ رمزنگاری L2TP" },
};

const STATUS_LABEL: Record<string, string> = {
  active: "فعال",
  inactive: "غیرفعال",
  failed: "خطا",
  unknown: "نامعلوم",
  "n/a": "غیرفعال (dev)",
};

export function protoMeta(key: string): ProtoMeta {
  return PROTO_META[key] || { icon: "🌐", accent: "#6366f1", title: key, sub: "" };
}

interface SparklineProps {
  history?: RateSample[];
  accent: string;
}

// اسپارک‌لاین ناحیه‌ای (SVG) — دو سری: دانلود و آپلود
function Sparkline({ history, accent }: SparklineProps) {
  const W = 560;
  const H = 120;
  const pad = 6;
  const pts: RateSample[] = history && history.length ? history : [];
  const maxV = Math.max(1, ...pts.map((p) => p.total));

  function path(sel: (p: RateSample) => number, close: boolean): string {
    if (pts.length < 2) return "";
    const step = (W - pad * 2) / (pts.length - 1);
    let d = "";
    pts.forEach((p, i) => {
      const x = pad + i * step;
      const y = H - pad - (sel(p) / maxV) * (H - pad * 2);
      d += `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)} `;
    });
    if (close) {
      const lastX = pad + (pts.length - 1) * step;
      d += `L${lastX.toFixed(1)},${H - pad} L${pad},${H - pad} Z`;
    }
    return d;
  }

  if (pts.length < 2) {
    return (
      <div className="spark-empty muted sm">
        در حال جمع‌آوری نمونه‌ها برای نمودار زنده…
      </div>
    );
  }

  return (
    <svg className="spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label="نمودار نرخ لحظه‌ای">
      <defs>
        <linearGradient id="spk-down" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={accent} stopOpacity="0.45" />
          <stop offset="100%" stopColor={accent} stopOpacity="0" />
        </linearGradient>
      </defs>
      {/* خطوط راهنما */}
      {[0.25, 0.5, 0.75].map((g) => (
        <line key={g} x1={pad} x2={W - pad} y1={H - pad - g * (H - pad * 2)} y2={H - pad - g * (H - pad * 2)}
          stroke="rgba(255,255,255,.06)" strokeWidth="1" />
      ))}
      <path d={path((p) => p.total, true)} fill="url(#spk-down)" />
      <path d={path((p) => p.total, false)} fill="none" stroke={accent} strokeWidth="2.5"
        strokeLinejoin="round" strokeLinecap="round" />
      <path d={path((p) => p.rx, false)} fill="none" stroke="rgba(255,255,255,.55)" strokeWidth="1.5"
        strokeDasharray="3 3" strokeLinejoin="round" />
    </svg>
  );
}

interface DonutProps {
  pct: number;
  accent: string;
  label: string;
}

function Donut({ pct, accent, label }: DonutProps) {
  const r = 34;
  const c = 2 * Math.PI * r;
  const off = c * (1 - Math.min(1, Math.max(0, pct)) / 100);
  return (
    <div className="donut-wrap">
      <svg className="donut" viewBox="0 0 84 84">
        <circle cx="42" cy="42" r={r} fill="none" stroke="rgba(255,255,255,.08)" strokeWidth="9" />
        <circle cx="42" cy="42" r={r} fill="none" stroke={accent} strokeWidth="9" strokeLinecap="round"
          strokeDasharray={c} strokeDashoffset={off} transform="rotate(-90 42 42)"
          style={{ transition: "stroke-dashoffset .7s cubic-bezier(.4,0,.2,1)" }} />
        <text x="42" y="40" textAnchor="middle" className="donut-num">{Math.round(pct)}%</text>
        <text x="42" y="55" textAnchor="middle" className="donut-lbl">{label}</text>
      </svg>
    </div>
  );
}

interface ProtocolModalProps {
  data: ProtocolStat;
  rate?: RateSample;
  history?: RateSample[];
  share?: number;
  onClose: () => void;
}

export default function ProtocolModal({ data, rate, history, share, onClose }: ProtocolModalProps) {
  const meta = protoMeta(data.key);
  const rx = data.rx_bytes || 0;
  const tx = data.tx_bytes || 0;
  const total = data.total_bytes || 0;
  const dlPct = total > 0 ? (tx / total) * 100 : 0; // دانلود کاربر = tx
  const r: RateSample = rate || { rx: 0, tx: 0, total: 0 };
  const isActive = data.status === "active";
  const hasBytes = data.key !== "strongswan";
  const peers = data.peers ?? [];

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal card proto-modal"
        onClick={(e) => e.stopPropagation()}
        style={{ "--acc": meta.accent } as React.CSSProperties}
      >
        <div className="modal-head">
          <div className="proto-title">
            <span className="proto-badge" style={{ "--acc": meta.accent } as React.CSSProperties}>{meta.icon}</span>
            <div>
              <h2>{meta.title}</h2>
              <div className="muted sm">{data.detail || meta.sub}</div>
            </div>
          </div>
          <button className="btn ghost" onClick={onClose}>✕</button>
        </div>

        <div className="proto-status-row">
          <span className={`svc-dot ${isActive ? "ok" : data.status === "inactive" || data.status === "n/a" ? "na" : "bad"}`} />
          <span className="sm">
            سرویس <code>{data.service}</code> — {STATUS_LABEL[data.status] || data.status}
          </span>
        </div>

        {/* سه کاشی اصلی */}
        <div className="proto-tiles">
          <div className="ptile">
            <div className="ptile-lbl">کاربران آنلاین</div>
            <div className="ptile-val">
              {data.online}
              <span className="pulse-dot" />
            </div>
          </div>
          <div className="ptile">
            <div className="ptile-lbl">سرعت لحظه‌ای</div>
            <div className="ptile-val">{fmtRate(r.total)}</div>
            <div className="ptile-sub">
              ↓ {fmtRate(r.tx)} · ↑ {fmtRate(r.rx)}
            </div>
          </div>
          <div className="ptile">
            <div className="ptile-lbl">مصرف کل</div>
            <div className="ptile-val">{hasBytes ? fmtBytes(total) : "—"}</div>
          </div>
        </div>

        {/* لوکیشن خروج (اوتباند) — فقط برای Xray که لایهٔ ترانزیت است */}
        {data.egress && (
          <div className="proto-section egress-section">
            <h3>لوکیشن خروج (اوتباند)</h3>
            {data.egress.mode === "direct" ? (
              <p className="muted sm">
                🌐 خروج مستقیم (freedom) — ترافیکِ هر سه پروتکل بدون رله از IP همین سرور خارج می‌شود.
                {data.egress.total > 0 &&
                  ` (${data.egress.total} اوتباند تعریف شده؛ هیچ‌کدام فعال نیست.)`}
              </p>
            ) : (
              <>
                <p className="muted sm">
                  {data.egress.mode === "balancer"
                    ? `⚖️ بالانسر round-robin روی ${data.egress.pool_size} لوکیشن — کاربران به‌صورت متوازن پخش می‌شوند.`
                    : "تمام ترافیکِ WireGuard/OpenVPN/L2TP از این رله خارج می‌شود."}
                </p>
                <div className="egress-list">
                  {data.egress.locations.map((l) => (
                    <div className="egress-item" key={l.id}>
                      {l.country_code ? (
                        <img className="egr-flag" src={l.flag} alt="" />
                      ) : (
                        <span className="egr-flag-emoji">🚩</span>
                      )}
                      <div className="egr-body">
                        <b>{l.country_name || l.name}</b>
                        <span className="muted sm" dir="ltr">{l.egress_ip || l.address || "—"}</span>
                      </div>
                      <span className="badge active">{l.protocol || "relay"}</span>
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        )}

        {/* نمودار زندهٔ نرخ */}
        <div className="proto-section">
          <h3>نرخ عبور زنده</h3>
          <Sparkline history={history} accent={meta.accent} />
          <div className="spark-legend sm muted">
            <span><i className="lg-line" style={{ background: meta.accent }} /> مجموع</span>
            <span><i className="lg-dash" /> آپلود</span>
          </div>
        </div>

        {/* تفکیک دانلود/آپلود + سهم از کل */}
        {hasBytes && (
          <div className="proto-section proto-split">
            <div className="split-bars">
              <div className="split-row">
                <div className="split-head">
                  <span>⬇ دانلود کاربران</span>
                  <strong>{fmtBytes(tx)}</strong>
                </div>
                <div className="bar"><div className="bar-fill" style={{ width: `${dlPct}%` }} /></div>
              </div>
              <div className="split-row">
                <div className="split-head">
                  <span>⬆ آپلود کاربران</span>
                  <strong>{fmtBytes(rx)}</strong>
                </div>
                <div className="bar"><div className="bar-fill warn" style={{ width: `${100 - dlPct}%` }} /></div>
              </div>
            </div>
            <Donut pct={share || 0} accent={meta.accent} label="سهم از کل" />
          </div>
        )}

        {/* جدول اتصال‌ها */}
        <div className="proto-section">
          <h3>اتصال‌های فعال {peers.length > 0 && <span className="count-chip">{peers.length}</span>}</h3>
          {peers.length === 0 ? (
            <p className="muted sm">
              {data.key === "xray"
                ? "Xray شمارندهٔ حجمِ واحد است؛ جزئیات هر کاربر در جدول «کاربران» صفحهٔ اصلی دیده می‌شود."
                : data.key === "strongswan"
                ? "این لایه فقط رمزنگاری L2TP را فراهم می‌کند و شمارندهٔ حجم جدا ندارد."
                : "هیچ اتصال فعالی روی این پروتکل نیست."}
            </p>
          ) : (
            <div className="table-scroll">
              <table className="peers-table">
                <thead>
                  <tr>
                    <th>کاربر</th>
                    <th>IP</th>
                    <th>⬇ دانلود</th>
                    <th>⬆ آپلود</th>
                    <th>آخرین اتصال</th>
                  </tr>
                </thead>
                <tbody>
                  {peers.map((p, i) => (
                    <tr key={i}>
                      <td data-label="کاربر">
                        <span className={`svc-dot ${p.online ? "ok" : "na"}`} />
                        <strong>{p.name}</strong>
                      </td>
                      <td data-label="IP" style={{ direction: "ltr", textAlign: "left" }}>{p.ip || "—"}</td>
                      <td data-label="دانلود">{fmtBytes(p.tx_bytes)}</td>
                      <td data-label="آپلود">{fmtBytes(p.rx_bytes)}</td>
                      <td data-label="آخرین اتصال" className="sm">
                        {p.online ? <span className="badge active">آنلاین</span> : fmtAgo(p.last_handshake)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
