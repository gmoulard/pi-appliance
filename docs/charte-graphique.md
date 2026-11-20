# Charte graphique pi-appliance

Une seule charte pour le site www.pi-appliance.com (page d'accueil `docs/index.html`, pages de documentation rendues par GitHub Pages avec `docs/_layouts/default.html`, styles communs `docs/site.css`) et la console `/pi-appliance` (`internal/console/web/`). Ambiance **ludique et colorée**, pour le grand public, pensée **d'abord pour le téléphone**. Maquette validée : [charte, site, console et premier démarrage](https://claude.ai/artifact/JYfQUgNQEz2sE3VYqNHngu).

## Logo

Piste 5, « La maison » : une maison avec un π à l'intérieur, en framboise. Fichiers dans [`docs/logo/framboise/`](logo/framboise/) :

| Fichier | Usage |
|---------|-------|
| `logo.svg` | Logo complet, fond clair |
| `logo-sombre.svg` | Logo complet sur fond aubergine ou sombre |
| `symbole.svg`, `symbole-sombre.svg` | Maison seule |
| `icone-app.svg` | Icône d'application : maison blanche sur carré framboise |
| `favicon.svg` | Icône simplifiée, 16 à 32 px |

Le nom « pi-appliance » est converti en tracés (dessiné avec Inter, licence OFL) : le logo ne dépend d'aucune police installée. « pi » en framboise et en gras, « -appliance » en aubergine.

**Console** : la console `/pi-appliance` affiche le même logo framboise « La maison » que le site, copié dans `internal/console/web/` :

| Fichier | Usage |
|---------|-------|
| `symbole.svg` | Maison en tête de la console, en 40 px, suivie du nom « **pi**-appliance » en texte (classe `wordmark`) |
| `favicon.svg` | Icône de l'onglet de la console |

Le logo photo (une main tenant des framboises, [`docs/logo/photo/`](logo/photo/)) n'est plus utilisé : jugé hors charte au premier essai sur un vrai Pi (2026-11-20). Le reste de la charte (couleurs, polices, formes) s'applique à la console sans changement.

## Couleurs

| Nom | Valeur | Usage |
|-----|--------|-------|
| Framboise | `#C71F5D` | Couleur de marque : boutons, bandeau d'état, logo |
| Framboise foncée | `#A8174E` | Liens, texte coloré |
| Ombre framboise | `#7E0F39` | Ombre pleine sous les boutons framboise |
| Rose clair | `#FCE4EC` | Boutons secondaires, fonds d'alerte |
| Aubergine | `#33141F` | Texte, logo, bandeaux sombres |
| Rose pâle | `#FFF5F7` | Fond des pages |
| Rose ligne | `#F0D3DC` | Ombres des cartes, séparateurs |

Une couleur vive par application, avec sa teinte pâle pour le fond de la carte :

| Teinte | Couleur | Fond | Exemple sur le site |
|--------|---------|------|---------------------|
| 0 | `#2F6FD6` bleu | `#E6EEFC` | Nextcloud |
| 1 | `#D9561C` orange | `#FDEBE1` | Partage de fichiers |
| 2 | `#1E8A52` vert | `#E2F4EA` | Site web |
| 3 | `#7445CC` violet | `#EEE7FB` | Wiki |
| 4 | `#B57F00` jaune | `#FFF4D1` | Etherpad |
| 5 | `#117F80` turquoise | `#DFF3F3` | VPN |

Dans la console, la teinte d'une application est tirée de son identifiant (`tint` dans `app.js`) : toujours la même pour une application, sans rien à déclarer dans son manifeste.

Pas de thème sombre.

## Typographie

Polices du système seulement, aucune police téléchargée (ni Google Fonts ni fichier de police dans le dépôt) :

- titres : `ui-rounded` (arrondie sur les appareils Apple), sinon `system-ui`, en gras 800 ;
- texte : `system-ui`, 17 px, interligne 1,5 ;
- adresses : `ui-monospace`.

## Formes et composants

- Coins arrondis : 14 px (champs) à 28 px (bandeaux) ; boutons en pilule.
- Bouton principal : framboise, texte blanc, ombre pleine `#7E0F39` de 4 px dessous, qui s'enfonce au clic.
- Bouton secondaire : rose clair, texte framboise foncée, sans ombre.
- Champs : bordure aubergine de 2 px, 48 px de haut.
- Cartes : blanches sur le rose pâle, ombre pleine rose ligne ; cartes d'application sur la teinte pâle de l'application, avec une tuile de couleur et l'icône de l'application en trait blanc (clé `icon` du manifeste dans la console).
- Zones tactiles de 44 px au moins ; contour de focus framboise de 3 px.
- États : vert « en marche », jaune « en cours », framboise « arrêté ».
