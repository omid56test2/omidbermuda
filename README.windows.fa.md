# اجرای مستقیم OMID-IRAN BERMUDA 1.0.2 روی ویندوز ۱۱ (بدون Docker)

این نسخه Backend جدید Go است، نه برنامه FastAPI قبلی؛ بنابراین `venv` و `pip install -r requirements.txt` ندارد. فایل اجرایی ویندوز داخل بسته قرار داده شده و نیازی به نصب Go برای اجرای روزمره نیست.

## اجرای سریع

۱. ZIP کامل را در مسیری مثل `C:\OMID-IRAN-BERMUDA` استخراج کن.

۲. روی `start-local.bat` دوبار کلیک کن. یا در PowerShell از ریشه پروژه اجرا کن:

```powershell
.\start-local.bat
```

در اولین اجرا، اسکریپت Xray رسمی Windows x64 نسخه `26.10.21` را از GitHub دریافت می‌کند، SHA-256 آن را با فایل رسمی `.dgst` مقایسه می‌کند؛ قالب رسمی `SHA2-256=` نیز پشتیبانی می‌شود و در `xray\xray.exe` قرار می‌دهد. اگر Xray را قبلاً داری، می‌توانی فایل `xray.exe` را دستی داخل همان پوشه بگذاری.

۳. پس از شروع سرویس، پنل را باز کن:

```text
http://127.0.0.1:8000
```

توکن مدیر در اولین اجرا تولید و در `data-preview\admin-token.txt` ذخیره می‌شود؛ همان توکن در پنجره PowerShell نیز چاپ می‌شود. در کادر Admin Bearer Token آن را وارد کن. این نسخه نام کاربری/رمز عبور جداگانه ندارد.

## اجرای دستی با PowerShell

در ریشه پروژه:

```powershell
$env:PORT = '8000'
$env:LISTEN_ADDR = '127.0.0.1:8000'
$env:ADMIN_TOKEN = (Get-Content .\data-preview\admin-token.txt -Raw).Trim()
$env:DATA_FILE = '.\data-preview\omid_state.json'
$env:XRAY_CONFIG = '.\data-preview\xray-generated.json'
$env:XRAY_BIN = '.\xray\xray.exe'
$env:PUBLIC_BASE_URL = 'http://127.0.0.1:8000'
$env:XRAY_AUTOSTART = 'true'
$env:XRAY_AUTORESTART = 'true'
.\dist\omid-iran-bermuda-windows-amd64.exe
```

برای اجرای دستی، ابتدا `setup-windows.bat` را اجرا کن تا Xray نصب شود و یک بار `start-local.bat` را اجرا کن تا توکن ساخته شود.

## بررسی سرویس

در یک PowerShell دیگر:

```powershell
Invoke-RestMethod http://127.0.0.1:8000/health
Invoke-RestMethod http://127.0.0.1:8000/ready
```

`/health` زنده‌بودن برنامه را بررسی می‌کند و `/ready` وقتی Xray اجرا نشده باشد وضعیت 503 می‌دهد.

## توقف و داده‌ها

در پنجره‌ای که برنامه در آن اجرا شده `Ctrl+C` بزن. اطلاعات کاربران و تنظیمات در `data-preview` ذخیره می‌شوند و با بستن برنامه حذف نمی‌شوند. برای پاک‌کردن محیط آزمایشی، ابتدا برنامه را متوقف کن و سپس پوشه `data-preview` را حذف کن؛ این کار کاربران، اشتراک‌ها و توکن مدیر را هم پاک می‌کند.

## نکات

- سرویس در اجرای محلی فقط روی `127.0.0.1:8000` گوش می‌دهد و به شبکه عمومی/LAN منتشر نمی‌شود.
- لینک‌های اشتراک با `http://127.0.0.1:8000` ساخته می‌شوند و فقط برای تست همان کامپیوتر مناسب‌اند. برای دستگاه دیگر به آدرس شبکه و TLS واقعی نیاز است.
- اگر دانلود Xray به‌خاطر دسترسی GitHub ناموفق بود، `Xray-windows-64.zip` و فایل `.dgst` همان نسخه را از [صفحه رسمی Xray releases](https://github.com/XTLS/Xray-core/releases/tag/v26.10.21) بگیر و فایل `xray.exe` را داخل `xray` قرار بده.
- Go 1.23 یا جدیدتر فقط برای توسعه و Build مجدد لازم است؛ برای اجرای بسته آماده لازم نیست.
