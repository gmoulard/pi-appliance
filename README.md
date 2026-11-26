# pi-appliance

<img src="docs/pi-appliance-logo.png" alt="Logo pi-appliance" width="200">

**pi-appliance transforme un Raspberry Pi en appliance** : un boîtier personnel, à la maison, sur lequel on déploie des services en quelques clics. C'est un nœud de **Fog Computing** : vos services tournent chez vous et restent accessibles depuis internet.

On flashe une image système prête à l'emploi sur la carte SD, et le Pi fonctionne aussitôt, sans installation manuelle.

> **État du projet** : expérimental. L'image système, publiée avec chaque [version](https://github.com/gmoulard/pi-appliance/releases/latest), est validée sur Raspberry Pi 3 B+ et Raspberry Pi 4 ; le Pi 5 et le Pi Zero 2 W restent à essayer. Le produit visé est décrit dans la [spécification](docs/specification.md), l'avancement dans le [TODO](docs/TODO.md). Pour essayer la première version de la console : [guide utilisateur](docs/guide-utilisateur.md).

> **Logo et charte** : la maison au π, en framboise, voir la [charte graphique](docs/charte-graphique.md).

## Trois parties, un seul nom

Tout s'appelle pi-appliance :

1. **l'image pi-appliance**, à flasher sur la carte SD : Raspberry Pi OS Lite 64 bits et le socle, publiée avec chaque [version](https://github.com/gmoulard/pi-appliance/releases/latest) ;
2. **le socle pi-appliance**, toujours installé sur le Pi : Docker, Traefik, les services et les outils du système, la **console** web (`/pi-appliance`), d'où tout se fait, et la **commande `pia`**, ligne de commande du Pi ; il se met à jour depuis la console ;
3. **l'application mobile**, [app.pi-appliance.com](https://app.pi-appliance.com) (adresse provisoire : [gmoulard.github.io/pi-appliance-pwa](https://gmoulard.github.io/pi-appliance-pwa/)) (dépôt [pi-appliance-pwa](https://github.com/gmoulard/pi-appliance-pwa)) : la documentation sans réseau et un lien direct vers ses Pi.

## Comment ça marche

L'image pi-appliance empile trois couches :

1. **Raspberry Pi OS** (ex-Raspbian), l'image système standard ;
2. **Docker**, pour faire tourner chaque service dans ses conteneurs ;
3. **la couche produit pi-appliance** : une console web d'administration, une **marketplace** d'applications installables et le reverse proxy **Traefik**, qui publie les services sur internet avec des certificats HTTPS.

## Parcours type

1. J'achète un Raspberry Pi (Pi 5, Pi 4, Pi 3 B+ ou Pi Zero 2 W) et je télécharge l'image pi-appliance.
2. Je la flashe sur une carte SD et je démarre le Pi.
3. S'il n'a pas de réseau, il crée un Wi-Fi « pi-appliance » : je m'y connecte avec mon téléphone et je choisis le Wi-Fi de la maison.
4. J'ouvre la console : `https://pi-appliance.local/pi-appliance` depuis la maison (ou `https://localhost/pi-appliance` sur le Pi lui-même).
5. Je me connecte avec mon identifiant et mon mot de passe.
6. Je choisis mon adresse, `<nom>.pi-appliance.com` (ou mon propre domaine), et je crée mes utilisateurs.
7. Je coche dans la marketplace les services que je veux : Nextcloud, site web, wiki, Etherpad, VPN…
8. pi-appliance télécharge les images Docker, configure Traefik et ouvre chaque service, à la maison comme sur internet, à l'adresse `https://<service>.<nom>.pi-appliance.com`, avec un seul identifiant pour tous les services.

Objectif : moins de 15 minutes entre la carte SD et un service prêt à l'emploi, à la maison et sur internet, sans aucune connaissance technique. Toutes vos données restent chez vous, en sécurité. Cette version est encore expérimentale.

## Contenu du dépôt

