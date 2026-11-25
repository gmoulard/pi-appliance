---
layout: default
lang: nl
ref: marketplace
traduit_le: 2026-11-25
---
# pi-appliance-marketplace

De marketplace brengt de toepassingen samen die u vanuit de pi-appliance-console kunt installeren en beheren. De catalogus wordt op deze site gepubliceerd: al geïnstalleerde consoles lezen hem elke 6 uur opnieuw, en een goedgekeurde toepassing verschijnt daar zonder nieuwe versie van pi-appliance.

> **Experimentele versie.** De image is gevalideerd op Raspberry Pi 3 B+ en Raspberry Pi 4; bij elke wijziging installeert een automatische test elke toepassing en opent hij haar pagina, thuis en via internet, maar elke toepassing moet nog op een echte Pi worden geprobeerd. De diensten openen ook via internet, onder het adres van de Pi (zie de [handleiding, in het Frans]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }})).

## De pi-appliance-basis

De **pi-appliance-basis** (Docker, Traefik, de diensten en hulpmiddelen van het systeem, de console en de opdracht `pia`) wordt met de image geleverd: hij staat niet in de catalogus en kan niet worden verwijderd. Alleen een beheerder, met het account dat bij de eerste start is aangemaakt of een ander beheerdersaccount, installeert en beheert de onderstaande toepassingen vanuit de console.

## Toepassingen die al in de catalogus staan

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">Catalogus laden…</p>
<p id="marketplace-tags-help" hidden>Tik op een of meer labels om alleen de toepassingen te zien die er minstens één van hebben; tik er opnieuw op om het weg te halen.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Filteren op label" hidden></div>
<p id="marketplace-tags-empty" hidden>Geen enkele toepassing heeft deze labels.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Toepassing</th><th>Beschrijving</th><th>Categorie</th><th>Labels</th><th>Geheugen</th><th>Schijf</th></tr></thead>
<tbody></tbody>
</table>
</div>

De namen en beschrijvingen van de toepassingen komen uit hun manifesten en zijn voorlopig alleen in het Frans geschreven.

Deze lijst wordt geladen uit de JSON-catalogus die wordt gegenereerd uit de manifesten in [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace). De consoles lezen hetzelfde bestand in de GitHub-repository: een geaccepteerde toepassing verschijnt op de Pi's zonder nieuwe versie van pi-appliance. Genereer het bestand na elke wijziging van een manifest of van `marketplace/planned.yaml` opnieuw met `go run ./cmd/marketplace-json` vanuit de hoofdmap van de repository; anders mislukken de tests (`go test ./...`). Vergeet u het, dan genereert een GitHub-actie het opnieuw op `master` na de acceptatie. De bestandsnaam bevat de MD5 van de inhoud; de index `docs/static/pi-appliance-catalog.json` geeft de huidige versie.

De console beheert meerdere instanties, het starten en stoppen ervan, updates met terugdraaien, en het bewaren of verwijderen van de gegevens bij het verwijderen.

## Wat gepland is of wordt onderzocht

Deze toepassingen kunnen nog niet vanuit de marketplace worden geïnstalleerd. De lijst komt uit dezelfde JSON-catalogus (`marketplace/planned.yaml`):

<ul id="marketplace-planned" hidden></ul>

