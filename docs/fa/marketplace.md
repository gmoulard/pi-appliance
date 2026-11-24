---
layout: default
lang: fa
ref: marketplace
traduit_le: 2026-11-24
---
# marketplace در pi-appliance

marketplace برنامه‌هایی را گرد هم می‌آورد که می‌توان از کنسول pi-appliance نصب و مدیریت کرد. فهرست هنوز درون خود برنامه است: برنامه‌ای که پیشنهاد می‌شود تنها پس از تأیید مشارکت و انتشار نسخهٔ تازه‌ای از کنسول در دسترس قرار می‌گیرد.

> **نسخهٔ آزمایشی.** ایمیج روی Raspberry Pi 3 B+ و Raspberry Pi 4 تأیید شده است؛ هر برنامه هنوز باید یکی‌یکی روی یک Pi واقعی آزموده شود. دسترسی به سرویس‌ها از اینترنت هنوز در دسترس نیست.

## پایهٔ pi-appliance

**پایهٔ pi-appliance** (Docker، Traefik، سرویس‌ها و ابزارهای سیستم، کنسول و فرمان `pia`) همراه ایمیج می‌آید: در فهرست نیست و حذف نمی‌شود. تنها یک مدیر، با حسابی که در نخستین راه‌اندازی ساخته شده یا حساب مدیر دیگری، برنامه‌های زیر را از کنسول نصب و مدیریت می‌کند.

## برنامه‌هایی که در فهرست هستند

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">در حال بارگیری فهرست…</p>
<p id="marketplace-tags-help" hidden>روی یک یا چند برچسب بزنید تا فقط برنامه‌هایی را ببینید که دست‌کم یکی از آن‌ها را دارند؛ برای برداشتن برچسب، دوباره روی آن بزنید.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="پالایش بر اساس برچسب" hidden></div>
<p id="marketplace-tags-empty" hidden>هیچ برنامه‌ای این برچسب‌ها را ندارد.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>برنامه</th><th>توضیح</th><th>دسته</th><th>برچسب‌ها</th><th>حافظه</th><th>دیسک</th></tr></thead>
<tbody></tbody>
</table>
</div>

نام و توضیح برنامه‌ها از مانیفست‌هایشان می‌آید و فعلاً فقط به فرانسوی نوشته شده است.

این فهرست از فهرست JSON بارگیری می‌شود که از مانیفست‌های [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace) ساخته می‌شود. کنسول‌ها همین فایل را در مخزن GitHub می‌خوانند: برنامه‌ای که پذیرفته شود بدون نسخهٔ تازهٔ pi-appliance روی Piها ظاهر می‌شود. پس از هر تغییر در یک مانیفست یا در `marketplace/planned.yaml`، فایل را با `go run ./cmd/marketplace-json` از ریشهٔ مخزن دوباره بسازید؛ وگرنه آزمون‌ها (`go test ./...`) شکست می‌خورند. اگر فراموش کنید، یک کنش GitHub پس از پذیرش آن را روی `master` دوباره می‌سازد. نام فایل شامل MD5 محتوای آن است؛ نمایهٔ `docs/static/pi-appliance-catalog.json` نسخهٔ کنونی را نشان می‌دهد.

کنسول چند نمونه از هر برنامه، راه‌اندازی و توقف آن‌ها، به‌روزرسانی با امکان بازگشت، و نگه‌داشتن یا پاک‌کردن داده‌ها هنگام حذف را مدیریت می‌کند.

## آنچه پیش‌بینی شده یا در دست بررسی است

این برنامه‌ها هنوز از marketplace نصب‌شدنی نیستند. فهرست از همان فهرست JSON (`marketplace/planned.yaml`) می‌آید:

<ul id="marketplace-planned" hidden></ul>

