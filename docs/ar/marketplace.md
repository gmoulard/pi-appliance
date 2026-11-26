---
layout: default
lang: ar
ref: marketplace
traduit_le: 2026-11-25
---
# الـmarketplace في pi-appliance

تجمع الـmarketplace التطبيقات التي يمكن تثبيتها وإدارتها من وحدة تحكم pi-appliance. يُنشر الكتالوج على هذا الموقع: تعيد وحدات التحكم المثبتة قراءته كل 6 ساعات، ويظهر فيها التطبيق المقبول دون إصدار جديد من pi-appliance.

> **إصدار تجريبي.** صورة النظام مُعتمدة على Raspberry Pi 3 B+ وRaspberry Pi 4؛ ومع كل تغيير يثبّت اختبار آلي كل تطبيق ويفتح صفحته، من المنزل ومن الإنترنت، لكن ما زال يجب تجربة كل تطبيق على جهاز Pi حقيقي. تُفتح الخدمات أيضًا من الإنترنت تحت عنوان الـPi (راجع [الدليل، بالفرنسية]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }})).

## قاعدة pi-appliance

**قاعدة pi-appliance** (Docker وTraefik وخدمات النظام وأدواته ووحدة التحكم والأمر `pia`) تأتي مع صورة النظام: ليست في الكتالوج ولا يمكن إزالتها. وحده المسؤول، بالحساب الذي أُنشئ عند التشغيل الأول أو بحساب مسؤول آخر، يثبّت التطبيقات أدناه ويديرها من وحدة التحكم.

## التطبيقات الموجودة في الكتالوج

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">جارٍ تحميل الكتالوج…</p>
<p id="marketplace-tags-help" hidden>المس وسمًا أو أكثر لعرض التطبيقات التي تحمل واحدًا منها على الأقل؛ والمسه مرة أخرى لإزالته.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="التصفية حسب الوسم" hidden></div>
<p id="marketplace-tags-empty" hidden>لا يوجد تطبيق يحمل هذه الوسوم.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>التطبيق</th><th>الوصف</th><th>الفئة</th><th>الوسوم</th><th>الذاكرة</th><th>القرص</th></tr></thead>
<tbody></tbody>
</table>
</div>

أسماء التطبيقات وأوصافها مأخوذة من ملفات البيان الخاصة بها، وهي مكتوبة بالفرنسية فقط في الوقت الحالي.

تُحمَّل هذه القائمة من كتالوج JSON المُولَّد من ملفات البيان في [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/main/marketplace). تقرأ وحدات التحكم الملف نفسه في مستودع GitHub: يظهر التطبيق المقبول على أجهزة Pi دون إصدار جديد من pi-appliance. بعد أي تعديل على ملف بيان أو على `marketplace/planned.yaml`، أعد توليد الملف بالأمر `go run ./cmd/marketplace-json` من جذر المستودع؛ وإلا فستفشل الاختبارات (`go test ./...`). إن نسيت ذلك، يعيد إجراء آلي في GitHub توليده على `main` بعد القبول. يحتوي اسم الملف على بصمة MD5 لمحتواه؛ ويشير الفهرس `docs/static/pi-appliance-catalog.json` إلى الإصدار الحالي.

تدير وحدة التحكم عدة نسخ من التطبيق، وتشغيلها وإيقافها، والتحديثات مع إمكانية الرجوع، والاحتفاظ بالبيانات أو حذفها عند الإزالة.

## ما هو مخطط له أو قيد الدراسة

لا يمكن تثبيت هذه التطبيقات من الـmarketplace بعد. تأتي القائمة من كتالوج JSON نفسه (`marketplace/planned.yaml`):

<ul id="marketplace-planned" hidden></ul>

**مدمج في وحدة التحكم، خارج الكتالوج**: [Cockpit](https://cockpit-project.org/)، لإدارة نظام الـPi (السجلات والبرامج والشبكة والأقراص وطرفية في المتصفح، للمستخدمين المتمرسين). ليس حاوية بل برنامج نظام: تقدّمه Marketplace وحدة التحكم للمسؤولين، في المنزل فقط، ويُدخَل إليه بحساب pi-appliance دون كلمة مرور أخرى (انظر [المواصفات]({{ '/specification.html' | relative_url }}) (بالفرنسية)، القسم 6.4).

**خارج الكتالوج، Docker Compose حر**: يمكن للمسؤول أيضًا تثبيت تطبيق غير موجود في الكتالوج انطلاقًا من ملف Docker Compose الخاص به، بلصقه أو بقراءته من عنوان ويب (صفحة GitHub مثلًا). هذه الخدمة لا يتحقق منها المشروع ولا يدعمها (راجع [الدليل، بالفرنسية]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }})).

قواعد البيانات: PostgreSQL متاح مع واجهة الويب Adminer. أما MongoDB (على Pi 5 فقط) وRedis وMSSQL (لا توجد صورة للـPi) وSQLite (ملف بسيط) فلا تُقدَّم وحدها: انظر [المواصفات]({{ '/specification.html' | relative_url }}) (بالفرنسية)، القسم 6.3.

روابط المشاركة العامة في Etherpad تحسين مخطط له أيضًا. راجع [خارطة الطريق]({{ '/TODO.html' | relative_url }}) (بالفرنسية) لمتابعة تقدّمها.

