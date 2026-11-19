# CLAUDE.md

Instructions pour Claude Code (ou tout agent) travaillant sur ce dépôt.

## Le projet

pi-appliance transforme un Raspberry Pi en appliance de Fog Computing : image Raspberry Pi OS + Docker + couche produit (console `/pi-appliance`, marketplace, Traefik, exposition sur internet). Le produit visé est décrit dans [docs/specification.md](docs/specification.md), l'avancement dans [docs/TODO.md](docs/TODO.md) : les lire avant toute modification fonctionnelle et les mettre à jour dans le même changement. La spécification est marquée « cible » tant que le code n'existe pas : quand une fonction est réalisée, le dire dans la spécification et cocher le TODO.

Public visé : le grand public (spécification 1.1). Tout se fait depuis la console, sans ligne de commande ; les écrans parlent en langage courant, sans jargon technique.

Ne jamais versionner de secret (mot de passe Wi-Fi, clé SSH, jeton DNS, mot de passe de service).

## Paramètres des skills

`.claude/skills/` est un sous-module git, le dépôt partagé https://github.com/gmoulard/claude_skills : `git submodule update --init` après un clone. Modifier un skill = commit et push dans ce dépôt-là (demander avant de pousser), puis commit ici du nouveau pointeur du sous-module. Ne jamais y écrire de valeur propre à pi-appliance : elle va ci-dessous. Une valeur absente : la demander à l'utilisateur.

- **`code-style`** :
  - code : `cmd/`, `internal/` (Go, `gofmt`, interface web dans `internal/console/web/`), `all-pi/`, `linux-tool/`, `docker-*/`, `pi-motion/`, `pi-timelaps/`, `RaspController/`, `pypy/` ; pièges : `grep -rn "NE PAS" .` ;
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
- **`release`** : pas encore de numéro de version ; branche principale : `master`.
