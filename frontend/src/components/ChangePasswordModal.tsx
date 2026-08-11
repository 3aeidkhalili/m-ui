import React, { useState } from "react";
import { api } from "../api";

interface ChangePasswordModalProps {
  onClose: () => void;
}

export default function ChangePasswordModal({ onClose }: ChangePasswordModalProps) {
  const [oldPassword, setOld] = useState("");
  const [newPassword, setNew] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    if (newPassword.length < 8) {
      setError("رمز جدید باید حداقل ۸ کاراکتر باشد.");
      return;
    }
    if (newPassword !== confirm) {
      setError("رمز جدید و تکرار آن یکسان نیستند.");
      return;
    }
    setBusy(true);
    try {
      await api.changePassword(oldPassword, newPassword);
      setDone(true);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal card" style={{ width: 420 }} onClick={(e) => e.stopPropagation()}>
        <div className="modal-head">
          <h2>تغییر رمز عبور</h2>
          <button className="btn ghost" onClick={onClose}>✕</button>
        </div>
        {done ? (
          <>
            <div className="alert" style={{ background: "#123a1d", borderColor: "#1f5133", color: "#a6f0c0" }}>
              رمز با موفقیت تغییر کرد. توکن‌های قبلی باطل شدند.
            </div>
            <div className="form-actions">
              <button className="btn primary" onClick={onClose}>بستن</button>
            </div>
          </>
        ) : (
          <form onSubmit={submit}>
            {error && <div className="alert">{error}</div>}
            <label>رمز فعلی</label>
            <input type="password" value={oldPassword} onChange={(e) => setOld(e.target.value)} required />
            <label>رمز جدید (حداقل ۸ کاراکتر)</label>
            <input type="password" value={newPassword} onChange={(e) => setNew(e.target.value)} required />
            <label>تکرار رمز جدید</label>
            <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
            <div className="form-actions" style={{ marginTop: 12 }}>
              <button className="btn primary" disabled={busy}>
                {busy ? "در حال تغییر…" : "تغییر رمز"}
              </button>
              <button type="button" className="btn ghost" onClick={onClose}>انصراف</button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