**درون کنسول، بیرون از فهرست**: [Cockpit](https://cockpit-project.org/)، مدیریت سیستم Pi (گزارش‌ها، برنامه‌ها، شبکه، دیسک‌ها و ترمینال در مرورگر، برای کاربران باتجربه). این یک کانتینر نیست بلکه نرم‌افزار سیستم است: Marketplace کنسول آن را فقط در خانه به مدیران پیشنهاد می‌کند و با حساب pi-appliance و بدون گذرواژهٔ دیگری وارد آن می‌شوید (نگاه کنید به [مشخصات]({{ '/specification.html' | relative_url }}) (به فرانسوی)، بخش 6.4).

پایگاه‌های داده: PostgreSQL همراه با رابط وب Adminer در دسترس است. MongoDB (فقط Pi 5)، Redis، MSSQL (بدون ایمیج برای Pi) و SQLite (یک فایل ساده) به‌تنهایی پیشنهاد نمی‌شوند: نگاه کنید به [مشخصات]({{ '/specification.html' | relative_url }}) (به فرانسوی)، بخش 6.3.

پیوندهای اشتراک‌گذاری عمومی Etherpad هم بهبودی پیش‌بینی‌شده است. برای پیگیری پیشرفت آن، [نقشهٔ راه]({{ '/TODO.html' | relative_url }}) (به فرانسوی) را ببینید.

## افزودن برنامه: همه می‌توانند مشارکت کنند

مشارکت آزاد است. می‌توانید یک برنامهٔ آزاد و سودمند برای خانه پیشنهاد کنید (تنها استثنا فعلاً: n8n با مجوز *fair-code* ‏«Sustainable Use License»، که پروژه آگاهانه برگزیده و در کارت آن ذکر کرده است)، یک مانیفست موجود را بهتر کنید یا مشکلی را گزارش دهید. پیشنهاد از راه pull request انجام می‌شود؛ پیش از ادغام بازبینی و آزموده می‌شود.

### ۱. ساختن یک مانیفست Docker Compose

از [مخزن pi-appliance](https://github.com/gmoulard/pi-appliance) یک fork بسازید، سپس `marketplace/<شناسه>/compose.yaml` را بسازید. شناسه باید با حروف کوچک لاتین باشد، با یک حرف آغاز شود و فقط حرف، رقم و خط تیره داشته باشد.

این فایل یک مانیفست معمولی Docker Compose است با بخشی به نام `x-pi-appliance` که برنامه را توصیف می‌کند. نمونهٔ ساختار (مقدارها و ایمیج را با مقدارها و ایمیج برنامهٔ خودتان جایگزین کنید):

```yaml
x-pi-appliance:
  name: برنامهٔ من
  description: توضیحی روشن برای کاربران.
  icon: M4 5h16v14H4z
  category: Outils
  tags: [administration]
  license: AGPL-3.0
  subdomain: outil
  service: app
  port: 8080
  memory_mb: 256
  disk_mb: 512
  login: application
  access: users
  parameters:
    - name: ADMIN_PASSWORD
      label: گذرواژهٔ مدیر
      generate: true

services:
  app:
    image: editeur/image:1.2.3
    restart: unless-stopped
    environment:
      ADMIN_PASSWORD: ${ADMIN_PASSWORD}
    volumes:
      - data:/var/lib/application

volumes:
  data:
```

### ۲. رعایت قواعد فهرست

- یک ایمیج Docker با **نسخهٔ دقیق** (نه `latest` یا `stable`) و سازگار با `linux/arm64`، معماری Raspberry Piهای مورد نظر، برگزینید.
- سرویس منتشرشده را در `service`، درگاه داخلی آن را در `port`، و برآوردهای واقع‌بینانه به مگابایت را در `memory_mb` و `disk_mb` اعلام کنید.
- فیلد `icon` یک مسیر SVG ‏(`d`) روی شبکهٔ 24 × 24 است. فیلد `login` مقدار `application` یا `console` را می‌پذیرد؛ `access` مقدار `public`، `users`، `home` یا `admins` را می‌پذیرد (مدیران، در خانه: ویژهٔ برنامه‌هایی که بر کل Pi کنترل دارند، مانند Portainer یا Claude Code).
- پارامترها را با حروف بزرگ در `parameters` اعلام کنید. برای رازها، به‌جای گذاشتن گذرواژه در مانیفست یا ایمیج، از `generate: true` استفاده کنید.
- داده‌های ماندگار را در volumeهای نام‌دار Docker نگه دارید. هیچ راز، دادهٔ شخصی یا تنظیم ویژهٔ یک کاربر را در مخزن نگذارید. volumeی که هرگز نباید پشتیبان‌گیری شود (توکن یک حساب) برچسب `pi-appliance.backup: "false"` دارد (`labels` همان volume، مانند Claude Code).
- مجوز برنامه را بنویسید و بررسی کنید که بازتوزیع آن با پروژه سازگار باشد.
- در `x-pi-appliance` فقط کلیدهای مستند را به کار ببرید: یک کلید ناشناخته باعث رد شدن مانیفست می‌شود.
- از گزینه‌هایی که کنترل کل Pi را می‌دهند پرهیز کنید (کانتینر ممتاز، دسترسی به Docker، پوشه‌ای از Pi، شبکهٔ میزبان، مجوزهای افزوده): برنامهٔ تازه‌ای که این‌ها را دارد از فهرست برخط نصب‌شدنی نیست و فقط پس از نسخهٔ تازه‌ای از pi-appliance نصب می‌شود. برنامه‌ای که به Docker دسترسی دارد یا کانتینر ممتاز است باید افزون بر این `access: admins` را اعلام کند، و کنسول نصب آن را به تأیید می‌رساند.
- برنامه‌ای که پیش‌بینی شده ولی هنوز آماده نیست در `marketplace/planned.yaml` قرار می‌گیرد (نام، توضیح، نماد، دسته و سطر نقشهٔ راه): سایت آن را «به‌زودی» نشان می‌دهد.

تعریف کامل فیلدها و قواعد اعتبارسنجی در [مشخصات، بخش 6.5]({{ '/specification.html' | relative_url }}) (به فرانسوی) آمده است.

### ۳. بررسی و پیشنهاد

از ریشهٔ مخزن، این را اجرا کنید:

```shell
go run ./cmd/marketplace-json
go test ./...
```

آزمون‌ها به‌ویژه بررسی می‌کنند که مانیفست‌ها معتبر باشند، ایمیج‌ها نسخهٔ ثابت داشته باشند و متغیرهای Compose اعلام شده باشند. اگر ممکن است، نصب، توقف، راه‌اندازی دوباره و حذف برنامه‌تان را نیز با Docker روی یک دستگاه آزمایشی امتحان کنید.

سپس یک pull request به سوی `master` باز کنید، همراه با توضیح برنامه، منابع لازم، گام‌های آزمون و محدودیت‌های شناخته‌شده. پس از پذیرش pull request روی `master`، دستگاه‌ها ظرف ۶ ساعت فهرست برخط تازه را می‌خوانند، بدون نسخهٔ تازهٔ pi-appliance.

## پیوندهای سودمند

- [کد منبع marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [مشخصات مانیفست‌ها]({{ '/specification.html' | relative_url }}) (به فرانسوی)
- [نقشهٔ راه]({{ '/TODO.html' | relative_url }}) (به فرانسوی)
- [مخزن pi-appliance](https://github.com/gmoulard/pi-appliance): برای پیشنهاد مشارکت خود، از fork خودتان یک pull request باز کنید.
