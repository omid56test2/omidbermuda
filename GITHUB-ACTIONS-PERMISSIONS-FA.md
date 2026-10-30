# رفع خطای Permission denied در GitHub Actions

این نسخه دو اصلاح دارد:

1. اسکریپت‌های `scripts/*.sh` با مجوز اجرایی Unix (`100755`) بسته‌بندی شده‌اند.
2. Workflowها و Makefile اسکریپت‌ها را با `bash` اجرا می‌کنند؛ بنابراین حتی اگر Git یا ZIP بیت اجرایی را حفظ نکند، CI با خطای `Permission denied` متوقف نمی‌شود.

برای اصلاح مخزن موجود بدون جایگزینی کل فایل‌ها، این دستورات را در ریشه مخزن اجرا کنید (Git Bash یا محیط Linux):

```bash
chmod +x scripts/verify.sh scripts/smoke.sh scripts/docker-smoke.sh
git add --chmod=+x scripts/verify.sh scripts/smoke.sh scripts/docker-smoke.sh
git add .github/workflows/ci.yml .github/workflows/release.yml Makefile
git commit -m "Fix executable permissions for CI scripts"
git push
```

حتی بدون تغییر مجوزها نیز Workflow اصلاح‌شده با `bash ./scripts/...` باید قابل اجرا باشد. پس از Push، هر دو Job مربوط به verify و Docker smoke را دوباره بررسی کنید.
