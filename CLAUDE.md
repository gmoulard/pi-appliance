# CLAUDE.md

Instructions pour Claude Code (ou tout agent) travaillant sur ce dépôt.

## Le projet

pi-appliance transforme un Raspberry Pi en appliance de Fog Computing : image Raspberry Pi OS + Docker + couche produit (console `/pi-appliance`, marketplace, Traefik, exposition sur internet). Le produit visé est décrit dans [docs/specification.md](docs/specification.md), l'avancement dans [docs/TODO.md](docs/TODO.md) : les lire avant toute modification fonctionnelle et les mettre à jour dans le même changement. La spécification est marquée « cible » tant que le code n'existe pas : quand une fonction est réalisée, le dire dans la spécification et la passer en `[~]` dans le TODO ; ne la cocher `[x]` qu'une fois ses critères d'acceptation remplis (essai sur un vrai Pi compris), puis la déplacer dans [docs/Todo-done.md](docs/Todo-done.md), à la même section (une action dont un sous-point reste ouvert reste dans le TODO). Les demandes notées par l'utilisateur dans `docs/Todo-ithem-for-todo.md` sont à traiter puis à retirer de ce fichier. États `[ ]`, `[~]`, `[!]` (bloqué, raison dite), `[x]` ; une action ouverte porte « Dépend de : » et ses critères d'acceptation (légende en tête du TODO, filtres du site dans `docs/todo-filter.js`).

Public visé : le grand public (spécification 1.1). Tout se fait depuis la console, sans ligne de commande ; les écrans parlent en langage courant, sans jargon technique.

Services hors du dépôt (DNS `pi-appliance.com` chez AWS Route 53, en passage chez OVH, tranché le 2026-11-26, TODO-3.19, GitHub Pages, AWS CLI dans Docker) : [docs/environnement.md](docs/environnement.md).

Ne jamais versionner de secret (mot de passe Wi-Fi, clé SSH, jeton DNS, mot de passe de service).

## Paramètres des skills

`.claude/skills/` est un sous-module git, le dépôt partagé https://github.com/gmoulard/claude_skills : `git submodule update --init` après un clone. Modifier un skill = commit et push dans ce dépôt-là (demander avant de pousser), puis commit ici du nouveau pointeur du sous-module. Ne jamais y écrire de valeur propre à pi-appliance : elle va ci-dessous. Une valeur absente : la demander à l'utilisateur.

- **`code-style`** :
  - code : `cmd/`, `internal/`, `marketplace/` (Go, `gofmt`, interface web dans `internal/console/web/`) ; pièges : `grep -rn "NE PAS" .` ;
  - en-tête de licence CC BY-NC-ND 4.0 (`LICENSE`, permissions supplémentaires et contributions dans `CONTRIBUTING.md` ; le code jusqu'à la 0.2.1 reste aussi sous AGPL-3.0), avec le commentaire du langage (`#`, `//`, `/* */`, `<!-- -->`), sur les fichiers de code (`.go`, `.sh`, `.py`, `.js`, `.css`, `.html`, `Dockerfile`, workflows) :

    ```
    This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
    Copyright (c) 2026 Guillaume Moulard.

    Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
    4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
    of the LICENSE file at the root of the project: this file may be shared and
    integrated into another project unmodified, keeping this notice and a link to
    the project; modified versions may only be published to propose them to the
    official repository. Commercial use requires a separate licence from the
    author: guillaume@moulard.org.

    See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.
    ```
- **`spec-sync`** :
  - référence : `docs/specification.md`, en français, qui fait foi ; pas de traduction pour l'instant ;
  - évolutions non implémentées : `docs/TODO.md`, section 15 « Questions ouvertes » et section 16 « Ordre de construction » ;
  - schéma Mermaid et choix techniques : section 10 ; arborescence : section 12 et README.
- **`todo`** :
  - TODO : `docs/TODO.md` ; actions réalisées : `docs/Todo-done.md` ; boîte de réception : `docs/Todo-ithem-for-todo.md` ;
  - spécification : `docs/specification.md` ;
  - filtres : oui (site GitHub Pages, `docs/todo-filter.js`) ;
  - lots : tableau « Lots » en tête du TODO, réalisés par l'agent `todo-lot` (`.claude/agents/todo-lot.md`, lien vers le sous-module) ; branche de push : celle de la session, puis `main` (choix du 2026-11-23 ci-dessous ; aussi `master` tant que la branche par défaut sur GitHub n'est pas `main`, TODO-0.9).
- **Tests** : `go vet ./... && go test ./...` ; binaire du Pi : `GOOS=linux GOARCH=arm64 go build ./cmd/pi-appliance`.
- **`release`** :
  - fichier de version : `VERSION` à la racine, `x.y.z` sans `v` (extraction : `cat VERSION`) ; étiquette `vX.Y.Z` ;
  - historique : `CHANGELOG.md` à la racine, en français, une ligne `- **X.Y.Z** — AAAA-MM-JJ — auteur — résumé` ajoutée en bas ;
  - auteur des commits de version : l'identité git locale, sans `--author` ;
  - branche principale : `main` (au lieu de `master`, 2026-11-26, TODO-0.9 ; `master` reste poussée au même point jusqu'à ce que l'utilisateur la supprime) ; tests : ceux de la ligne « Tests » ci-dessus ;
  - choix de l'utilisateur (2026-11-23) : un travail terminé et testé est poussé sur `main` (et `master` tant qu'elle existe) sans redemander (branche de travail gardée au même point) ; **sortir une version** (`VERSION`, `CHANGELOG.md`, workflow « Tag ») se fait seulement après son accord ;
  - application déployée : `cmd/`, `internal/`, `system/`, `image/` ; `docs/` (site www.pi-appliance.com, catalogue publié `docs/static/`) et `.github/` n'appellent pas de version ; **un nouveau service de la marketplace (ou un manifeste changé) n'appelle pas de version** (choix de l'utilisateur du 2026-11-23) : les consoles installées relisent le catalogue publié en ligne (`docs/static/`, toutes les 6 heures) ; seul ce qui touche aussi `cmd/` ou `internal/` (nouvelle clé de manifeste, nouvelle option, code de la console) en appelle une, et le catalogue publié doit rester lisible par les consoles déjà installées (spécification 6.5) ;
  - contrôle de l'étiquette : première étape du workflow « Image » (`.github/workflows/image.yml`), qui refuse une étiquette différente de `VERSION` ; ce workflow construit l'image et crée lui-même la version GitHub : ne pas la créer à la main ; sans git (session cloud, dont le proxy refuse les étiquettes), lancer à la demande le workflow « Tag » (`.github/workflows/tag.yml`) sur `main` : il pose `vX.Y.Z` d'après `VERSION` et lance « Image » sur l'étiquette ; case « image » décochée : socle seul (archive de mise à jour, quelques minutes), la version renvoie à la dernière image ; **toujours décocher « image »** (`image: false`) : l'image n'est reconstruite que sur demande expresse de l'utilisateur (choix du 2026-11-21) ;
  - angles morts : essais sur un vrai Pi 3B+ puis Pi 4 avec la même carte (2026-11-20, TODO-2.7 ; mise à jour v0.1.1 vers v0.1.3 réussie), ni Pi 5 ni Zero 2 W, ni point d'accès sur la durée ni portail captif sur téléphone ; l'image n'est construite que par le workflow « Image » (plusieurs dizaines de minutes) ; l'installation depuis la console (Mon Pi, Mises à jour) n'est vérifiable que sur un Pi déjà installé.
