# MultiVPN Panel (OpenVPN + WireGuard + L2TP, unified quota via Xray-core)

پنل مدیریت VPN چندپروتکلی. کاربر با کلاینت استاندارد **OpenVPN / WireGuard / L2TP** وصل می‌شود، اما تمام
ترافیک خروجی‌اش از **Xray-core** عبور می‌کند (Xray به‌عنوان outbound). به‌این‌ترتیب مصرف هر سه پروتکل در
**یک شمارنده‌ی واحد** جمع می‌شود و کاربر یک سهمیه‌ی حجمی مشترک دارد.

## چطور «حجم واحد» کار می‌کند

هر کاربر یک **ایندکس `N`** می‌گیرد و در هر سه پروتکل یک IP داخلی ثابت مشتق از N دریافت می‌کند:

| پروتکل    | ساب‌نت داخلی | IP کاربر N |
|-----------|-------------|-----------|
| OpenVPN   | 10.8.0.0/24 | 10.8.0.N  |
| WireGuard | 10.9.0.0/24 | 10.9.0.N  |
| L2TP/IPsec| 10.10.0.0/24| 10.10.0.N |

تمام ترافیک این ساب‌نت‌ها با **TPROXY (nftables)** به‌صورت شفاف وارد یک inbound از نوع
`dokodemo-door` در Xray می‌شود. سپس یک قانون routing:

```
source = [10.8.0.N, 10.9.0.N, 10.10.0.N]  →  outboundTag = "user-N"
```

ترافیک را به یک outbound اختصاصی هدایت می‌کند. Xray آمار `outbound>>>user-N>>>traffic>>>{up,down}`
را نگه می‌دارد که **مجموع مصرف هر سه پروتکل** است. یک job پس‌زمینه این آمار را می‌خواند و اگر کاربری
از سهمیه رد شد یا منقضی شد، او را از هر سه پروتکل و از routing حذف می‌کند.

## معماری

```
┌────────────┐   REST/JWT   ┌──────────────┐  scripts/*.sh   ┌─ OpenVPN (easy-rsa)
│  React UI  │ ───────────▶ │  Go API      │ ──────────────▶ ├─ WireGuard (wg)
│ (Vite+TS)  │ ◀─────────── │  + SQLite    │                 └─ L2TP (xl2tpd/strongSwan)
└────────────┘              │              │  xray config +
                            │  traffic job │  statsquery      ┌───────────┐
                            └──────────────┘ ───────────────▶ │ Xray-core │──▶ Internet
                                                              └───────────┘
                                              nftables TPROXY: VPN subnets → dokodemo-door
```

## پروتکل‌ها

- **OpenVPN** — UDP/1194، احراز هویت با گواهی (easy-rsa)، IP ثابت با `client-config-dir`.
- **WireGuard** — UDP/51820، هر کاربر یک peer با `AllowedIPs = <ip>/32`.
- **L2TP/IPsec** — strongSwan + xl2tpd، IP ثابت از طریق ستون چهارم `chap-secrets`.

همه پشت یک هسته‌ی Xray با سهمیه‌ی واحد.

## نصب روی اوبونتو ۲۲.۰۴/۲۴.۰۴

```bash
git clone <repo> /opt/multivpn && cd /opt/multivpn
sudo bash install.sh
```

اسکریپت به‌ترتیب: بسته‌ها، Xray-core، OpenVPN+easy-rsa، WireGuard، strongSwan+xl2tpd، قوانین
nftables/TPROXY، بک‌اند (systemd) و nginx را نصب و راه‌اندازی می‌کند و ادمین اولیه را می‌سازد.

### نصب آفلاین / پشت فیلترینگ (ایران)
Xray-core به‌صورت باینریِ لوکال همراه پروژه است (`vendor/xray/`)، پس **بدون اینترنت آزاد** نصب می‌شود.
اولویت نصب Xray: **بسته‌ی لوکال ← mirror ← GitHub رسمی**. برای بقیه‌ی دانلودها می‌توانید mirror بدهید:

