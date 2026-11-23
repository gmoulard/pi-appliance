# Actions réalisées

Actions de la [feuille de route](TODO.md) réalisées et validées (`[x]`), retirées du TODO le 2026-11-23 pour le garder lisible. Mêmes numéros et mêmes sections que le TODO ; les numéros entre parenthèses renvoient à la [spécification](specification.md). Une action réalisée dont un point reste ouvert reste dans le TODO.

## 0. Ménage du dépôt

- [x] **TODO-0.1**: Retirer les anciens prototypes (`all-pi/`, `linux-tool/`, `docker-*/`, `pi-motion/`, `pi-timelaps/`, `RaspController/`, `pypy/`) : ils restent dans l'historique git, au commit `4ee13f9` (spécification, section 12).
- [x] **TODO-0.2**: Changer les secrets qui étaient versionnés (mots de passe Wi-Fi de `all-pi/wpa_supplicant.conf`, clés de `all-pi/authorized_keys`, `linux-tool/ssh/`) : retirés du dépôt, ils restent dans l'historique git (8.4).
- [x] **TODO-0.3**: Retirer les fichiers générés (`*.log`, `old.ip`, `ipcheck.dat`) et ajouter un `.gitignore`.
- [x] **TODO-0.4**: Supprimer `tmp.uml` (exemple PlantUML sans rapport avec le projet).
- [x] **TODO-0.5**: Choisir une licence et ajouter `LICENSE` : AGPL-3.0 (1.2).
- [x] **TODO-0.6**: Ajouter l'en-tête de licence aux fichiers de code existants (Go et interface web).
- [x] **TODO-0.7**: Vérifier l'origine et la licence des fichiers repris d'ailleurs (`docker-apache/`, d'après gwendal-orinel ; `linux-tool/dynHost/ipcheck.py`) avant de les reprendre depuis l'historique git (vérifié le 2026-11-19 sur le commit `4ee13f9`) :
  - `docker-apache/` : **réutilisable sous AGPL-3.0**, avec l'en-tête de licence du projet et sans autre mention obligatoire. Les 126 commits sont de Guillaume Moulard (premier commit le 2018-09-19). Le `readme.md` dit seulement « Merci @gwendal-orinel ». Le dépôt [gwendal-orinel/docker](https://github.com/gwendal-orinel/docker) ne déclare aucune licence, donc tous droits réservés : rien n'est à y prendre. Son seul fichier proche, [`apache-php/Dockerfile`](https://github.com/gwendal-orinel/docker/blob/master/apache-php/Dockerfile), a été créé le 2018-09-28, après les nôtres. Il fait une seule ligne `apt-get install` sur `debian:latest`, sans rien d'original qui aurait été recopié. Garder le remerciement par courtoisie. Les fichiers `sites-available/*.conf` reprennent les commentaires du site par défaut d'Apache dans Debian (Apache-2.0, compatible avec l'AGPL-3.0). Ils citent des domaines personnels (`moulard.org`, `cyprin.eu`) et une adresse e-mail : ne pas les reprendre tels quels. De toute façon, la cible passe par Traefik et la marketplace (6, 10) : Apache ne serait qu'une application parmi d'autres.
  - `linux-tool/dynHost/ipcheck.py` : **à laisser, ne pas reprendre**. C'est ipcheck 0.67 de Kal (`kal@users.sourceforge.net`, [projet SourceForge](https://sourceforge.net/projects/ipcheck/), [site](https://ipcheck.sourceforge.io/)). Seul changement de notre part : l'hôte de mise à jour, remplacé par `www.ovh.com`. L'en-tête et la page SourceForge disent « GNU General Public License version 2 », sans « ou toute version ultérieure ». Or la GPL-2.0 seule est incompatible avec l'AGPL-3.0. Le code est aussi en Python 2 (`httplib`, `print` sans parenthèses), lit la page d'état de routeurs des années 2000 et n'est plus maintenu (site mis à jour pour la dernière fois en 2011). Le DNS dynamique (7, 10) sera écrit à neuf dans la console, en Go.
  - Au passage, `linux-tool/dynHost/dynhost.sh` suit le script DynHost publié par OVH, et son mot de passe a déjà été changé (ligne 2 ci-dessus). Ce script ne sert plus non plus, puisque le DNS de `pi-appliance.com` est chez Route 53 et non OVH ([environnement](environnement.md)).

## 1. Décisions

- [x] **TODO-1.1**: Public visé : grand public (1.1).
- [x] **TODO-1.2**: Objectif : Nextcloud sur internet en 15 minutes, sans aide (1.3).
- [x] **TODO-1.3**: Langage de la console : Go (10.1).
- [x] **TODO-1.4**: Matériel : Pi 5, 4, 3 B+, Zero 2 W ; avertissement pour les applications trop lourdes (4).
- [x] **TODO-1.5**: Premier réseau : point d'accès Wi-Fi de configuration (4).
- [x] **TODO-1.6**: Stockage : carte SD, disque USB proposé (4).
- [x] **TODO-1.7**: Catalogue : dépôt GitHub du projet, contributions par pull request (6.1).
- [x] **TODO-1.8**: Applications v1 : Nextcloud, site web, DokuWiki, Etherpad, WireGuard (6.3). Samba sorti de la v1 le 2026-11-19 : Nextcloud couvre les fichiers.
- [x] **TODO-1.9**: Désinstallation : toujours demander pour les données (6.2).
- [x] **TODO-1.10**: Adresse : `<nom>.pi-appliance.com`, alias vers un DNS dynamique gratuit ; domaine personnel en option (7.1).
- [x] **TODO-1.11**: Accès : direct (UPnP ou guide), relais du projet sinon (7.2, 7.3).
- [x] **TODO-1.12**: Connexion unique ; rôles administrateur, utilisateur, invité (8.1).
- [x] **TODO-1.13**: Accès choisi par service : public, connectés, maison (8.2).
- [x] **TODO-1.14**: Console sur internet seulement avec double authentification (8.3). Obligation retirée le 2026-11-22 à la demande de l'utilisateur : la double authentification reste facultative et conseillée.
- [x] **TODO-1.15**: Statistiques anonymes sur accord explicite (8.4).
- [x] **TODO-1.16**: Sauvegardes : disque USB, cloud, autre pi-appliance (9.1).
- [x] **TODO-1.17**: Mises à jour proposées, jamais automatiques (9.2).
- [x] **TODO-1.18**: Interface et site dans les 12 langues de Wikipedia2Book (5.3, 13).
- [x] **TODO-1.19**: Point d'accès Wi-Fi au premier démarrage même avec un câble Ethernet (4).
- [x] **TODO-1.20**: E-mail de l'administrateur obligatoire et vérifié (5.1).
- [x] **TODO-1.21**: DNS dynamique : DuckDNS ; adresse liée à l'appliance, libérée après 90 jours (7.1).
- [x] **TODO-1.22**: Relais gratuit avec limites, payant au-delà (7.3). Revu le 2026-11-21 : pas d'offre payante (TODO-1.45).
- [x] **TODO-1.23**: Fournisseur d'identité intégré à la console (8.1).
- [x] **TODO-1.24**: Récupération par e-mail, fichier `reset` sur la carte SD en dernier recours (8.5).
- [x] **TODO-1.25**: Alertes par e-mail (8.6).
- [x] **TODO-1.26**: Sauvegarde chaque nuit, 7 quotidiennes, 4 hebdomadaires, 6 mensuelles (9.1).
- [x] **TODO-1.27**: Correctifs de sécurité proposés comme le reste, rappelés par e-mail (9.2).
- [x] **TODO-1.28**: Services du projet hébergés sur un Pi du propriétaire du projet (10.2).
- [x] **TODO-1.29**: E-mails envoyés par le relais SMTP d'OVH ; sauvegarde et Pi de secours pour les services du projet ; minimum de données personnelles (10.2).
- [x] **TODO-1.30**: Relais gratuit : 5 Mbit/s, 20 Go par mois, sans garantie (7.3).
- [x] **TODO-1.31**: Pas de LDAP dans la v1 (il viendra avec Samba) ; invitation par e-mail ; mots de passe de 8 caractères ; double authentification facultative ; pas de limite de comptes (8.1).
- [x] **TODO-1.32**: Installation réservée aux administrateurs, plusieurs instances, retour arrière automatique (6.2).
- [x] **TODO-1.33**: Réglages avancés et Docker Compose libre (6.4).
- [x] **TODO-1.34**: Dépôt du site web, Etherpad (6.3).
- [x] **TODO-1.35**: Clé des sauvegardes protégée par le mot de passe administrateur, jumelage par code, restauration appliance/service/fichier, formatage ext4 avec accord (9.1).
- [x] **TODO-1.36**: Graphiques sur 30 jours ; une console par appliance (5.2) ; canaux stable et bêta (5.4).
- [x] **TODO-1.37**: Format des manifestes : Docker Compose + section `x-pi-appliance` (6.5).
- [x] **TODO-1.38**: Relais : nombre d'appliances relayées limité, liste d'attente (7.3).
- [x] **TODO-1.39**: Mot de passe administrateur de 12 caractères au moins (8.1).
- [x] **TODO-1.40**: Compose libre : options dangereuses signalées, pas bloquées (6.4).
- [x] **TODO-1.41**: Pas d'identifiant initial : compte administrateur créé dans le portail (5.1).
- [x] **TODO-1.42**: Point d'accès Wi-Fi ouvert, limité à 24 heures (4).
- [x] **TODO-1.43**: E-mail de l'administrateur vérifié dès que le service d'e-mails est joignable (5.1).
- [x] **TODO-1.44**: Certificat auto-signé avant Let's Encrypt, alerte expliquée (8.4).
- [x] **TODO-1.46**: Accès par défaut de Nextcloud : la spécification 8.2 dit « utilisateurs connectés », le manifeste dit `public`. Derrière l'authentification de la console, ses applications de synchronisation (ordinateur, téléphone) ne passent plus ; Nextcloud a déjà ses propres comptes. Trancher, puis aligner la spécification ou le manifeste. Tranché le 2026-11-20 : public, comme le manifeste ; spécification 8.2 alignée.

## 2. Étape 1 — Local


### Image système (4)

- [x] **TODO-2.2**: Docker et Docker Compose préinstallés. Vérifié sur un Pi 4 (2026-11-20) : Docker 29.10, Compose v5.8.
- [x] **TODO-2.5**: Construction automatique de l'image (GitHub Actions) et publication avec somme de contrôle. Versions v0.1.2 et v0.1.3 publiées par le workflow « Image » avec l'image, l'archive de mise à jour et leurs `.sha256`.
- [x] **TODO-2.6**: Première construction réelle de l'image par le workflow « Image » (à lancer à la main une fois le workflow sur `master`) : durée, taille, place sur le disque du runner.
  - Run [44578711083](https://github.com/gmoulard/pi-appliance/actions/runs/44578711083) du 2026-11-19, commit `48c047d` : **réussi** en 37 min 4 s, dont 34 min 53 s pour l'étape pi-gen (`real` 33 min 48 s) et 1 min 43 s pour « Place sur le disque ». Image `pi-appliance-2026-11-19-48c047d.img.xz` : 720 Mio compressée, 3 344 Mio décompressée (xz, taux 0,215) ; artefact `pi-appliance-image` de 755 Mo (image et `.sha256`), gardé jusqu'au 2026-12-03, somme de contrôle vérifiée après téléchargement. Place sur le disque après nettoyage : 145 G, 40 G utilisés, 105 G libres (28 %), largement assez.
  - Run [44573501694](https://github.com/gmoulard/pi-appliance/actions/runs/44573501694) du 2026-11-19 : **échec** après 19 min 35 s (pi-gen : 16 min 39 s), refusé par `01-check` (« docker.service n'est pas activé »), pas d'image donc pas de taille. Place sur le disque après nettoyage : 145 G, 36 G utilisés, 109 G libres (25 %). Cause : `[ -e ]` suivait les liens absolus de systemd dans le conteneur de pi-gen ; corrigé en `[ -L ]`, à relancer.

### Console (5)

- [x] **TODO-2.16**: Squelette Go : page d'accueil sous `/pi-appliance/`, `api/health`, tests, binaire `arm64` construit par GitHub Actions (10.1).
- [x] **TODO-2.17**: Service systemd et passage derrière Traefik, en HTTPS : code écrit (`system/etc/systemd/system/pi-appliance.service`, `system/etc/pi-appliance/traefik/`), Traefik et console essayés ensemble dans Docker ; à cocher après l'essai sur un vrai Pi. Vérifié sur un Pi 4 (2026-11-20) : service démarré au boot, console sur `127.0.0.1:8080` seulement, Traefik seul sur 80 et 443, `http` renvoyé vers `https`, adresse nue vers `/pi-appliance/`.
- [x] **TODO-2.18**: Authentification : compte administrateur créé au premier démarrage, connexion, sessions, limitation des tentatives (5.1, 8.1, 8.4).
- [x] **TODO-2.21**: Assistant de premier démarrage, première version : compte, réseau, premiers services (5.1).
- [x] **TODO-2.25**: Traduction dans les 12 langues, arabe et persan de droite à gauche : interface de la console traduite (`i18n.js`, `i18n/<code>.js`, test de complétude) ; traductions à faire relire par des locuteurs natifs. Restent en français : les messages du serveur Go (erreurs, état des services) et le catalogue des applications.

### Marketplace (6)

- [x] **TODO-2.27**: Les services de la marketplace sont décrits dans le fichier `docs/static/pi-appliance-<MD5Fichier>.json` ; chaque fois qu'il faut lister les services, se référer à ce document.
- [x] **TODO-2.28**: Format de manifeste et validation (6.5).
- [x] **TODO-2.30**: Installation, désinstallation (question sur les données), plusieurs instances (6.5).
- [x] **TODO-2.31**: Démarrer, arrêter, redémarrer un service ; état vert/orange/rouge.
- [x] **TODO-2.32**: Mise à jour d'un service avec retour arrière automatique.
- [x] **TODO-2.34**: Avertissement mémoire sur les petits Pi.
- [x] **TODO-2.35**: Jauges de mémoire et de disque : ce que le Pi a, ce qui est utilisé, ce que prendrait le service et ce qui resterait après l'installation (6.2) ; aussi dans « Mon Pi », avec le conseil de brancher un disque USB quand la place manque.
- [x] **TODO-2.36**: Ouvrir l'installation au réseau une fois l'authentification faite : réservée aux administrateurs connectés.
- [x] **TODO-2.37**: Application Nextcloud (manifeste `marketplace/nextcloud/`).
- [x] **TODO-2.38**: Applications site web Apache/PHP, DokuWiki, Etherpad (manifestes `marketplace/site-web/`, `dokuwiki/`, `etherpad/`).
- [x] **TODO-2.59**: Étiquettes (tags) sur les services de la marketplace pour les filtrer (par exemple « site web », « fichiers », « base de données », « administration ») : clé dans le manifeste (6.5), reprise dans le catalogue publié `docs/static/pi-appliance-<MD5>.json`, filtre dans l'écran de la marketplace et sur le site, libellés traduits dans les 12 langues. Réalisé (spécification 6.5) : clé `tags`, liste fermée de 8 étiquettes (`marketplace/tags.go`), filtre et étiquettes dans la console et sur la page marketplace du site.
  - [x] Catalogue publié lisible par les consoles 0.1.0 à 0.1.3 : la clé `tags` refusée par elles (« field tags not found », vu sur le Pi d'essai en 0.1.1) est retirée des fichiers Compose publiés et passe par le champ `tags` de chaque application ; règle de compatibilité écrite (spécification 6.5) et vérifiée par un test.

## 3. Étape 2 — Internet (7)

- [x] **TODO-3.13**: Écran de la console pour DuckDNS : saisie de l'identifiant et du jeton, état de la dernière mise à jour (l'API `api/duckdns` existe).
- [x] **TODO-3.3**: ~~FTP.~~ Abandonné le 2026-11-20 (FTP retiré).

## 5. Site www.pi-appliance.com (13)

- [x] **TODO-5.1**: Charte graphique (framboise, logo « La maison ») appliquée à la console et au site : `docs/charte-graphique.md`.
- [x] **TODO-5.2**: Page d'accueil du site : `docs/index.html`, `docs/site.css`.
- [x] **TODO-5.3**: Pages de documentation à la charte : `docs/_config.yml`, `docs/_layouts/default.html`.
- [x] **TODO-5.4**: GitHub Pages pour `www.pi-appliance.com` (source : branche `master`, dossier `/docs`), zone hébergée `pi-appliance.com` dans AWS Route 53.
- [x] **TODO-5.5**: Guide utilisateur de l'état du prototype, de ses fonctions et des essais restants : `docs/guide-utilisateur.md`.
- [x] **TODO-5.6**: Site mis à jour pour distinguer l'image construite de la version prête à télécharger ; essais sur un vrai Pi toujours à faire.
- [x] **TODO-5.7**: Page marketplace avec les applications présentes, les pistes à venir et les instructions de contribution.
- [x] **TODO-5.8**: Guide de téléchargement et de flashage de l'image expérimentale publiée comme artefact GitHub temporaire (expire le 2026-12-03). Depuis le 2026-11-21 : site et guide pointent vers la dernière version GitHub (`releases/latest`).
- [x] **TODO-5.12**: Ajouter à la TODO une option pour masquer et réafficher les actions réalisées.
- [x] **TODO-5.13**: Pied de page du site : dépôt, version et sa date, commit et sa date (section 13).
- [x] **TODO-5.14**: Gestion des versions : fichier `VERSION`, historique `CHANGELOG.md`, contrôle de l'étiquette dans le workflow « Image », paramètres du skill `release` dans `CLAUDE.md` (9.2).
- [x] **TODO-5.15**: Documenter dans le guide utilisateur la récupération par le fichier `reset` (8.5) : quand l'utiliser (message « identifiant ou mot de passe incorrect », identifiant et non adresse e-mail), créer un fichier vide nommé exactement `reset` sans extension `.txt` sur la partition de démarrage de la carte SD, redémarrer, choisir un nouveau mot de passe ; services et données gardés. Fait : section « Mot de passe administrateur oublié » du guide, et rappel « l'identifiant n'est pas l'adresse e-mail » dans l'aide de l'écran de connexion (12 langues).
- [x] **TODO-5.23**: États de la feuille de route (demande de l'utilisateur du 2026-11-22) : `[~]` en cours ou à vérifier, `[!]` bloqué, `[x]` réservé aux actions validées ; « Dépend de : » et critères d'acceptation sur chaque action ouverte ; pastilles, quatre filtres et nombre d'actions par état sur le site (`docs/todo-filter.js`), essayés dans Chromium.
