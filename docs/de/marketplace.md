---
layout: default
lang: de
ref: marketplace
traduit_le: 2026-11-25
---
# pi-appliance-Marketplace

Der Marketplace versammelt die Anwendungen, die Sie über die pi-appliance-Konsole installieren und verwalten können. Der Katalog wird auf dieser Website veröffentlicht: Bereits installierte Konsolen lesen ihn alle 6 Stunden neu, und eine angenommene Anwendung erscheint dort ohne neue Version von pi-appliance.

> **Experimentelle Version.** Das Image ist auf Raspberry Pi 3 B+ und Raspberry Pi 4 geprüft; bei jeder Änderung installiert ein automatischer Test jede Anwendung und öffnet ihre Seite, zu Hause und aus dem Internet, aber jede muss noch auf einem echten Pi ausprobiert werden. Die Dienste öffnen sich auch aus dem Internet, unter der Adresse des Pi (siehe die [Anleitung, auf Französisch]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }})).

## Die pi-appliance-Basis

Die **pi-appliance-Basis** (Docker, Traefik, die Systemdienste und -werkzeuge, die Konsole und der Befehl `pia`) wird mit dem Image geliefert: Sie ist nicht im Katalog und lässt sich nicht deinstallieren. Nur ein Administrator, mit dem beim ersten Start angelegten Konto oder einem anderen Administratorkonto, installiert und verwaltet die folgenden Anwendungen über die Konsole.

## Anwendungen, die schon im Katalog sind

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">Katalog wird geladen…</p>
<p id="marketplace-tags-help" hidden>Tippen Sie auf ein oder mehrere Schlagwörter, um nur die Anwendungen zu sehen, die mindestens eines davon haben; tippen Sie erneut darauf, um es zu entfernen.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Nach Schlagwort filtern" hidden></div>
<p id="marketplace-tags-empty" hidden>Keine Anwendung hat diese Schlagwörter.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Anwendung</th><th>Beschreibung</th><th>Kategorie</th><th>Schlagwörter</th><th>Speicher</th><th>Festplatte</th></tr></thead>
<tbody></tbody>
</table>
</div>

Die Namen und Beschreibungen der Anwendungen stammen aus ihren Manifesten und sind vorerst nur auf Französisch verfasst.

Diese Liste wird aus dem JSON-Katalog geladen, der aus den Manifesten in [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace) erzeugt wird. Die Konsolen lesen dieselbe Datei im GitHub-Repository: Eine angenommene Anwendung erscheint auf den Pis ohne neue Version von pi-appliance. Erzeugen Sie die Datei nach jeder Änderung eines Manifests oder von `marketplace/planned.yaml` mit `go run ./cmd/marketplace-json` im Hauptverzeichnis des Repositorys neu; sonst schlagen die Tests (`go test ./...`) fehl. Wenn Sie es vergessen, erzeugt eine GitHub-Action sie nach der Annahme auf `master` neu. Der Dateiname enthält die MD5-Summe ihres Inhalts; der Index `docs/static/pi-appliance-catalog.json` nennt die aktuelle Version.

Die Konsole verwaltet mehrere Instanzen, deren Start und Stopp, Aktualisierungen mit Rückkehr zur vorigen Version sowie das Behalten oder Löschen der Daten bei der Deinstallation.

## Was geplant ist oder geprüft wird

Diese Anwendungen lassen sich noch nicht über den Marketplace installieren. Die Liste stammt aus demselben JSON-Katalog (`marketplace/planned.yaml`):

<ul id="marketplace-planned" hidden></ul>

**In die Konsole eingebaut, außerhalb des Katalogs**: [Cockpit](https://cockpit-project.org/), die Verwaltung des Pi-Systems (Protokolle, Programme, Netzwerk, Festplatten und Terminal im Browser, für erfahrene Nutzer). Es ist kein Container, sondern Systemsoftware: Der Marketplace der Konsole bietet es Administratoren an, nur zu Hause, und man meldet sich mit seinem pi-appliance-Konto an, ohne weiteres Passwort (siehe die [Spezifikation]({{ '/specification.html' | relative_url }}) (auf Französisch), Abschnitt 6.4).

**Außerhalb des Katalogs, freies Docker Compose**: Ein Administrator kann auch eine Anwendung installieren, die nicht im Katalog ist, aus ihrer Docker-Compose-Datei, eingefügt oder von einer Webadresse gelesen (zum Beispiel einer GitHub-Seite). Ein solcher Dienst wird vom Projekt weder geprüft noch unterstützt (siehe die [Anleitung, auf Französisch]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }})).

Datenbanken: PostgreSQL ist verfügbar, mit der Weboberfläche Adminer. MongoDB (nur Pi 5), Redis, MSSQL (kein Image für den Pi) und SQLite (eine einfache Datei) werden nicht einzeln angeboten: siehe die [Spezifikation]({{ '/specification.html' | relative_url }}) (auf Französisch), Abschnitt 6.3.

Öffentliche Freigabelinks für Etherpad sind ebenfalls eine geplante Verbesserung. Den Stand finden Sie im [Fahrplan]({{ '/TODO.html' | relative_url }}) (auf Französisch).

## Eine Anwendung hinzufügen: Jeder kann mitmachen