```bash
# استفاده از پروکسی گیت‌هاب (برای Xray وقتی bundle نیست)
sudo GITHUB_PROXY=https://ghproxy.com/ bash install.sh
```

برای آفلاین‌کردن کامل فرانت‌اند: روی سیستمی با اینترنت `cd frontend && npm run build` بزنید و پوشه‌ی
`frontend/dist` را همراه پروژه به سرور ببرید؛ `install.sh` اگر `dist` را ببیند از همان استفاده می‌کند و
به Node نیاز ندارد. فونت وزیر هم از قبل لوکال است (بدون CDN).

## توسعه‌ی لوکال

```bash
cd backend
cp ../.env.example .env         # مقادیر را تنظیم کنید (روی ویندوز/لوکال PROVISIONING_ENABLED=false)
go run ./cmd/multivpn seed      # ساخت ادمین
go run ./cmd/multivpn           # اجرای API روی 127.0.0.1:8000

cd ../frontend && npm install && npm run dev
```

> بک‌اند با **Go** نوشته شده و فرانت‌اند با **TypeScript + React (Vite)**. بیلد آفلاین با `go build -mod=vendor`
> (وابستگی‌ها در `backend/vendor/` هستند). نصب کامل روی سرور: `sudo bash install.sh`.

> در ویندوز/لوکال، `PROVISIONING_ENABLED=false` بگذارید. در این حالت اسکریپت‌های provisioning/Xray اجرا
> نمی‌شوند، بک‌اند فقط داده‌ها را مدیریت می‌کند و یک کلید موقتِ توسعه به‌کار می‌رود (نیازی به `SECRET_KEY` نیست).
> در حالت production (`PROVISIONING_ENABLED=true`) اگر `SECRET_KEY` ضعیف/خالی باشد، بک‌اند عمداً بوت نمی‌شود.

## امنیت (پس از ممیزی و تست نفوذ)

پروژه یک ممیزی امنیتی و تست نفوذ چندعاملی را پشت سر گذاشته و کنترل‌های زیر اعمال شده‌اند:

| کنترل | جزئیات |
|-------|--------|
| رمز ادمین تصادفی | نصب‌کننده رمز قوی می‌سازد و یک‌بار چاپ می‌کند؛ `admin/admin` پذیرفته نمی‌شود |
| Fail-closed روی کلید ضعیف | `SECRET_KEY` خالی/پیش‌فرض → بک‌اند در production بوت نمی‌شود |
| محدودسازی لاگین | قفل per-IP پس از ۵ تلاش ناموفق + لاگ حسابرسی + برابرسازی زمان (ضد timing-oracle) |
| باطل‌سازی توکن | `token_version`؛ تغییر رمز همه‌ی توکن‌های قبلی را باطل می‌کند؛ عمر توکن ۸ ساعت |
| TLS اجباری | با دامنه: Let's Encrypt؛ بدون دامنه: گواهی خودامضا (بدون HTTP ساده) |
| بستن پورت‌های داخلی | زنجیره‌ی `input` در nftables دسترسی خارجی به `12345`/`10085` را drop می‌کند |
| ضدجعلِ source-IP | `rp_filter=loose` + قفل per-session L2TP (ip-up/down) + `/32` در WG + کنترل داخلی OpenVPN |
| L2TP قوی | `aes256-sha256-modp2048`؛ حذف `3des`/`sha1`/`modp1024` |
| سخت‌سازی داده‌ها | DB با مجوز `600`، `UMask=077` سرویس، حذف رمز از `.env` پس از seed |
| CORS بسته | پیش‌فرض same-origin (بدون wildcard) |
| هدرهای امنیتی + CSP | middleware اپ: CSP سختِ بدون CDN، `X-Frame-Options: DENY`، `nosniff`، `Referrer-Policy: no-referrer`، HSTS روی TLS، `Cache-Control: no-store` برای API/اشتراک |
| کران ورودی | حدِ طول/بازه روی username/password لاگین و quota/expires/note (ضد DoS، مقادیر منفی/عظیم) و کشِ کوتاهِ مانیتور (ضد اشباع پروسه) |

