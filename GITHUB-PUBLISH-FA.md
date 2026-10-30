# راهنمای انتشار OMID-IRAN BERMUDA در GitHub

## ۱. قبل از عمومی‌کردن مخزن

- فایل `LICENSE-STATUS.md` را بخوان. فعلاً هیچ مجوز متن‌بازی برای استفاده مجدد از کد صادر نشده است.
- مطمئن شو مالک فایل‌ها و دارایی‌های رابط کاربری هستی یا اجازه بازنشر آن‌ها را داری.
- هیچ‌وقت فایل `.env`، پوشه‌های `data` و `data-preview`، فایل‌های Backup، توکن مدیر یا لینک‌های اشتراک واقعی را Commit نکن.

## ۲. ساخت مخزن

۱. وارد GitHub شو و یک Repository جدید بساز. برای بررسی اولیه بهتر است آن را Private نگه داری.
۲. فایل `OMID-IRAN-BERMUDA-v1.0.1-GitHub-Source.zip` را Extract کن.
۳. وارد پوشه `OMID-IRAN-BERMUDA` شو؛ باید فایل‌های `README.md`، `go.mod` و پوشه‌های `cmd` و `internal` را ببینی.
۴. PowerShell را در همین پوشه باز کن و این دستورات را اجرا کن. آدرس مخزن را با آدرس واقعی خودت جایگزین کن:

```powershell
git init
git add .
git status
git commit -m "Initial release: OMID-IRAN BERMUDA 1.0.1"
git branch -M main
git remote add origin https://github.com/USERNAME/REPOSITORY.git
git push -u origin main
```

## ۳. ساخت Release نسخه 1.0.1

پس از سبزشدن Workflow مربوط به CI، تگ نسخه را Push کن:

```powershell
git tag -a v1.0.1 -m "OMID-IRAN BERMUDA 1.0.1"
git push origin v1.0.1
```

Workflow با نام `Release` اجرا می‌شود و پس از موفقیت، این دارایی‌ها را در GitHub Releases قرار می‌دهد:

- بسته آماده اجرای Windows x64
- باینری Linux AMD64
- فایل SHA256 برای بررسی صحت دانلود

## ۴. بررسی نهایی

در زبانه Actions هر دو Workflow به نام‌های `CI` و `Release` را بررسی کن. انتشار را تا سبزشدن تست Docker و Xray اعلام نکن. این Workflowها برنامه را روی هاست ابری Deploy نمی‌کنند.

## نکته مجوز

GitHub عمومی‌بودن مخزن به معنی اعطای مجوز استفاده مجدد از کد نیست. تا زمانی که مالکیت و مجوز همه اجزای پروژه روشن نشده، مخزن را Private نگه دار.
