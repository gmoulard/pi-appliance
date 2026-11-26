---
layout: default
lang: tr
ref: marketplace
traduit_le: 2026-11-25
---
# pi-appliance marketplace

Marketplace, pi-appliance konsolundan kurup yönetebileceğiniz uygulamaları bir araya getirir. Katalog bu sitede yayımlanır: kurulu konsollar onu 6 saatte bir yeniden okur ve kabul edilen bir uygulama, pi-appliance'ın yeni bir sürümü olmadan orada görünür.

> **Deneysel sürüm.** İmaj Raspberry Pi 3 B+ ve Raspberry Pi 4 üzerinde doğrulanmıştır; her değişiklikte otomatik bir deneme her uygulamayı kurar ve sayfasını evden ve internetten açar, ancak her birinin yine de gerçek bir Pi üzerinde denenmesi gerekir. Hizmetler internetten de Pi'nin adresi altında açılır ([Fransızca kılavuza]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }}) bakın).

## pi-appliance temeli

**pi-appliance temeli** (Docker, Traefik, sistem hizmetleri ve araçları, konsol ve `pia` komutu) imajla birlikte gelir: katalogda yer almaz ve kaldırılamaz. Aşağıdaki uygulamaları konsoldan yalnızca bir yönetici, ilk açılışta oluşturulan hesapla ya da başka bir yönetici hesabıyla kurar ve yönetir.

## Katalogda zaten bulunan uygulamalar

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">Katalog yükleniyor…</p>
<p id="marketplace-tags-help" hidden>Yalnızca en az birine sahip uygulamaları görmek için bir veya daha fazla etikete dokunun; kaldırmak için yeniden dokunun.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Etikete göre süz" hidden></div>
<p id="marketplace-tags-empty" hidden>Bu etiketlere sahip uygulama yok.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Uygulama</th><th>Açıklama</th><th>Kategori</th><th>Etiketler</th><th>Bellek</th><th>Disk</th></tr></thead>
<tbody></tbody>
</table>
</div>

Uygulamaların adları ve açıklamaları manifestlerinden gelir ve şimdilik yalnızca Fransızca yazılmıştır.

Bu liste, [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/main/marketplace) içindeki manifestlerden üretilen JSON kataloğundan yüklenir. Konsollar aynı dosyayı GitHub deposunda okur: kabul edilen bir uygulama, pi-appliance'ın yeni bir sürümü olmadan Pi'lerde görünür. Bir manifesti veya `marketplace/planned.yaml` dosyasını her değiştirdiğinizde, dosyayı deponun kökünden `go run ./cmd/marketplace-json` ile yeniden üretin; aksi hâlde testler (`go test ./...`) başarısız olur. Unutursanız, bir GitHub eylemi kabulden sonra `main` üzerinde onu yeniden üretir. Dosya adı içeriğinin MD5 değerini içerir; `docs/static/pi-appliance-catalog.json` dizini güncel sürümü belirtir.

Konsol birden çok örneği, bunların başlatılmasını ve durdurulmasını, geri dönüşlü güncellemeleri ve kaldırma sırasında verilerin saklanmasını ya da silinmesini yönetir.

## Planlanan veya incelenenler

Bu uygulamalar henüz marketplace'ten kurulamıyor. Liste aynı JSON kataloğundan (`marketplace/planned.yaml`) gelir:

<ul id="marketplace-planned" hidden></ul>

**Konsola yerleşik, katalog dışında**: [Cockpit](https://cockpit-project.org/), Pi sisteminin yönetimi (günlükler, programlar, ağ, diskler ve tarayıcıda terminal, deneyimli kullanıcılar için). Bir konteyner değil, bir sistem yazılımıdır: konsolun Marketplace'i onu yöneticilere yalnızca evde sunar ve başka bir parola olmadan pi-appliance hesabınızla girilir (bkz. [şartname]({{ '/specification.html' | relative_url }}) (Fransızca), bölüm 6.4).

**Katalog dışında, serbest Docker Compose**: bir yönetici, katalogda olmayan bir uygulamayı da Docker Compose dosyasından kurabilir; dosya yapıştırılır ya da bir web adresinden (örneğin bir GitHub sayfasından) okunur. Böyle bir hizmet proje tarafından ne denetlenir ne de desteklenir ([Fransızca kılavuza]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }}) bakın).

Veritabanları: PostgreSQL, Adminer web arayüzüyle birlikte kullanılabilir. MongoDB (yalnızca Pi 5), Redis, MSSQL (Pi için imaj yok) ve SQLite (basit bir dosya) tek başına sunulmaz: bkz. [şartname]({{ '/specification.html' | relative_url }}) (Fransızca), bölüm 6.3.

Etherpad için herkese açık paylaşım bağlantıları da planlanan bir iyileştirmedir. İlerlemesini görmek için [yol haritasına]({{ '/TODO.html' | relative_url }}) (Fransızca) bakın.

## Uygulama eklemek: herkes katkıda bulunabilir

