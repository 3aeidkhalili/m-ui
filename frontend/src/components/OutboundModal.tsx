import React, { useEffect, useState } from "react";
import { api } from "../api";
import type { Outbound, OutboundTestResult, OutboundTcpTest, OutboundProxyTest } from "../api";

interface OutboundModalProps {
  onClose: () => void;
}

interface GroupInfo {
  balanced: boolean;
  group_size: number;
  group_name: string;
  strategy: string;
}

interface EditingOutbound {
  id: number;
  name: string;
  config: string;
}

type StoredTest = Partial<OutboundTestResult>;

export default function OutboundModal({ onClose }: OutboundModalProps) {
  const [items, setItems] = useState<Outbound[]>([]);
  const [direct, setDirect] = useState(true);
  const [group, setGroup] = useState<GroupInfo>({ balanced: false, group_size: 0, group_name: "", strategy: "" });
  const [name, setName] = useState("");
  const [config, setConfig] = useState("");
  const [preview, setPreview] = useState("");
  const [viewJson, setViewJson] = useState<Outbound | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [tests, setTests] = useState<Record<number, StoredTest | null>>({}); // id → نتیجه‌ی تست
  const [editing, setEditing] = useState<EditingOutbound | null>(null); // {id, name, config}
  const [iranDirect, setIranDirect] = useState(true);
  const [iranBusy, setIranBusy] = useState(false);

  async function refresh() {
    try {
      const d = await api.getOutbounds();
      setItems(d.items);
      setDirect(d.direct);
      setGroup({
        balanced: d.balanced,
        group_size: d.group_size,
        group_name: d.group_name,
        strategy: d.strategy,
      });
    } catch (e) {
      setError((e as Error).message);
    }
  }
  useEffect(() => {
    refresh();
    api.getIranDirect().then((d) => setIranDirect(d.enabled)).catch(() => {});
  }, []);

  async function toggleIran() {
    setIranBusy(true);
    setError("");
    try {
      const d = await api.setIranDirect(!iranDirect);
      setIranDirect(d.enabled);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setIranBusy(false);
    }
  }

  async function doParse() {
    setError("");
    setPreview("");
    try {
      const d = await api.parseOutbound(config);
      setPreview(d.config);
    } catch (e) {
      setError((e as Error).message);
    }
  }
  async function doAdd(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await api.addOutbound(name.trim(), config);
      setName("");
      setConfig("");
      setPreview("");
      await refresh();
    } catch (e2) {
      setError((e2 as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function activate(id: number) {
    try { await api.activateOutbound(id); await refresh(); } catch (e) { setError((e as Error).message); }
  }
  async function goDirect() {
    try { await api.useDirect(); await refresh(); } catch (e) { setError((e as Error).message); }
  }
  async function activateAll() {
    try { await api.activateAllOutbounds(); await refresh(); } catch (e) { setError((e as Error).message); }
  }
  async function saveEdit() {
    if (!editing) return;
    setError("");
    setBusy(true);
    try {
      await api.updateOutbound(editing.id, { name: editing.name, config: editing.config });
      setEditing(null);
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function remove(id: number) {
    if (!confirm("حذف این اوتباند؟")) return;
    try { await api.deleteOutbound(id); await refresh(); } catch (e) { setError((e as Error).message); }
  }
  async function test(id: number) {
    setTestingId(id);
    setTests((t) => ({ ...t, [id]: null }));
    try {
      const r = await api.testOutbound(id);
      setTests((t) => ({ ...t, [id]: r }));
      await refresh(); // کشور/پرچمِ ذخیره‌شده روی ردیف به‌روز شود
    } catch (e) {
      setTests((t) => ({ ...t, [id]: { ok: false, message: (e as Error).message } }));
    } finally {
      setTestingId(null);
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>اوتباند (مسیر خروجی ترافیک)</h2>
          <button className="btn ghost" onClick={onClose}>✕</button>
        </div>
        <p className="muted sm">
          لینک <code>vless/vmess/trojan/ss</code> یا Xray outbound JSON بدهید؛ سیستم آن را به JSON تبدیل می‌کند.
          با فعال‌سازی، ترافیک همه‌ی کاربران از این اوتباند عبور می‌کند (حساب حجم دست‌نخورده می‌ماند).
        </p>
        {error && <div className="alert">{error}</div>}

        <div className={`iran-toggle ${iranDirect ? "on" : "off"}`}>
          <div className="iran-tinfo">
            <div className="iran-ttitle">
              <span className="iran-flag">🇮🇷</span> مسیریابیِ مستقیمِ ایران (Split Tunnel)
            </div>
            <div className="iran-tdesc">
              وقتی <b>روشن</b> باشد، ترافیکِ دامنه‌های <code>.ir</code> و IPهای ایرانی از رله‌ی خارجی عبور
              <b> نمی‌کند</b> و مستقیم از مسیرِ داخلیِ سرور خارج می‌شود. نتیجه:
              سایت‌های ایرانی <b>سریع‌تر</b> باز می‌شوند، سایت‌هایی که IP خارجی را مسدود می‌کنند (بانک‌ها/دولتی)
              درست کار می‌کنند، و این ترافیک <b>از حجمِ کاربر کم نمی‌شود</b>. ترافیکِ خارجی همچنان از رله عبور می‌کند.
            </div>
          </div>
          <button
            className={`switch ${iranDirect ? "on" : ""}`}
            onClick={toggleIran}
            disabled={iranBusy}
            aria-label="toggle iran direct"
            title={iranDirect ? "خاموش کردن" : "روشن کردن"}
          >
            <span className="switch-knob" />
            <span className="switch-txt">{iranBusy ? "…" : iranDirect ? "روشن" : "خاموش"}</span>
          </button>
        </div>

        <div className="sub-box">
          <div className="sub-label">
            حالت فعلی:{" "}
            {direct ? (
              <span className="badge active">مستقیم (freedom)</span>
            ) : group.balanced ? (
              <span className="badge balancer">
                ⚖ بالانسر round-robin · «{group.group_name}» · {group.group_size} رله
              </span>
            ) : (
              <span className="badge active">
                {group.group_name || items.find((i) => i.is_active)?.name || "اوتباند فعال"}
              </span>
            )}
            <button className="btn xs" style={{ marginRight: 8 }} onClick={activateAll}>
              فعال‌سازی همه (بالانسر)
            </button>
            {!direct && (
              <button className="btn xs" onClick={goDirect}>
                بازگشت به حالت مستقیم
              </button>
            )}
          </div>
          {group.balanced && (
            <div className="muted sm" style={{ marginTop: 8 }}>
              کاربران به‌صورت خودکار و متوازن بین {group.group_size} رله‌ی هم‌نام پخش می‌شوند
              (هر کاربر یک رله‌ی ثابت؛ شمارش حجم per-user دست‌نخورده). افزودن رله‌ی هم‌نامِ دیگر، خودکار به گروه اضافه می‌شود.
            </div>
          )}
        </div>

        <form onSubmit={doAdd}>
          <h3>افزودن اوتباند</h3>
          <label>نام (اختیاری)</label>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="مثلاً relay-de" />
          <div className="muted sm" style={{ marginTop: 4 }}>
            ⚖ چند اوتباند با <b>نامِ دقیقاً یکسان</b> بسازید؛ با فعال‌سازی، خودکار به‌صورت
            بالانسرِ round-robin بین رله‌ها پخش می‌شوند.
          </div>
          <label>لینک یا JSON</label>
          <textarea
            rows={4}
            value={config}
            onChange={(e) => setConfig(e.target.value)}
            placeholder="vless://uuid@host:443?...  یا  { &quot;protocol&quot;: &quot;vmess&quot;, ... }"
            style={{ direction: "ltr", textAlign: "left", fontFamily: "Consolas, monospace" }}
          />
          <div className="form-actions">
            <button className="btn primary" disabled={busy}>{busy ? "…" : "افزودن"}</button>
            <button type="button" className="btn" onClick={doParse}>پیش‌نمایش JSON</button>
          </div>
        </form>

        {preview && (
          <>
            <h3 style={{ marginTop: 14 }}>خروجی JSON تولیدشده</h3>
            <textarea className="config-box" readOnly value={preview} />
          </>
        )}

        <h3 style={{ marginTop: 16 }}>اوتباندها</h3>
        {items.length === 0 ? (
          <p className="muted sm">هنوز اوتباندی اضافه نشده.</p>
        ) : (
          <div className="table-scroll">
            <table>
              <thead>
                <tr><th>نام</th><th>پروتکل</th><th>آدرس</th><th>عملیات</th></tr>
              </thead>
              <tbody>
                {items.map((o) => (
                  <React.Fragment key={o.id}>
                    <tr className={o.is_active ? "" : "dim"}>
                      <td data-label="نام">
                        <strong>{o.name}</strong>{" "}
                        {o.balanced ? (
                          <span className="badge balancer">⚖ بالانسر · {o.group_size}</span>
                        ) : o.is_active ? (
                          <span className="badge active">فعال</span>
                        ) : o.name_count > 1 ? (
                          <span className="badge group">هم‌نام ×{o.name_count}</span>
                        ) : null}
                      </td>
                      <td data-label="پروتکل">
                        {o.country_code ? (
                          <span className="loc-tag" title={o.country_name}>
                            <img className="flag" src={o.flag} alt="" loading="lazy" />
                            {o.country_name || o.protocol}
                          </span>
                        ) : (
                          o.protocol
                        )}
                      </td>
                      <td className="sm" data-label="آدرس" style={{ direction: "ltr", textAlign: "left" }}>
                        {o.address}
                        {o.egress_ip && <div className="muted" style={{ fontSize: 11 }}>خروجی: {o.egress_ip}</div>}
                      </td>
                      <td className="actions" data-label="عملیات">
                        <button className="btn xs" onClick={() => test(o.id)} disabled={testingId === o.id}>
                          {testingId === o.id ? "…در حال تست" : "تست اتصال"}
                        </button>
                        {!o.is_active && <button className="btn xs" onClick={() => activate(o.id)}>فعال‌سازی</button>}
                        <button className="btn xs" onClick={() => setEditing({ id: o.id, name: o.name, config: o.config })}>ویرایش</button>
                        <button className="btn xs" onClick={() => setViewJson(o)}>JSON</button>
                        <button className="btn xs danger" onClick={() => remove(o.id)}>حذف</button>
                      </td>
                    </tr>
                    {editing?.id === o.id && (
                      <tr className="test-row">
                        <td colSpan={4}>
                          <div className="edit-box">
                            <label>نام</label>
                            <input
                              value={editing.name}
                              onChange={(e) => setEditing((s) => ({ ...s!, name: e.target.value }))}
                            />
                            <label>کانفیگ (لینک یا JSON)</label>
                            <textarea
                              rows={5}
                              value={editing.config}
                              onChange={(e) => setEditing((s) => ({ ...s!, config: e.target.value }))}
                              style={{ direction: "ltr", textAlign: "left", fontFamily: "Consolas, monospace" }}
                            />
                            <div className="muted sm">تغییر کانفیگ، کشور/پرچمِ ذخیره‌شده را پاک می‌کند؛ دوباره «تست اتصال» بزنید.</div>
                            <div className="form-actions">
                              <button className="btn primary" onClick={saveEdit} disabled={busy}>{busy ? "…" : "ذخیره"}</button>
                              <button className="btn" onClick={() => setEditing(null)}>انصراف</button>
                            </div>
                          </div>
                        </td>
                      </tr>
                    )}
                    {(testingId === o.id || tests[o.id]) && (
                      <tr className="test-row">
                        <td colSpan={4}>
                          <TestResult loading={testingId === o.id} result={tests[o.id]} />
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {viewJson && (
          <>
            <h3 style={{ marginTop: 14 }}>JSON — {viewJson.name}</h3>
            <textarea className="config-box" readOnly value={viewJson.config} />
            <div className="form-actions">
              <button className="btn" onClick={() => setViewJson(null)}>بستن نمایش</button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

interface TestResultProps {
  loading: boolean;
  result?: StoredTest | null;
}

function TestResult({ loading, result }: TestResultProps) {
  if (loading) {
    return (
      <div className="test-result pending">
        <span className="spin" /> در حال بررسی اتصال به رله… (تا چند ثانیه)
      </div>
    );
  }
  if (!result) return null;
  const cls = result.busy ? "warn" : result.ok ? "ok" : "bad";
  const icon = result.busy ? "⏳" : result.ok ? "✓" : "✕";
  const tcp: OutboundTcpTest = result.tcp || {};
  const proxy: OutboundProxyTest = result.proxy || {};
  return (
    <div className={`test-result ${cls}`}>
      <div className="tr-head">
        <span className="tr-icon">{icon}</span>
        <span>{result.message}</span>
      </div>
      {(tcp.ok !== undefined || proxy.ran) && (
        <div className="tr-meta sm">
          {tcp.ok !== undefined && (
            <span className={`tr-chip ${tcp.ok ? "ok" : "bad"}`}>
              TCP: {tcp.ok ? `باز · ${tcp.latency_ms}ms` : "بسته"}
            </span>
          )}
          {proxy.ran && (
            <span className={`tr-chip ${proxy.ok ? "ok" : "bad"}`}>
              عبور ترافیک: {proxy.ok ? `موفق · ${proxy.latency_ms}ms` : "ناموفق"}
            </span>
          )}
          {proxy.egress_ip && (
            <span className="tr-chip ip" style={{ direction: "ltr" }}>IP خروجی: {proxy.egress_ip}</span>
          )}
        </div>
      )}
    </div>
  );
}
