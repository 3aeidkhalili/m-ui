# Bundled Xray-core (نصب آفلاین)

این باینری‌های رسمی Xray-core برای **نصب بدون نیاز به اینترنت آزاد** (مثلاً در ایران) همراه پروژه‌اند.

- `Xray-linux-64.zip` — amd64/x86_64 (بیشتر VPSها)
- `Xray-linux-arm64-v8a.zip` — arm64/aarch64

`install.sh` معماری سرور را تشخیص می‌دهد و به‌صورت خودکار از zip مناسب نصب می‌کند
(اولویت: **بسته‌ی لوکال ← mirror (`GITHUB_PROXY`) ← GitHub رسمی**).

## به‌روزرسانی نسخه‌ی Xray
روی سیستمی با اینترنت آزاد:
```bash
curl -fsSL https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-64.zip -o Xray-linux-64.zip
curl -fsSL https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-arm64-v8a.zip -o Xray-linux-arm64-v8a.zip
```