| Chemin | Contenu |
|--------|---------|
| `cmd/pi-appliance/`, `internal/` | Console pi-appliance, en Go : interface web, installation des services, réseau du Pi, tableau de bord, adresse pi-appliance.com tenue à jour, sauvegardes chiffrées sur disque USB, alertes par e-mail, statistiques anonymes, catalogue lu en ligne, administration du système par Cockpit, diagnostic du Pi sans écran, ligne de commande du Pi (`pia <commande>`, `internal/cli`) ; `cmd/pi-appliance-stats` : service du projet qui les reçoit ; `cmd/marketplace-json` : catalogue publié dans `docs/static/` |
| `marketplace/` | Catalogue des applications : un manifeste Docker Compose par application (Nextcloud, site web, DokuWiki, Etherpad, FileBrowser Quantum, n8n, WordPress, WordPress vers Markdown, Wikipedia2Book, PostgreSQL, VPN WireGuard, Portainer, Claude Code, Caméra et timelapse, sites statiques Hugo, MkDocs, Jekyll, Astro et Next.js) et `planned.yaml`, les applications prévues |
| `image/` | Construction de l'image système avec pi-gen (`image/build.sh`) et de l'archive de mise à jour publiée avec chaque version (`image/update-archive.sh`) |
| `system/` | Couche système installée sur le Pi : services systemd, Traefik, point d'accès Wi-Fi, secrets du premier démarrage (`system/install.sh`) |
| [`docs/`](docs/) | [Guide utilisateur](docs/guide-utilisateur.md), [installation du socle sans l'image](docs/installation.md), [essais avec plusieurs Pi](docs/essais-plusieurs-pi.md), [Spécification](docs/specification.md), [TODO](docs/TODO.md), [actions réalisées](docs/Todo-done.md), [notes d'installation du prototype](docs/prototype.md), [mémo Git](docs/manGit.md), [environnement (DNS, GitHub Pages, AWS CLI)](docs/environnement.md), [charte graphique](docs/charte-graphique.md), [logos](docs/logo/), site www.pi-appliance.com (`index.html`, `site.css`, `CNAME`, `_config.yml`, `_layouts/`, textes des 12 langues dans `_data/textes/`, pages traduites dans `en/`, `es/`… `fa/`) |
| `.claude/skills/` | Sous-module : [skills Claude Code](https://github.com/gmoulard/claude_skills) partagés |

Les anciens prototypes (configuration des Pi, tunnel autossh, DNS dynamique OVH, sauvegarde, services Docker, caméra, GPIO) ont été retirés du dépôt ; ils restent consultables dans l'historique git, [au commit `4ee13f9`](https://github.com/gmoulard/pi-appliance/tree/4ee13f9f0ef665e21388101aa5e06e20d3548c79).

Après un clone, récupérer les skills :

```shell
git submodule update --init
```

## Console : construire et lancer

Avec Go 1.24 ou plus :

```shell
go test ./...
go run ./cmd/pi-appliance -data ./tmp   # puis http://localhost:8080/pi-appliance/
GOOS=linux GOARCH=arm64 go build -o pi-appliance-arm64 ./cmd/pi-appliance   # binaire pour le Pi
```

Image système à flasher, avec Docker (environ une heure, voir `image/build.sh`) :

```shell
GOOS=linux GOARCH=arm64 go build -o pi-appliance-arm64 ./cmd/pi-appliance
image/build.sh pi-appliance-arm64   # puis image/deploy/pi-appliance-<date>.img.xz et .sha256
```

Options : `-addr` (adresse d'écoute, `:8080`), `-data` (dossier des services installés, `/var/lib/pi-appliance`), `-domain` (adresse de l'appliance, `pi-appliance.local`), `-catalog-url` (catalogue de la marketplace lu en ligne dans ce dépôt ; vide : catalogue intégré seulement). L'installation des services demande Docker et Docker Compose ; pour l'instant, elle n'est possible que depuis le Pi lui-même (`http://localhost:8080/pi-appliance/`).

## Pour qui

Pour tout le monde : aucune connaissance technique n'est nécessaire. Tout se fait depuis la console, dans un navigateur, sans ligne de commande.

## Licence

Version : [`VERSION`](VERSION), historique : [`CHANGELOG.md`](CHANGELOG.md).

Le code de pi-appliance est public, sous licence [CC BY-NC-ND 4.0 avec des permissions supplémentaires](LICENSE) : chacun peut l'utiliser, le diffuser tel quel et l'intégrer sans le modifier dans un projet non commercial, en citant le projet ; les versions modifiées et les projets dérivés ne sont pas permis, sauf pour proposer une contribution ([`CONTRIBUTING.md`](CONTRIBUTING.md)) ; un usage commercial demande l'accord de l'auteur (guillaume@moulard.org). Les versions jusqu'à 0.2.1 restent sous GNU AGPL-3.0.

Contact du projet : [admin@pi-appliance.com](mailto:admin@pi-appliance.com).

## Site

Le site du projet est publié par GitHub Pages sur [www.pi-appliance.com](https://www.pi-appliance.com) (source : dossier `docs/`, fichier `docs/CNAME`), avec la [charte graphique](docs/charte-graphique.md).
