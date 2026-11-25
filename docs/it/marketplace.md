---
layout: default
lang: it
ref: marketplace
traduit_le: 2026-11-25
---
# Marketplace di pi-appliance

La marketplace raccoglie le applicazioni che si possono installare e gestire dalla console di pi-appliance. Il catalogo è pubblicato su questo sito: le console già installate lo rileggono ogni 6 ore, e un’applicazione accettata vi compare senza una nuova versione di pi-appliance.

> **Versione sperimentale.** L’immagine è convalidata su Raspberry Pi 3 B+ e Raspberry Pi 4; a ogni modifica, una prova automatica installa ogni applicazione e ne apre la pagina, a casa e da internet, ma ognuna va ancora provata su un vero Pi. I servizi si aprono anche da internet, sotto l’indirizzo del Pi (vedi la [guida, in francese]({{ '/guide-utilisateur.html#services-sur-internet' | relative_url }})).

## La base di pi-appliance

La **base di pi-appliance** (Docker, Traefik, i servizi e gli strumenti di sistema, la console e il comando `pia`) è fornita con l’immagine: non è nel catalogo e non si disinstalla. Solo un amministratore, con l’account creato al primo avvio o un altro account amministratore, installa e gestisce le applicazioni qui sotto dalla console.

## Applicazioni già nel catalogo

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">Caricamento del catalogo…</p>
<p id="marketplace-tags-help" hidden>Tocchi una o più etichette per vedere solo le applicazioni che ne hanno almeno una; la tocchi di nuovo per toglierla.</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Filtrare per etichetta" hidden></div>
<p id="marketplace-tags-empty" hidden>Nessuna applicazione ha queste etichette.</p>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Applicazione</th><th>Descrizione</th><th>Categoria</th><th>Etichette</th><th>Memoria</th><th>Disco</th></tr></thead>
<tbody></tbody>
</table>
</div>

I nomi e le descrizioni delle applicazioni provengono dai loro manifesti e, per ora, sono scritti solo in francese.

Questo elenco è caricato dal catalogo JSON generato a partire dai manifesti di [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace). Le console leggono questo stesso file nel repository GitHub: un’applicazione accettata compare sui Pi senza una nuova versione di pi-appliance. Dopo ogni modifica di un manifesto o di `marketplace/planned.yaml`, rigeneri il file con `go run ./cmd/marketplace-json` dalla radice del repository; altrimenti i test (`go test ./...`) falliscono. Se se ne dimentica, un’azione GitHub lo rigenera su `master` dopo l’accettazione. Il nome del file contiene l’MD5 del suo contenuto; l’indice `docs/static/pi-appliance-catalog.json` indica la versione corrente.

La console gestisce più istanze, il loro avvio e arresto, gli aggiornamenti con ritorno alla versione precedente, e la conservazione o la cancellazione dei dati alla disinstallazione.

## Ciò che è previsto o allo studio

Queste applicazioni non sono ancora installabili dalla marketplace. L’elenco proviene dallo stesso catalogo JSON (`marketplace/planned.yaml`):

<ul id="marketplace-planned" hidden></ul>

