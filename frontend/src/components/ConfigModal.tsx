import React, { useEffect, useState } from "react";
import { api, subUrl } from "../api";
import type { Configs, User } from "../api";

type TabKey = "openvpn" | "wireguard" | "l2tp";

const TABS: { key: TabKey; label: string; ext: string }[] = [
  { key: "openvpn", label: "OpenVPN", ext: "ovpn" },
  { key: "wireguard", label: "WireGuard", ext: "conf" },
  { key: "l2tp", label: "L2TP/IPsec", ext: "txt" },
];

interface ConfigModalProps {
  user: User;
  baseUrl?: string;
  onClose: () => void;
  onRefresh?: () => void;
}

export default function ConfigModal({ user, baseUrl, onClose, onRefresh }: ConfigModalProps) {
  const [configs, setConfigs] = useState<Configs | null>(null);
  const [tab, setTab] = useState<TabKey>("openvpn");
  const [error, setError] = useState("");
  const [subToken, setSubToken] = useState(user.sub_token);
  const link = subUrl(subToken, baseUrl);

  async function rotate() {
    if (!confirm("لینک اشتراک قبلی باطل و لینک جدید ساخته شود؟")) return;
    try {
      const u = await api.rotateSub(user.id);
      setSubToken(u.sub_token);
      onRefresh?.();
    } catch (e) {
      setError((e as Error).message);
    }
  }

  useEffect(() => {
    // با تغییر کاربر، توکن اشتراک را دوباره همگام کن
    setSubToken(user.sub_token);
    api
      .getConfigs(user.id)
      .then(setConfigs)
      .catch((e) => setError((e as Error).message));
  }, [user.id, user.sub_token]);

  function currentText(): string {
    if (!configs) return "";
    if (tab === "l2tp") {
      const l = configs.l2tp || ({} as Configs["l2tp"]);
      return [
        `Server:   ${l.server}`,
        `IPsec PSK: ${l.psk}`,
        `Username: ${l.username}`,
        `Password: ${l.password}`,
        `Assigned IP: ${l.assigned_ip}`,
      ].join("\n");
    }
    return configs[tab] || "";
  }

  function download() {
    const meta = TABS.find((t) => t.key === tab);
    if (!meta) return;
    const blob = new Blob([currentText()], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${user.username}.${meta.ext}`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function copy() {
    navigator.clipboard?.writeText(currentText());
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card" onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>کانفیگ‌های {user.username}</h2>
          <button className="btn ghost" onClick={onClose}>✕</button>
        </div>
        {error && <div className="alert">{error}</div>}

        <div className="sub-box">
          <div className="sub-label">🔗 لینک اشتراک اختصاصی (کاربر: حجم باقی‌مانده + کانفیگ‌ها)</div>
          <div className="sub-row">
            <input className="sub-input" readOnly value={link} onFocus={(e) => e.target.select()} />
            <button className="btn xs" onClick={() => navigator.clipboard?.writeText(link)}>کپی</button>
            <a className="btn xs" href={link} target="_blank" rel="noreferrer">باز کردن</a>
            <button className="btn xs danger" onClick={rotate}>چرخش کلید</button>
          </div>
        </div>

        <div className="tabs">
          {TABS.map((t) => (
            <button
              key={t.key}
              className={`tab ${tab === t.key ? "active" : ""}`}
              onClick={() => setTab(t.key)}
            >
              {t.label}
            </button>
          ))}
        </div>
        <textarea className="config-box" readOnly value={configs ? currentText() : "در حال بارگذاری…"} />
        <div className="form-actions">
          <button className="btn primary" onClick={download}>دانلود</button>
          <button className="btn" onClick={copy}>کپی</button>
        </div>
      </div>
    </div>
  );
}
