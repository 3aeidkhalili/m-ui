// ابزارهای فرمت‌کردن اعداد (مشترک بین داشبورد و مدال‌ها)

export function fmtBytes(n: number | null | undefined): string {
  n = Number(n || 0);
  if (n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  return `${(n / 1024 ** i).toFixed(i ? 2 : 0)} ${units[i]}`;
}

// نرخ لحظه‌ای (بایت بر ثانیه) → مثلاً «۱۲٫۳ MB/s»
export function fmtRate(bytesPerSec: number | null | undefined): string {
  const n = Number(bytesPerSec || 0);
  if (n < 1) return "0 B/s";
  const units = ["B/s", "KB/s", "MB/s", "GB/s"];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  return `${(n / 1024 ** i).toFixed(i ? 1 : 0)} ${units[i]}`;
}

// «۴۳ ثانیه پیش» / «۲ دقیقه پیش» — ورودی: ثانیه از رویداد
export function fmtAgo(sec: number | null | undefined): string {
  if (sec === null || sec === undefined) return "—";
  if (sec < 5) return "هم‌اکنون";
  if (sec < 60) return `${Math.round(sec)} ثانیه پیش`;
  const m = Math.floor(sec / 60);
  if (m < 60) return `${m} دقیقه پیش`;
  const h = Math.floor(m / 60);
  return `${h} ساعت پیش`;
}
