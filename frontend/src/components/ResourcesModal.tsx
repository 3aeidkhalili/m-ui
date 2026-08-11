import React, { useEffect, useState } from "react";
import { api } from "../api";
import type { Resource, ResourceInput } from "../api";

interface ResourcesModalProps {
  onClose: () => void;
}

// فرمِ ویرایش می‌تواند id هم داشته باشد (هنگام ویرایش یک آیتم موجود)
type ResourceForm = ResourceInput & { id?: number };

// کلیدهای متنی که با کمکِ عمومیِ set() ویرایش می‌شوند
type StringField = "kind" | "title" | "description" | "url" | "icon" | "platform";

const EMPTY: ResourceForm = {
  kind: "download",
  icon: "📦",
  title: "",
  platform: "",
  description: "",
  url: "",
  sort_order: 0,
  enabled: true,
};

const ICONS = ["📦", "🤖", "🍏", "🪟", "🐧", "🌐", "📘", "🎬", "⚙️", "🔒", "📱", "💻", "⬇️", "▶️"];

export default function ResourcesModal({ onClose }: ResourcesModalProps) {
  const [items, setItems] = useState<Resource[]>([]);
  const [form, setForm] = useState<ResourceForm>(EMPTY);
  const [editId, setEditId] = useState<number | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function refresh() {
    try {
      setItems(await api.getResources());
    } catch (e) {
      setError((e as Error).message);
    }
  }
  useEffect(() => {
    refresh();
  }, []);

  function edit(r: Resource) {
    setEditId(r.id);
    setForm({ ...EMPTY, ...r });
  }
  function reset() {
    setEditId(null);
    setForm(EMPTY);
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      if (editId) await api.updateResource(editId, form);
      else await api.createResource(form);
      reset();
      await refresh();
    } catch (e2) {
      setError((e2 as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function remove(id: number) {
    if (!confirm("حذف این آیتم؟")) return;
    try {
      await api.deleteResource(id);
      if (editId === id) reset();
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  }
  async function toggle(r: Resource) {
    try {
      await api.updateResource(r.id, { enabled: !r.enabled });
      await refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  const set = (k: StringField) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setForm((f): ResourceForm => ({ ...f, [k]: e.target.value }));

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>آموزش و دانلود نرم‌افزار</h2>
          <button className="btn ghost" onClick={onClose}>✕</button>
        </div>
        <p className="muted sm">
          این آیتم‌ها در صفحه‌ی اشتراکِ کاربران (بخش «📚 آموزش و دانلود») نمایش داده می‌شوند.
        </p>
        {error && <div className="alert">{error}</div>}

        <form onSubmit={save}>
          <h3>{editId ? "ویرایش آیتم" : "افزودن آیتم"}</h3>
          <div className="form-grid">
            <div>
              <label>نوع</label>
              <select value={form.kind} onChange={set("kind")}>
                <option value="download">دانلود نرم‌افزار</option>
                <option value="guide">آموزش</option>
              </select>
            </div>
            <div>
              <label>پلتفرم (اختیاری)</label>
              <input value={form.platform} onChange={set("platform")} placeholder="Android / iOS / Windows" />
            </div>
          </div>
          <label>عنوان</label>
          <input value={form.title} onChange={set("title")} placeholder="مثلاً v2rayNG" required />
          <label>توضیح (اختیاری)</label>
          <input value={form.description} onChange={set("description")} placeholder="توضیح کوتاه" />
          <label>لینک (اختیاری، فقط http/https)</label>
          <input
            value={form.url}
            onChange={set("url")}
            placeholder="https://…"
            style={{ direction: "ltr", textAlign: "left" }}
          />
          <label>آیکون</label>
          <div className="icon-pick">
            {ICONS.map((ic) => (
              <button
                type="button"
                key={ic}
                className={`icon-opt ${form.icon === ic ? "active" : ""}`}
                onClick={() => setForm((f) => ({ ...f, icon: ic }))}
              >
                {ic}
              </button>
            ))}
            <input
              className="icon-free"
              value={form.icon}
              onChange={set("icon")}
              maxLength={4}
              aria-label="آیکون دلخواه"
            />
          </div>
          <div className="form-grid" style={{ marginTop: 12 }}>
            <div>
              <label>ترتیب نمایش</label>
              <input
                type="number"
                value={form.sort_order}
                onChange={(e) => setForm((f) => ({ ...f, sort_order: Number(e.target.value) }))}
              />
            </div>
            <div style={{ display: "flex", alignItems: "flex-end", gap: 8 }}>
              <label style={{ margin: 0 }}>
                <input
                  type="checkbox"
                  checked={!!form.enabled}
                  onChange={(e) => setForm((f) => ({ ...f, enabled: e.target.checked }))}
                  style={{ width: "auto", marginLeft: 6 }}
                />
                فعال (نمایش در صفحه‌ی اشتراک)
              </label>
            </div>
          </div>
          <div className="form-actions">
            <button className="btn primary" disabled={busy}>{busy ? "…" : editId ? "ذخیره" : "افزودن"}</button>
            {editId && <button type="button" className="btn" onClick={reset}>انصراف</button>}
          </div>
        </form>

        <h3 style={{ marginTop: 16 }}>آیتم‌ها</h3>
        {items.length === 0 ? (
          <p className="muted sm">هنوز آیتمی اضافه نشده.</p>
        ) : (
          <div className="res-admin-list">
            {items.map((r) => (
              <div key={r.id} className={`res-admin ${r.enabled ? "" : "dim"}`}>
                <span className="res-admin-ico">{r.icon}</span>
                <div className="res-admin-body">
                  <b>
                    {r.title}
                    {r.platform && <span className="res-plat-a">{r.platform}</span>}
                    <span className={`kind-chip ${r.kind}`}>{r.kind === "guide" ? "آموزش" : "دانلود"}</span>
                  </b>
                  {r.description && <div className="muted sm">{r.description}</div>}
                  {r.url && <div className="muted sm" style={{ direction: "ltr", textAlign: "left" }}>{r.url}</div>}
                </div>
                <div className="res-admin-act">
                  <button className="btn xs" onClick={() => toggle(r)}>{r.enabled ? "غیرفعال" : "فعال"}</button>
                  <button className="btn xs" onClick={() => edit(r)}>ویرایش</button>
                  <button className="btn xs danger" onClick={() => remove(r.id)}>حذف</button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
