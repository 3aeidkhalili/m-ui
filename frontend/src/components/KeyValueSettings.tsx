import React, { useEffect, useState } from "react";
import type { Field, SettingsResponse } from "../api";

interface KeyValueSettingsProps {
  title: string;
  hint?: string;
  loader: () => Promise<SettingsResponse>;
  saver: (values: Record<string, string>) => Promise<SettingsResponse>;
  onClose: () => void;
  onSaved?: (values: Record<string, string>) => void;
}

/**
 * مودال عمومیِ ویرایش تنظیمات key/value (هم برای «تنظیمات سایت» و هم «تنظیمات پروتکل»).
 * props: title, loader() -> {fields, values}, saver(values) -> {fields, values}
 */
export default function KeyValueSettings({ title, hint, loader, saver, onClose, onSaved }: KeyValueSettingsProps) {
  const [fields, setFields] = useState<Field[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    loader()
      .then((d) => {
        setFields(d.fields);
        setValues(d.values);
      })
      .catch((e) => setError((e as Error).message));
  }, [loader]);

  function set(key: string, val: string) {
    setValues((v) => ({ ...v, [key]: val }));
    setSaved(false);
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      const d = await saver(values);
      setValues(d.values);
      setSaved(true);
      onSaved?.(d.values);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  const groups: Record<string, Field[]> = {};
  for (const f of fields) (groups[f.group] ||= []).push(f);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>{title}</h2>
          <button className="btn ghost" onClick={onClose}>✕</button>
        </div>
        {hint && <p className="muted sm">{hint}</p>}
        {error && <div className="alert">{error}</div>}
        {saved && (
          <div className="alert" style={{ background: "#123a1d", borderColor: "#1f5133", color: "#a6f0c0" }}>
            ذخیره شد. کاربران با گرفتن کانفیگ جدید از صفحه‌ی اشتراک، تغییرات را دریافت می‌کنند.
          </div>
        )}
        <form onSubmit={save}>
          {Object.entries(groups).map(([group, gfields]) => (
            <div key={group} style={{ marginBottom: 10 }}>
              <h3>{group}</h3>
              {gfields.map((f) => (
                <div key={f.key} style={{ marginBottom: 10 }}>
                  <label>{f.label}</label>
                  {f.type === "textarea" ? (
                    <textarea rows={2} value={values[f.key] ?? ""} onChange={(e) => set(f.key, e.target.value)} />
                  ) : (
                    <input
                      type={f.type === "number" ? "number" : "text"}
                      value={values[f.key] ?? ""}
                      onChange={(e) => set(f.key, e.target.value)}
                    />
                  )}
                </div>
              ))}
            </div>
          ))}
          <div className="form-actions">
            <button className="btn primary" disabled={busy}>{busy ? "در حال ذخیره…" : "ذخیره"}</button>
            <button type="button" className="btn ghost" onClick={onClose}>بستن</button>
          </div>
        </form>
      </div>
    </div>
  );
}