Beiträge sind willkommen. Sie können eine freie Anwendung vorschlagen, die zu Hause nützlich ist (einzige Ausnahme bisher: n8n, unter der *Fair-Code*-Lizenz „Sustainable Use License“, bewusst vom Projekt gewählt und auf seiner Karte vermerkt), ein bestehendes Manifest verbessern oder ein Problem melden. Ein Vorschlag erfolgt über einen Pull-Request; er wird vor der Aufnahme geprüft und getestet.

### 1. Ein Docker-Compose-Manifest erstellen

Erstellen Sie einen Fork des [pi-appliance-Repositorys](https://github.com/gmoulard/pi-appliance) und legen Sie `marketplace/<kennung>/compose.yaml` an. Die Kennung muss kleingeschrieben sein, mit einem Buchstaben beginnen und darf nur Buchstaben, Ziffern und Bindestriche enthalten.

Die Datei ist ein übliches Docker-Compose-Manifest mit einem Abschnitt `x-pi-appliance`, der die Anwendung beschreibt. Beispielaufbau (ersetzen Sie die Werte und das Image durch die Ihrer Anwendung):

```yaml
x-pi-appliance:
  name: Meine Anwendung
  description: Eine klare Beschreibung für die Nutzer.
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
      label: Administratorpasswort
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

### 2. Die Regeln des Katalogs einhalten

- Wählen Sie ein Docker-Image mit einer **genauen Version** (nicht `latest` oder `stable`), das mit `linux/arm64` kompatibel ist, der Architektur der vorgesehenen Raspberry Pis.
- Geben Sie den veröffentlichten Dienst in `service` an, seinen internen Port in `port` sowie realistische Schätzungen in MB für `memory_mb` und `disk_mb`.
- Das Feld `icon` ist ein SVG-Pfad (`d`) auf einem 24 × 24-Raster. Das Feld `login` akzeptiert `application` oder `console`; `access` akzeptiert `public`, `users`, `home` oder `admins` (Administratoren, zu Hause: für Anwendungen, die den ganzen Pi steuern, etwa Portainer oder Claude Code).
- Geben Sie die Parameter in Großbuchstaben in `parameters` an. Verwenden Sie für Geheimnisse `generate: true`, statt ein Passwort ins Manifest oder in ein Image zu schreiben.
- Speichern Sie dauerhafte Daten in benannten Docker-Volumes. Legen Sie weder Geheimnisse noch persönliche Daten noch benutzerspezifische Einstellungen ins Repository. Ein Volume, das nie gesichert werden darf (Token eines Kontos), trägt das Label `pi-appliance.backup: "false"` (`labels` des Volumes, wie bei Claude Code).
- Geben Sie die Lizenz der Anwendung an und prüfen Sie, ob ihre Weitergabe mit dem Projekt vereinbar ist.
- Verwenden Sie in `x-pi-appliance` nur die dokumentierten Schlüssel: Ein unbekannter Schlüssel führt zur Ablehnung des Manifests.
- Vermeiden Sie Optionen, die die Kontrolle über den ganzen Pi geben (privilegierter Container, Zugriff auf Docker, ein Ordner des Pi, Host-Netzwerk, zusätzliche Rechte): Eine neue Anwendung damit lässt sich nicht aus dem Online-Katalog installieren, sondern erst nach einer neuen Version von pi-appliance. Eine Anwendung mit Zugriff auf Docker oder als privilegierter Container muss zusätzlich `access: admins` angeben, und die Konsole lässt ihre Installation bestätigen.
- Eine geplante, aber noch nicht fertige Anwendung kommt in `marketplace/planned.yaml` (Name, Beschreibung, Symbol, Kategorie und Zeile im Fahrplan): Die Website zeigt sie als „Demnächst“.

Die vollständige Beschreibung der Felder und die Prüfregeln stehen in der [Spezifikation, Abschnitt 6.5]({{ '/specification.html' | relative_url }}) (auf Französisch).

### 3. Prüfen und vorschlagen

Führen Sie im Hauptverzeichnis des Repositorys aus:

```shell
go run ./cmd/marketplace-json
go test ./...
```

Die Tests prüfen vor allem, ob die Manifeste gültig sind, ob die Images eine feste Version haben und ob die Compose-Variablen angegeben sind. Testen Sie nach Möglichkeit auch Installation, Stopp, Neustart und Deinstallation Ihrer Anwendung mit Docker auf einem Testrechner.

Öffnen Sie dann einen Pull-Request gegen `master`, mit einer Beschreibung der Anwendung, den benötigten Ressourcen, den Testschritten und eventuell bekannten Einschränkungen. Sobald der Pull-Request auf `master` angenommen ist, lesen die Appliances den neuen Online-Katalog innerhalb von 6 Stunden, ohne neue Version von pi-appliance.

## Nützliche Links

- [Quellcode des Marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [Spezifikation der Manifeste]({{ '/specification.html' | relative_url }}) (auf Französisch)
- [Fahrplan]({{ '/TODO.html' | relative_url }}) (auf Französisch)
- [pi-appliance-Repository](https://github.com/gmoulard/pi-appliance): Öffnen Sie einen Pull-Request aus Ihrem Fork, um Ihren Beitrag vorzuschlagen.