**Integrato nella console, fuori catalogo**: [Cockpit](https://cockpit-project.org/), l’amministrazione del sistema del Pi (registri, programmi, rete, dischi e terminale nel browser, per utenti esperti). Non è un container ma un software di sistema: la Marketplace della console lo propone agli amministratori, solo a casa, e vi si entra con il proprio account pi-appliance, senza altre password (veda la [specifica]({{ '/specification.html' | relative_url }}) (in francese), sezione 6.4).

**Fuori dal catalogo, Docker Compose libero**: un amministratore può anche installare un’applicazione che non è nel catalogo, dal suo file Docker Compose, incollato o letto da un indirizzo web (una pagina GitHub, per esempio). Questo servizio non è né verificato né supportato dal progetto (vedi la [guida, in francese]({{ '/guide-utilisateur.html#docker-compose-libre' | relative_url }})).

Database: PostgreSQL è disponibile, con l’interfaccia web Adminer. MongoDB (solo Pi 5), Redis, MSSQL (nessuna immagine per il Pi) e SQLite (un semplice file) non sono proposti da soli: veda la [specifica]({{ '/specification.html' | relative_url }}) (in francese), sezione 6.3.

Anche i link pubblici di condivisione di Etherpad sono un miglioramento previsto. Consulti la [tabella di marcia]({{ '/TODO.html' | relative_url }}) (in francese) per seguirne l’avanzamento.

## Aggiungere un’applicazione: tutti possono contribuire

I contributi sono aperti. Può proporre un’applicazione libera utile a casa (unica eccezione per ora: n8n, con licenza *fair-code* «Sustainable Use License», scelta dal progetto e segnalata nella sua scheda), migliorare un manifesto esistente o segnalare un problema. Una proposta passa da una pull request; sarà riletta e provata prima di essere integrata.

### 1. Creare un manifesto Docker Compose

Faccia un fork del [repository pi-appliance](https://github.com/gmoulard/pi-appliance), poi crei `marketplace/<identificativo>/compose.yaml`. L’identificativo deve essere in minuscolo, iniziare con una lettera e contenere solo lettere, cifre e trattini.

Il file è un classico manifesto Docker Compose con una sezione `x-pi-appliance` che descrive l’applicazione. Esempio di struttura (sostituisca i valori e l’immagine con quelli della sua applicazione):

```yaml
x-pi-appliance:
  name: La mia applicazione
  description: Una descrizione chiara per gli utenti.
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
      label: Password dell’amministratore
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

### 2. Rispettare le regole del catalogo

- Scelga un’immagine Docker con una **versione precisa** (non `latest` o `stable`) e compatibile con `linux/arm64`, l’architettura dei Raspberry Pi previsti.
- Dichiari il servizio pubblicato in `service`, la sua porta interna in `port`, e stime realistiche in MB per `memory_mb` e `disk_mb`.
- Il campo `icon` è un tracciato SVG (`d`) su una griglia 24 × 24. Il campo `login` accetta `application` o `console`; `access` accetta `public`, `users`, `home` o `admins` (amministratori, a casa: riservato alle applicazioni che controllano tutto il Pi, come Portainer o Claude Code).
- Dichiari i parametri in maiuscolo in `parameters`. Per i segreti, usi `generate: true` invece di mettere una password nel manifesto o in un’immagine.
- Conservi i dati persistenti in volumi Docker con nome. Non metta nel repository né segreti, né dati personali, né configurazioni proprie di un utente. Un volume che non deve mai essere salvato nei backup (token di un account) porta l’etichetta `pi-appliance.backup: "false"` (`labels` del volume, come Claude Code).
- Indichi la licenza dell’applicazione e verifichi che la sua ridistribuzione sia compatibile con il progetto.
- Usi solo le chiavi documentate in `x-pi-appliance`: una chiave sconosciuta fa rifiutare il manifesto.
- Eviti le opzioni che danno il controllo di tutto il Pi (container privilegiato, accesso a Docker, cartella del Pi, rete dell’host, permessi aggiunti): una nuova applicazione che le ha non sarà installabile dal catalogo online, ma solo dopo una nuova versione di pi-appliance. Un’applicazione che ha accesso a Docker o che è un container privilegiato deve inoltre dichiarare `access: admins`, e la console ne fa confermare l’installazione.
- Un’applicazione prevista ma non ancora pronta va in `marketplace/planned.yaml` (nome, descrizione, icona, categoria e riga della tabella di marcia): il sito la mostra «In arrivo».

La definizione completa dei campi e le regole di convalida sono nella [specifica, sezione 6.5]({{ '/specification.html' | relative_url }}) (in francese).

### 3. Verificare e proporre

Dalla radice del repository, esegua:

```shell
go run ./cmd/marketplace-json
go test ./...
```

I test verificano in particolare che i manifesti siano validi, che le immagini abbiano una versione fissa e che le variabili di Compose siano dichiarate. Se possibile, provi anche l’installazione, l’arresto, il riavvio e la disinstallazione della sua applicazione con Docker su una macchina di prova.

Apra poi una pull request verso `master`, con una descrizione dell’applicazione, le risorse necessarie, i passi di prova e gli eventuali limiti noti. Una volta accettata la pull request su `master`, gli appliance leggono il nuovo catalogo online entro 6 ore, senza nuova versione di pi-appliance.

## Link utili

- [Codice sorgente della marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [Specifica dei manifesti]({{ '/specification.html' | relative_url }}) (in francese)
- [Tabella di marcia]({{ '/TODO.html' | relative_url }}) (in francese)
- [Repository pi-appliance](https://github.com/gmoulard/pi-appliance): apra una pull request dal suo fork per proporre il suo contributo.
