# CLAUDE.md

Instructions pour Claude Code (ou tout agent) travaillant sur ce dépôt.

## Le projet

pi-appliance transforme un Raspberry Pi en appliance de Fog Computing : image Raspberry Pi OS + Docker + couche produit (console `/pi-appliance`, marketplace, Traefik, exposition sur internet). Le produit visé est décrit dans [docs/specification.md](docs/specification.md), l'avancement dans [docs/TODO.md](docs/TODO.md) : les lire avant toute modification fonctionnelle et les mettre à jour dans le même changement. La spécification est marquée « cible » tant que le code n'existe pas : quand une fonction est réalisée, le dire dans la spécification et cocher le TODO.

Ne jamais versionner de secret (mot de passe Wi-Fi, clé SSH, jeton DNS, mot de passe de service).

## Paramètres des skills

`.claude/skills/` est un sous-module git, le dépôt partagé https://github.com/gmoulard/claude_skills : `git submodule update --init` après un clone. Modifier un skill = commit et push dans ce dépôt-là (demander avant de pousser), puis commit ici du nouveau pointeur du sous-module. Ne jamais y écrire de valeur propre à pi-appliance : elle va ci-dessous. Une valeur absente : la demander à l'utilisateur.

- **`code-style`** :
  - code : `all-pi/`, `linux-tool/`, `docker-*/`, `pi-motion/`, `pi-timelaps/`, `RaspController/`, `pypy/` ; pièges : `grep -rn "NE PAS" .` ;
  - en-tête de licence : à définir (pas encore de licence).
- **`spec-sync`** :
  - référence : `docs/specification.md`, en français, qui fait foi ; pas de traduction pour l'instant ;
  - évolutions non implémentées : `docs/TODO.md` et section 14 « Questions ouvertes » ;
  - schéma Mermaid : section 10 ; arborescence : section 12 et README.
- **`release`** : pas encore de numéro de version ; branche principale : `master`.