**Ingebouwd in de console, buiten de catalogus**: [Cockpit](https://cockpit-project.org/), het beheer van het systeem van de Pi (logboeken, programma's, netwerk, schijven en terminal in de browser, voor ervaren gebruikers). Het is geen container maar systeemsoftware: de Marketplace van de console biedt het aan beheerders aan, alleen thuis, en u logt in met uw pi-appliance-account, zonder ander wachtwoord (zie de [specificatie]({{ '/specification.html' | relative_url }}) (in het Frans), sectie 6.4).

**Buiten de catalogus, vrije Docker Compose**: een beheerder kan ook een toepassing installeren die niet in de catalogus staat, vanuit haar Docker Compose-bestand, geplakt of gelezen van een webadres (bijvoorbeeld een GitHub-pagina). Zo'n dienst wordt door het project niet gecontroleerd en niet ondersteund (zie de [handleiding, in het Frans]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }})).

Databases: PostgreSQL is beschikbaar, met de webinterface Adminer. MongoDB (alleen Pi 5), Redis, MSSQL (geen image voor de Pi) en SQLite (een gewoon bestand) worden niet los aangeboden: zie de [specificatie]({{ '/specification.html' | relative_url }}) (in het Frans), sectie 6.3.

Openbare deellinks voor Etherpad zijn ook een geplande verbetering. Bekijk de [routekaart]({{ '/TODO.html' | relative_url }}) (in het Frans) om de voortgang te volgen.

## Een toepassing toevoegen: iedereen kan bijdragen

Bijdragen zijn welkom. U kunt een vrije toepassing voorstellen die thuis nuttig is (enige uitzondering voorlopig: n8n, onder de *fair-code*-licentie "Sustainable Use License", bewust gekozen door het project en vermeld op de kaart ervan), een bestaand manifest verbeteren of een probleem melden. Een voorstel gaat via een pull request; het wordt nagelezen en getest voordat het wordt opgenomen.

### 1. Een Docker Compose-manifest maken

Maak een fork van de [pi-appliance-repository](https://github.com/gmoulard/pi-appliance) en maak `marketplace/<id>/compose.yaml` aan. De id moet in kleine letters zijn, met een letter beginnen en mag alleen letters, cijfers en koppeltekens bevatten.

Het bestand is een gewoon Docker Compose-manifest met een sectie `x-pi-appliance` die de toepassing beschrijft. Voorbeeld van de opbouw (vervang de waarden en de image door die van uw toepassing):

```yaml
x-pi-appliance:
  name: Mijn toepassing
  description: Een duidelijke beschrijving voor de gebruikers.
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
      label: Beheerderswachtwoord
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

### 2. De regels van de catalogus volgen

- Kies een Docker-image met een **precieze versie** (niet `latest` of `stable`) die compatibel is met `linux/arm64`, de architectuur van de beoogde Raspberry Pi's.
- Geef de gepubliceerde dienst op in `service`, de interne poort in `port`, en realistische schattingen in MB voor `memory_mb` en `disk_mb`.
- Het veld `icon` is een SVG-pad (`d`) op een raster van 24 × 24. Het veld `login` accepteert `application` of `console`; `access` accepteert `public`, `users`, `home` of `admins` (beheerders, thuis: voorbehouden aan toepassingen die de hele Pi besturen, zoals Portainer of Claude Code).
- Geef de parameters in hoofdletters op in `parameters`. Gebruik voor geheimen `generate: true` in plaats van een wachtwoord in het manifest of in een image te zetten.
- Bewaar blijvende gegevens in benoemde Docker-volumes. Zet geen geheimen, persoonlijke gegevens of gebruikersspecifieke instellingen in de repository. Een volume dat nooit in een back-up mag komen (token van een account) krijgt het label `pi-appliance.backup: "false"` (`labels` van het volume, zoals bij Claude Code).
- Vermeld de licentie van de toepassing en controleer of de herverdeling ervan verenigbaar is met het project.
- Gebruik in `x-pi-appliance` alleen de gedocumenteerde sleutels: een onbekende sleutel leidt tot weigering van het manifest.
- Vermijd opties die de controle over de hele Pi geven (geprivilegieerde container, toegang tot Docker, een map van de Pi, het netwerk van de host, extra rechten): een nieuwe toepassing met zulke opties kan niet vanuit de online catalogus worden geïnstalleerd, alleen na een nieuwe versie van pi-appliance. Een toepassing met toegang tot Docker of die een geprivilegieerde container is, moet bovendien `access: admins` opgeven, en de console laat de installatie bevestigen.
- Een geplande toepassing die nog niet klaar is, komt in `marketplace/planned.yaml` (naam, beschrijving, pictogram, categorie en regel van de routekaart): de site toont haar als "Binnenkort".

De volledige beschrijving van de velden en de validatieregels staan in de [specificatie, sectie 6.5]({{ '/specification.html' | relative_url }}) (in het Frans).

### 3. Controleren en voorstellen

Voer vanuit de hoofdmap van de repository uit:

```shell
go run ./cmd/marketplace-json
go test ./...
```

De tests controleren met name of de manifesten geldig zijn, of de images een vaste versie hebben en of de Compose-variabelen zijn opgegeven. Probeer zo mogelijk ook het installeren, stoppen, herstarten en verwijderen van uw toepassing met Docker op een testmachine.

Open daarna een pull request naar `master`, met een beschrijving van de toepassing, de benodigde middelen, de teststappen en eventuele bekende beperkingen. Zodra de pull request op `master` is geaccepteerd, lezen de appliances de nieuwe online catalogus binnen 6 uur, zonder nieuwe versie van pi-appliance.

## Nuttige links

- [Broncode van de marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [Specificatie van de manifesten]({{ '/specification.html' | relative_url }}) (in het Frans)
- [Routekaart]({{ '/TODO.html' | relative_url }}) (in het Frans)
- [pi-appliance-repository](https://github.com/gmoulard/pi-appliance): open een pull request vanuit uw fork om uw bijdrage voor te stellen.