## إضافة تطبيق: يمكن للجميع المساهمة

المساهمات مفتوحة. يمكنك اقتراح تطبيق حر مفيد في المنزل (الاستثناء الوحيد حاليًا: n8n، برخصة *fair-code* ‏«Sustainable Use License»، اختاره المشروع عن قصد وأشار إلى ذلك في بطاقته)، أو تحسين ملف بيان موجود، أو الإبلاغ عن مشكلة. يمر الاقتراح عبر pull request؛ وستتم مراجعته وتجربته قبل دمجه.

### 1. إنشاء ملف بيان Docker Compose

أنشئ fork من [مستودع pi-appliance](https://github.com/gmoulard/pi-appliance)، ثم أنشئ `marketplace/<المعرّف>/compose.yaml`. يجب أن يكون المعرّف بأحرف لاتينية صغيرة، وأن يبدأ بحرف، وألا يحتوي إلا على أحرف وأرقام وشرطات.

الملف بيان Docker Compose عادي فيه قسم `x-pi-appliance` يصف التطبيق. مثال على البنية (استبدل القيم والصورة بقيم تطبيقك وصورته):

```yaml
x-pi-appliance:
  name: تطبيقي
  description: وصف واضح للمستخدمين.
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
      label: كلمة مرور المسؤول
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

### 2. احترام قواعد الكتالوج

- اختر صورة Docker ذات **إصدار محدد** (ليس `latest` ولا `stable`) ومتوافقة مع `linux/arm64`، معمارية أجهزة Raspberry Pi المستهدفة.
- صرّح بالخدمة المنشورة في `service`، وبمنفذها الداخلي في `port`، وبتقديرات واقعية بالميغابايت في `memory_mb` و`disk_mb`.
- الحقل `icon` مسار SVG ‏(`d`) على شبكة 24 × 24. يقبل الحقل `login` القيمتين `application` أو `console`؛ ويقبل `access` القيم `public` أو `users` أو `home` أو `admins` (المسؤولون، في المنزل: مخصص للتطبيقات التي تتحكم في الـPi كله، مثل Portainer أو Claude Code).
- صرّح بالمعاملات بأحرف كبيرة في `parameters`. للأسرار، استخدم `generate: true` بدل وضع كلمة مرور في ملف البيان أو في صورة.
- خزّن البيانات الدائمة في وحدات تخزين Docker مسماة. لا تضع في المستودع أسرارًا ولا بيانات شخصية ولا إعدادات خاصة بمستخدم. وحدة التخزين التي يجب ألا تُنسخ احتياطيًا أبدًا (رمز حساب) تحمل الوسم `pi-appliance.backup: "false"` (`labels` وحدة التخزين، كما في Claude Code).
- اذكر رخصة التطبيق وتحقق من أن إعادة توزيعه متوافقة مع المشروع.
- لا تستخدم في `x-pi-appliance` إلا المفاتيح الموثقة: أي مفتاح غير معروف يؤدي إلى رفض ملف البيان.
- تجنب الخيارات التي تمنح التحكم في الـPi كله (حاوية ذات امتيازات، الوصول إلى Docker، مجلد من الـPi، شبكة المضيف، صلاحيات إضافية): لا يمكن تثبيت تطبيق جديد يحملها من الكتالوج على الإنترنت، بل بعد إصدار جديد من pi-appliance فقط. التطبيق الذي يصل إلى Docker أو يعمل في حاوية ذات امتيازات يجب أن يصرّح أيضًا بـ`access: admins`، وتطلب وحدة التحكم تأكيد تثبيته.
- التطبيق المخطط له وغير الجاهز بعد يوضع في `marketplace/planned.yaml` (الاسم والوصف والأيقونة والفئة وسطر خارطة الطريق): يعرضه الموقع بعبارة «قريبًا».

التعريف الكامل للحقول وقواعد التحقق موجود في [المواصفات، القسم 6.5]({{ '/specification.html' | relative_url }}) (بالفرنسية).

### 3. التحقق والاقتراح

من جذر المستودع، شغّل:

```shell
go run ./cmd/marketplace-json
go test ./...
```

تتحقق الاختبارات خصوصًا من صحة ملفات البيان، ومن أن للصور إصدارًا ثابتًا، ومن التصريح بمتغيرات Compose. إن أمكن، جرّب أيضًا تثبيت تطبيقك وإيقافه وإعادة تشغيله وإزالته باستخدام Docker على جهاز تجربة.

ثم افتح pull request نحو `main`، مع وصف للتطبيق والموارد اللازمة وخطوات التجربة والقيود المعروفة إن وجدت. بعد قبول الـpull request على `main`، تقرأ الأجهزة الكتالوج الجديد على الإنترنت خلال 6 ساعات، دون إصدار جديد من pi-appliance.

## روابط مفيدة

- [الشيفرة المصدرية للـmarketplace](https://github.com/gmoulard/pi-appliance/tree/main/marketplace)
- [مواصفات ملفات البيان]({{ '/specification.html' | relative_url }}) (بالفرنسية)
- [خارطة الطريق]({{ '/TODO.html' | relative_url }}) (بالفرنسية)
- [مستودع pi-appliance](https://github.com/gmoulard/pi-appliance): افتح pull request من الـfork الخاص بك لاقتراح مساهمتك.