Katkılara açığız. Evde işe yarayan özgür bir uygulama önerebilir (şimdilik tek istisna: projenin bilinçli olarak seçtiği ve sayfasında belirtilen, *fair-code* "Sustainable Use License" lisanslı n8n), mevcut bir manifesti iyileştirebilir veya bir sorun bildirebilirsiniz. Bir öneri pull request ile yapılır; eklenmeden önce incelenir ve denenir.

### 1. Bir Docker Compose manifesti oluşturun

[pi-appliance deposunu](https://github.com/gmoulard/pi-appliance) fork edin, ardından `marketplace/<kimlik>/compose.yaml` dosyasını oluşturun. Kimlik küçük harfle yazılmalı, bir harfle başlamalı ve yalnızca harf, rakam ve tire içermelidir.

Dosya, uygulamayı tanımlayan bir `x-pi-appliance` bölümü olan sıradan bir Docker Compose manifestidir. Yapı örneği (değerleri ve imajı kendi uygulamanızınkilerle değiştirin):

```yaml
x-pi-appliance:
  name: Uygulamam
  description: Kullanıcılar için açık bir açıklama.
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
      label: Yönetici parolası
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

### 2. Katalog kurallarına uyun

- **Kesin bir sürüme** sahip (`latest` veya `stable` değil) ve hedeflenen Raspberry Pi'lerin mimarisi olan `linux/arm64` ile uyumlu bir Docker imajı seçin.
- Yayımlanan hizmeti `service` içinde, iç bağlantı noktasını `port` içinde ve `memory_mb` ile `disk_mb` için MB cinsinden gerçekçi tahminleri belirtin.
- `icon` alanı 24 × 24 bir ızgara üzerinde bir SVG yoludur (`d`). `login` alanı `application` veya `console` kabul eder; `access` alanı `public`, `users`, `home` veya `admins` kabul eder (yöneticiler, evde: Portainer veya Claude Code gibi tüm Pi'yi kontrol eden uygulamalara ayrılmıştır).
- Parametreleri `parameters` içinde büyük harfle belirtin. Gizli bilgiler için manifeste veya imaja parola koymak yerine `generate: true` kullanın.
- Kalıcı verileri adlandırılmış Docker birimlerinde saklayın. Depoya gizli bilgi, kişisel veri veya bir kullanıcıya özgü ayar koymayın. Hiçbir zaman yedeklenmemesi gereken bir birim (bir hesabın belirteci) `pi-appliance.backup: "false"` etiketini taşır (birimin `labels` alanı, Claude Code'daki gibi).
- Uygulamanın lisansını belirtin ve yeniden dağıtımının projeyle uyumlu olduğunu kontrol edin.
- `x-pi-appliance` içinde yalnızca belgelenmiş anahtarları kullanın: bilinmeyen bir anahtar manifestin reddedilmesine yol açar.
- Tüm Pi üzerinde kontrol veren seçeneklerden kaçının (ayrıcalıklı konteyner, Docker'a erişim, Pi'nin bir klasörü, ana makine ağı, eklenen yetkiler): bunlara sahip yeni bir uygulama çevrimiçi katalogdan kurulamaz, ancak pi-appliance'ın yeni bir sürümünden sonra kurulabilir. Docker'a erişimi olan veya ayrıcalıklı konteyner olan bir uygulama ayrıca `access: admins` belirtmelidir ve konsol kurulumunu onaylatır.
- Planlanan ama henüz hazır olmayan bir uygulama `marketplace/planned.yaml` dosyasına girer (ad, açıklama, simge, kategori ve yol haritası satırı): site onu "Yakında" olarak gösterir.

Alanların tam tanımı ve doğrulama kuralları [şartnamenin 6.5 bölümündedir]({{ '/specification.html' | relative_url }}) (Fransızca).

### 3. Kontrol edin ve önerin

Deponun kökünden şunu çalıştırın:

```shell
go run ./cmd/marketplace-json
go test ./...
```

Testler özellikle manifestlerin geçerli olduğunu, imajların sabit bir sürümü olduğunu ve Compose değişkenlerinin tanımlandığını kontrol eder. Mümkünse uygulamanızın kurulumunu, durdurulmasını, yeniden başlatılmasını ve kaldırılmasını bir test makinesinde Docker ile de deneyin.

Ardından uygulamanın açıklaması, gereken kaynaklar, test adımları ve bilinen sınırlamalarla birlikte `main` dalına bir pull request açın. Pull request `main` üzerinde kabul edildikten sonra cihazlar yeni çevrimiçi kataloğu, pi-appliance'ın yeni bir sürümü olmadan 6 saat içinde okur.

## Yararlı bağlantılar

- [Marketplace'in kaynak kodu](https://github.com/gmoulard/pi-appliance/tree/main/marketplace)
- [Manifestlerin şartnamesi]({{ '/specification.html' | relative_url }}) (Fransızca)
- [Yol haritası]({{ '/TODO.html' | relative_url }}) (Fransızca)
- [pi-appliance deposu](https://github.com/gmoulard/pi-appliance): katkınızı önermek için fork'unuzdan bir pull request açın.