تغییر رمز از داخل پنل (دکمه‌ی «تغییر رمز») در دسترس است.

## API (خلاصه)

| متد | مسیر | توضیح |
|-----|------|-------|
| POST | `/api/auth/login` | ورود ادمین، دریافت JWT |
| GET  | `/api/users` | لیست کاربران + مصرف |
| POST | `/api/users` | ساخت کاربر (روی هر سه پروتکل) |
| GET  | `/api/users/{id}` | جزئیات کاربر |
| PATCH| `/api/users/{id}` | تغییر سهمیه/انقضا/فعال‌سازی |
| DELETE | `/api/users/{id}` | حذف کاربر از همه‌ی پروتکل‌ها |
| POST | `/api/users/{id}/reset` | صفر کردن مصرف |
| GET  | `/api/users/{id}/configs` | دانلود کانفیگ‌های کلاینت (ovpn/wg/l2tp) |
| GET  | `/api/system/status` | وضعیت سرویس‌ها و Xray |
| GET  | `/api/system/protocols` | آمار زندهٔ مصرف/آنلاین به‌تفکیک هر پروتکل (مانیتور) |
| PATCH | `/api/outbounds/{id}` | ویرایش نام/کانفیگِ اوتباند (تجزیه‌ی مجدد، پاک‌سازی ژئو، sync در صورت فعال‌بودن) |
| POST | `/api/outbounds/{id}/test` | تست اتصال اوتباند + ژئولوکیشنِ IP خروجی (کشور/پرچم) |
| POST | `/api/outbounds/activate-all` | فعال‌سازی همه‌ی اوتباندها (بالانسر round-robin) |
| GET  | `/api/sub/{token}/locations` | لیست لوکیشن‌های در دسترس + انتخابِ فعلیِ کاربر (عمومی) |
| POST | `/api/sub/{token}/location` | انتخابِ لوکیشنِ خروج توسط کاربر (عمومی، token-gated) |

مستندات کامل و تعاملی: `/docs`.

## وضعیت و نقشه‌ی راه

MVP کامل، قابل‌اجرا و امنیت‌سخت‌شده. مسیر توسعه:
- [ ] Hot-reload کاربران در Xray با gRPC (حذف نیاز به restart و pre-flush آمار)
- [ ] رمزنگاری ستون‌های حساسِ DB (کلید خارج از DB) به‌جای ذخیره‌ی خام
- [ ] انتقال توکن به کوکی HttpOnly/Secure به‌جای localStorage
- [ ] فایروال default-deny خودکار (با تشخیص امن پورت SSH)
- [ ] چند نود و load-balance
- [ ] RADIUS برای L2TP به‌جای chap-secrets
- [x] مانیتور زندهٔ هر پروتکل (مصرف/سرعت/آنلاین) با مدال — `/api/system/protocols`
- [x] تستِ اتصال اوتباند (TCP + عبورِ واقعیِ ترافیک با Xrayِ موقت) — `/api/outbounds/{id}/test`
- [x] بالانسرِ round-robin برای اوتباندهای هم‌نام + «فعال‌سازی همه» (توزیعِ per-user، آمار حجم دست‌نخورده)
- [x] انتخابِ لوکیشنِ خروج توسط کاربر در صفحه‌ی اشتراک (با پرچمِ کشور از ژئولوکیشنِ IP خروجی) — `GET/POST /api/sub/{token}/location(s)`
- [x] پرچم‌های کشور به‌صورت آفلاین در پروژه (`frontend/public/flags/`, flag-icons)
- [ ] نمودار مصرف تاریخی و لاگ اتصالات (ماندگاری بلندمدت)
