# CLAUDE.md

Instructions pour Claude Code (ou tout agent) travaillant sur ce dépôt.

## Le projet

pi-appliance transforme un Raspberry Pi en appliance de Fog Computing : image Raspberry Pi OS + Docker + couche produit (console `/pi-appliance`, marketplace, Traefik, exposition sur internet). Le produit visé est décrit dans [docs/specification.md](docs/specification.md), l'avancement dans [docs/TODO.md](docs/TODO.md) : les lire avant toute modification fonctionnelle et les mettre à jour dans le même changement. La spécification est marquée « cible » tant que le code n'existe pas : quand une fonction est réalisée, le dire dans la spécification et cocher le TODO.

Public visé : le grand public (spécification 1.1). Tout se fait depuis la console, sans ligne de commande ; les écrans parlent en langage courant, sans jargon technique.

Services hors du dépôt (DNS `pi-appliance.com` chez AWS Route 53 et non OVH, GitHub Pages, AWS CLI dans Docker) : [docs/environnement.md](docs/environnement.md).

Ne jamais versionner de secret (mot de passe Wi-Fi, clé SSH, jeton DNS, mot de passe de service).

## Paramètres des skills

`.claude/skills/` est un sous-module git, le dépôt partagé https://github.com/gmoulard/claude_skills : `git submodule update --init` après un clone. Modifier un skill = commit et push dans ce dépôt-là (demander avant de pousser), puis commit ici du nouveau pointeur du sous-module. Ne jamais y écrire de valeur propre à pi-appliance : elle va ci-dessous. Une valeur absente : la demander à l'utilisateur.

- **`code-style`** :
  - code : `cmd/`, `internal/`, `marketplace/` (Go, `gofmt`, interface web dans `internal/console/web/`) ; pièges : `grep -rn "NE PAS" .` ;
  - en-tête de licence AGPL-3.0, avec le commentaire du langage (`#`, `//`, `/* */`, `<!-- -->`), sur les fichiers de code (`.go`, `.sh`, `.py`, `.js`, `.css`, `.html`, `Dockerfile`, workflows) :

    ```
    This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
    Copyright (c) 2026 Guillaume Moulard.

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License as published
    by the Free Software Foundation, version 3.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <https://www.gnu.org/licenses/>.
    ```
- **`spec-sync`** :
  - référence : `docs/specification.md`, en français, qui fait foi ; pas de traduction pour l'instant ;
  - évolutions non implémentées : `docs/TODO.md`, section 15 « Questions ouvertes » et section 16 « Ordre de construction » ;
  - schéma Mermaid et choix techniques : section 10 ; arborescence : section 12 et README.
- **Tests** : `go vet ./... && go test ./...` ; binaire du Pi : `GOOS=linux GOARCH=arm64 go build ./cmd/pi-appliance`.
- **`release`** :
  - fichier de version : `VERSION` à la racine, `x.y.z` sans `v` (extraction : `cat VERSION`) ; étiquette `vX.Y.Z` ;
  - historique : `CHANGELOG.md` à la racine, en français, une ligne `- **X.Y.Z** — AAAA-MM-JJ — auteur — résumé` ajoutée en bas ;
  - auteur des commits de version : l'identité git locale, sans `--author` ;
  - branche principale : `master` ; tests : ceux de la ligne « Tests » ci-dessus ;
  - application déployée : `cmd/`, `internal/`, `marketplace/`, `system/`, `image/` ; `docs/` (site www.pi-appliance.com, catalogue publié `docs/static/`) et `.github/` n'appellent pas de version ;
  - contrôle de l'étiquette : première étape du workflow « Image » (`.github/workflows/image.yml`), qui refuse une étiquette différente de `VERSION` ; ce workflow construit l'image et crée lui-même la version GitHub : ne pas la créer à la main ; sans git (session cloud, dont le proxy refuse les étiquettes), lancer à la demande le workflow « Tag » (`.github/workflows/tag.yml`) sur `master` : il pose `vX.Y.Z` d'après `VERSION` et lance « Image » sur l'étiquette ;
  - angles morts : essais sur un vrai Pi 3B+ puis Pi 4 avec la même carte (2026-11-20, TODO-2.7 ; mise à jour v0.1.1 vers v0.1.3 réussie), ni Pi 5 ni Zero 2 W, ni point d'accès sur la durée ni portail captif sur téléphone ; l'image n'est construite que par le workflow « Image » (plusieurs dizaines de minutes) ; l'installation depuis la console (Mon Pi, Mises à jour) n'est vérifiable que sur un Pi déjà installé.
