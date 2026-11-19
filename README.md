# pi-appliance

**pi-appliance transforme un Raspberry Pi en appliance** : un boîtier personnel, à la maison, sur lequel on déploie des services en quelques clics. C'est un nœud de **Fog Computing** : vos services tournent chez vous et restent accessibles depuis internet.

On flashe une image système prête à l'emploi sur la carte SD, et le Pi fonctionne aussitôt, sans installation manuelle.

> **État du projet** : en conception. Le dépôt contient pour l'instant des prototypes (scripts et services Docker utilisés sur de vrais Pi). Le produit visé est décrit dans la [spécification](docs/specification.md), l'avancement dans le [TODO](docs/TODO.md).

## Comment ça marche

L'image pi-appliance empile trois couches :

1. **Raspberry Pi OS** (ex-Raspbian), l'image système standard ;
2. **Docker**, pour faire tourner chaque service dans ses conteneurs ;
3. **la couche produit pi-appliance** : une console web d'administration, une **marketplace** d'applications installables et le reverse proxy **Traefik**, qui publie les services sur internet avec des certificats HTTPS.

## Parcours type

1. J'achète un Raspberry Pi et je télécharge l'image pi-appliance.
2. Je la flashe sur une carte SD et je démarre le Pi branché sur ma box.
3. J'ouvre la console : `https://pi-appliance.local/pi-appliance` depuis mon réseau (ou `https://localhost/pi-appliance` sur le Pi lui-même).
4. Je me connecte avec mon identifiant et mon mot de passe.
5. Je donne mon nom de domaine et je crée mes utilisateurs.
6. Je coche dans la marketplace les services que je veux.
7. pi-appliance télécharge les images Docker, configure Traefik et ouvre chaque service, à la maison comme sur internet, à l'adresse `https://<service>.<mon domaine>`.

## Contenu du dépôt

| Chemin | Contenu |
|--------|---------|
| `cmd/pi-appliance/`, `internal/console/` | Console pi-appliance, en Go |
| [`docs/`](docs/) | [Spécification](docs/specification.md), [TODO](docs/TODO.md), [notes d'installation du prototype](docs/prototype.md), [mémo Git](docs/manGit.md) |
| `all-pi/` | Configuration commune des Pi : alias, crontab, tunnel autossh, sauvegarde |
| `linux-tool/` | Outils système : DNS dynamique OVH (`dynHost/`), sauvegarde, montage Swift |
| `docker-apache/`, `docker-nextcloud/`, `docker-samba/` | Services Docker, futures applications de la marketplace |
| `pi-motion/`, `pi-timelaps/` | Caméra : vidéosurveillance et timelapse |
| `RaspController/` | GPIO et capteur DHT |
| `.claude/skills/` | Sous-module : [skills Claude Code](https://github.com/gmoulard/claude_skills) partagés |

Après un clone, récupérer les skills :

```shell
git submodule update --init
```

## Console : construire et lancer

Avec Go 1.24 ou plus :

```shell
go test ./...
go run ./cmd/pi-appliance            # puis http://localhost:8080/pi-appliance/
GOOS=linux GOARCH=arm64 go build -o pi-appliance-arm64 ./cmd/pi-appliance   # binaire pour le Pi
```

## Site

Le site du projet est publié par GitHub Pages sur [www.pi-appliance.com](https://www.pi-appliance.com) (fichier `CNAME`).
