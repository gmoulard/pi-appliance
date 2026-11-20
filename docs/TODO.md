# TODO

Feuille de route de pi-appliance, dans l'ordre de construction de la section 16 de la [spécification](specification.md). Les numéros entre parenthèses renvoient à ses sections.

<div id="todo-filters" class="todo-filters" role="group" aria-label="Filtrer les actions">
  <label><input id="todo-show-completed" type="checkbox"> Actions réalisées</label>
  <label><input id="todo-show-pending" type="checkbox" checked> Actions à réaliser</label>
</div>

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
- [x] **TODO-1.14**: Console sur internet seulement avec double authentification (8.3).
- [x] **TODO-1.15**: Statistiques anonymes sur accord explicite (8.4).
- [x] **TODO-1.16**: Sauvegardes : disque USB, cloud, autre pi-appliance (9.1).
- [x] **TODO-1.17**: Mises à jour proposées, jamais automatiques (9.2).
- [x] **TODO-1.18**: Interface et site dans les 12 langues de Wikipedia2Book (5.3, 13).
- [x] **TODO-1.19**: Point d'accès Wi-Fi au premier démarrage même avec un câble Ethernet (4).
- [x] **TODO-1.20**: E-mail de l'administrateur obligatoire et vérifié (5.1).
- [x] **TODO-1.21**: DNS dynamique : DuckDNS ; adresse liée à l'appliance, libérée après 90 jours (7.1).
- [x] **TODO-1.22**: Relais gratuit avec limites, payant au-delà (7.3).
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
- [ ] **TODO-1.45**: Questions ouvertes de la section 15 : prix et paiement du relais, nombre maximal d'appliances relayées (à l'étape Internet).

## 2. Étape 1 — Local

### Image système (4)

- [ ] **TODO-2.1**: Construction de l'image à partir de Raspberry Pi OS Lite 64 bits (`pi-gen`), une seule image pour les quatre modèles.
- [ ] **TODO-2.2**: Docker et Docker Compose préinstallés.
- [ ] **TODO-2.3**: Premier démarrage : nom d'hôte, mDNS `pi-appliance.local`, secrets propres au boîtier, certificat local.
- [ ] **TODO-2.4**: Point d'accès Wi-Fi « pi-appliance » ouvert, limité à 24 heures, et portail de configuration avec création du compte administrateur.
- [ ] **TODO-2.5**: Construction automatique de l'image (GitHub Actions) et publication avec somme de contrôle.

Code écrit dans `image/` et `system/` pour les cinq points ci-dessus (spécification, section 4), vérifié sans Pi seulement (`shellcheck`, `yamllint`, Traefik essayé dans Docker, pi-gen préparé sans construire). À cocher après :

- [x] **TODO-2.6**: Première construction réelle de l'image par le workflow « Image » (à lancer à la main une fois le workflow sur `master`) : durée, taille, place sur le disque du runner.
  - Run [44578711083](https://github.com/gmoulard/pi-appliance/actions/runs/44578711083) du 2026-11-19, commit `48c047d` : **réussi** en 37 min 4 s, dont 34 min 53 s pour l'étape pi-gen (`real` 33 min 48 s) et 1 min 43 s pour « Place sur le disque ». Image `pi-appliance-2026-11-19-48c047d.img.xz` : 720 Mio compressée, 3 344 Mio décompressée (xz, taux 0,215) ; artefact `pi-appliance-image` de 755 Mo (image et `.sha256`), gardé jusqu'au 2026-12-03, somme de contrôle vérifiée après téléchargement. Place sur le disque après nettoyage : 145 G, 40 G utilisés, 105 G libres (28 %), largement assez.
  - Run [44573501694](https://github.com/gmoulard/pi-appliance/actions/runs/44573501694) du 2026-11-19 : **échec** après 19 min 35 s (pi-gen : 16 min 39 s), refusé par `01-check` (« docker.service n'est pas activé »), pas d'image donc pas de taille. Place sur le disque après nettoyage : 145 G, 36 G utilisés, 109 G libres (25 %). Cause : `[ -e ]` suivait les liens absolus de systemd dans le conteneur de pi-gen ; corrigé en `[ -L ]`, à relancer.
- [ ] **TODO-2.7**: Essai sur un vrai Pi (Pi 5, 4, 3 B+, Zero 2 W) : partition étendue, `pi-appliance.local`, secrets différents sur deux cartes flashées avec la même image, Traefik et console démarrés sans internet, point d'accès ouvert même avec un câble, fermé après le choix du réseau, après 24 heures, rouvert après un redémarrage et après 10 minutes sans réseau, portail captif sur iPhone et Android, mémoire libre sur le Zero 2 W.
- [ ] **TODO-2.48**: Valider la cohabitation de plusieurs Raspberry Pi pi-appliance et du service de contrôle controlTower dans la même maison : noms réseau uniques, accès à chaque console et service, absence de conflits réseau et procédure de configuration.
  - [x] Noms réseau uniques : `pi-appliance-XXXX` au premier démarrage (adresse matérielle), nom d'hôte, `<nom>.local` et Wi-Fi de configuration ; nom modifiable dans la console (`controltower` pour le Pi du concepteur) ; certificat refait (`name.sh`, `network.sh name`, `POST api/network/name`, `system/tests/name-test.sh`). À vérifier sur un vrai Pi, avec deux Pi allumés.
  - [x] Wi-Fi de configuration sans accès à internet ni à la maison (pare-feu `ap-firewall.nft`, `system/tests/ap-firewall-test.sh`) : cause probable de l'incident du 2026-11-19.
  - [X] **TODO-2.8**: test PI4B OK
    - [ ] **TODO-2.9**: ajouté un bouton pour pouvoir voir le password du wiki
    - [ ] **TODO-2.10**: ne pas proposer l'intallation directe des service.
    - [ ] **TODO-2.11**: quand tout est bien setup, bouton Test WIFI.
    - [ ] **TODO-2.12**: Quand le teste est OK alors bouton Rebbot. avec un message de confirmation qui indique a quelle URL on va pouvoir trouver la machine sur le reseau local. Info sur la suite de la procedure avec un timer a 10s
    - [x] **TODO-2.13**: Dans la DOC guide-utilisateur apres 6. explique ce qui se passer.
- [x] **TODO-2.14**: API réseau de la console (`internal/network`, branchée sur `/usr/local/lib/pi-appliance/network.sh`, option `-network-script`), réservée à l'administrateur connecté ; essayée contre un faux `network.sh`, reste l'essai sur un vrai Pi (ligne ci-dessus) :
  - `GET api/network` → `network.sh status` : `{"accessPoint": {"open", "ssid", "reason", "closesInSeconds"}, "configured": "" | "wifi" | "wired", "wired": {"connected"}, "wifi": {"connected", "ssid"}, "addresses": ["192.168.1.20"], "lastError": ""}` ;
  - `GET api/network/wifi` → `network.sh wifi-list` : `{"scannedAt", "networks": [{"ssid", "signal" (0 à 100), "security": "open" | "wpa" | "wpa3" | "enterprise"}]}` ;
  - `POST api/network/wifi/scan` → `network.sh wifi-scan` : même réponse ; prévenir que le point d'accès se ferme quelques secondes (le téléphone se reconnecte) ;
  - `POST api/network/wifi` `{"ssid", "password", "hidden"}` → `network.sh wifi-join` (corps passé tel quel sur l'entrée standard) : répondre `202` **avant** de lancer la commande (quelques secondes plus tard), car le point d'accès se ferme et le téléphone perd la réponse ; dire à l'utilisateur de rejoindre le Wi-Fi de la maison puis d'ouvrir `https://pi-appliance.local/pi-appliance`. En cas d'échec, le point d'accès se rouvre et `lastError` vaut `wrong-password`, `not-found`, `timeout` ou `failed` ;
  - `POST api/network/wired` → `network.sh wired` : `{"ok": true}` ou `{"ok": false, "error": "no-cable", "message"}` ;
  - écrans du portail : liste des Wi-Fi, mot de passe, choix du câble, temps restant avant la fermeture du point d'accès, explication de l'alerte du certificat.
- [x] **TODO-2.15**: Choix du pays du Wi-Fi dans le portail (l'image part sur la France ; le point d'accès reste sur le canal 6, permis partout) : `network.sh country`, `POST api/network/country` ; à vérifier sur un vrai Pi.

### Console (5)

- [x] **TODO-2.16**: Squelette Go : page d'accueil sous `/pi-appliance/`, `api/health`, tests, binaire `arm64` construit par GitHub Actions (10.1).
- [ ] **TODO-2.17**: Service systemd et passage derrière Traefik, en HTTPS : code écrit (`system/etc/systemd/system/pi-appliance.service`, `system/etc/pi-appliance/traefik/`), Traefik et console essayés ensemble dans Docker ; à cocher après l'essai sur un vrai Pi.
- [x] **TODO-2.18**: Authentification : compte administrateur créé au premier démarrage, connexion, sessions, limitation des tentatives (5.1, 8.1, 8.4).
- [ ] **TODO-2.19**: Vérification de l'e-mail par un code, dès que le service d'e-mails est joignable (5.1, étape 2).
- [x] **TODO-2.20**: Fichier `reset` sur la carte SD (8.5).
- [x] **TODO-2.21**: Assistant de premier démarrage, première version : compte, réseau, premiers services (5.1).
- [ ] **TODO-2.22**: Étapes suivantes de l'assistant, avec leurs fonctions : langue, adresse sur internet, test d'accès, utilisateurs, statistiques (5.1).
- [x] **TODO-2.23**: Tableau de bord : charge, température, mémoire, disque, état des services, graphiques sur 24 heures et 30 jours, historique horaire écrit une fois par heure (`internal/metrics`) ; à vérifier sur un vrai Pi (capteur de température, interfaces réseau, mémoire prise sur le Zero 2 W). Adresse publique, certificats et mode d'accès viendront avec l'étape Internet.
- [ ] **TODO-2.24**: Canaux de version stable et bêta.
- [x] **TODO-2.25**: Traduction dans les 12 langues, arabe et persan de droite à gauche : interface de la console traduite (`i18n.js`, `i18n/<code>.js`, test de complétude) ; traductions à faire relire par des locuteurs natifs. Restent en français : les messages du serveur Go (erreurs, état des services) et le catalogue des applications.
- [x] **TODO-2.26**: Disque USB : détection, formatage en ext4 avec accord, déplacement des données de Docker (volumes et images), alerte de remplissage de la carte SD dans la console (`storage.sh`, `internal/storage`) ; à vérifier sur un vrai Pi (copie, redémarrage de Docker et de Traefik, disque débranché au démarrage). Reste : revenir sur la carte SD, changer de disque, alerte par e-mail (avec le service d'e-mails).

### Marketplace (6)

- [x] **TODO-2.27**: Les services de la marketplace sont décrits dans le fichier `docs/static/pi-appliance-<MD5Fichier>.json` ; chaque fois qu'il faut lister les services, se référer à ce document.
- [x] **TODO-2.28**: Format de manifeste et validation (6.5).
- [ ] **TODO-2.29**: Dépôt GitHub du catalogue et règles de contribution ; lecture en ligne (aujourd'hui intégré au binaire).
- [x] **TODO-2.30**: Installation, désinstallation (question sur les données), plusieurs instances (6.5).
- [x] **TODO-2.31**: Démarrer, arrêter, redémarrer un service ; état vert/orange/rouge.
- [x] **TODO-2.32**: Mise à jour d'un service avec retour arrière automatique.
- [x] **TODO-2.33**: Copie des données d'un service avant sa mise à jour, pour un retour arrière complet : volumes copiés dans des volumes de secours, service arrêté, remis en cas d'échec, place vérifiée avant (`internal/services/data.go`) ; à vérifier sur un vrai Pi. Les sauvegardes (9.1) restent à faire.
- [x] **TODO-2.34**: Avertissement mémoire sur les petits Pi.
- [x] **TODO-2.35**: Jauges de mémoire et de disque : ce que le Pi a, ce qui est utilisé, ce que prendrait le service et ce qui resterait après l'installation (6.2) ; aussi dans « Mon Pi », avec le conseil de brancher un disque USB quand la place manque.
- [x] **TODO-2.36**: Ouvrir l'installation au réseau une fois l'authentification faite : réservée aux administrateurs connectés.
- [x] **TODO-2.37**: Application Nextcloud (manifeste `marketplace/nextcloud/`).
- [x] **TODO-2.38**: Applications site web Apache/PHP, DokuWiki, Etherpad (manifestes `marketplace/site-web/`, `dokuwiki/`, `etherpad/`).
- [ ] **TODO-2.39**: Site web : dépôt des fichiers par Nextcloud, ZIP ou FTP.
  - [x] **TODO-2.40**: 6.5.1 ZIP : endpoint administrateur, vérification du ZIP, extraction sûre, remplacement du contenu du site.
  - [x] **TODO-2.41**: 6.5.2 Nextcloud : dossier partagé monté dans Nextcloud et dans chaque instance du site web.
  - [x] **TODO-2.42**: 6.5.3 FTP : service FTP installé avec un compte `pi`, un port configurable et un mot de passe généré.
  - [ ] **TODO-2.43**: 6.5.4 Valider les trois méthodes sur Raspberry Pi réel et documenter les limites.
- [ ] **TODO-2.44**: Etherpad : liens de partage pour une personne sans compte.
- [x] **TODO-2.45**: Permettre à l'administrateur de déployer et gérer ses propres services Docker Compose depuis la console, à partir de fichiers `compose.yaml`, sans les ajouter au catalogue ; inclure les réglages avancés et signaler les options dangereuses (6.4) : écran « Réglages avancés » sur chaque service (variables, ports, volumes), carte « Docker Compose libre » dans la marketplace, options dangereuses signalées et confirmées, pas bloquées (`internal/services/advanced.go`, `custom.go`) ; à vérifier sur un vrai Pi. Règles d'accès et sauvegardes viendront pour tous les services.
- [ ] **TODO-2.46**: Générateurs de sites statiques (SSG) MkDocs, Astro, Hugo, Jekyll et Next.js.
- [x] **TODO-2.47**: MSSQL, PostgreSQL, SQLite, MongoDB, Redis : PostgreSQL retenu avec l'interface web Adminer (manifeste `marketplace/postgresql/`, accès maison seulement ; à vérifier sur un vrai Pi) ; MSSQL écarté (pas d'image `arm64`), SQLite (pas un serveur), MongoDB (Pi 5 seulement à partir de la version 5), Redis (cache sans interface web, à mettre dans le manifeste d'une application) : raisons en 6.3. Une base ouverte sur le réseau de la maison sans interface web attend le format de manifeste pour un service qui n'est pas un site web (comme Samba, TODO-4.8).

## 3. Étape 2 — Internet (7)

- [x] **TODO-3.1**: Traefik : point d'entrée unique, routage par sous-domaine et sélection de Let's Encrypt pour les domaines publics ; e-mail administrateur écrit localement et chargé au prochain démarrage de Traefik ; certificat local conservé pour les noms locaux et les adresses IP.
- [x] **TODO-3.2**: DuckDNS tenu à jour par l'appliance (`internal/ddns`, API `api/duckdns` réservée à l'administrateur) ; à vérifier sur un vrai Pi.
- [x] **TODO-3.13**: Écran de la console pour DuckDNS : saisie de l'identifiant et du jeton, état de la dernière mise à jour (l'API `api/duckdns` existe).
- [ ] **TODO-3.3**: FTP.
- [ ] **TODO-3.4**: Shell Linux.
- [ ] **TODO-3.5**: Service d'adresses du projet : réservation liée à la clé du Pi, alias vers DuckDNS, signes de vie, libération après 90 jours, noms interdits (7.1, 10.2).
- [ ] **TODO-3.6**: Service d'envoi d'e-mails du projet : codes de vérification, récupération, alertes (8.5, 8.6, 10.2).
- [ ] **TODO-3.7**: Domaine personnel : guide OVH, avec étapes DNS chez OVH séparées des réglages dans la console ([guide utilisateur](guide-utilisateur.md#domaine-personnel-chez-ovh)). Tâche ouverte : le guide signale l'indisponibilité du parcours ; avant de le rendre opérationnel, il faut TODO-3.2 et TODO-3.5 pour fournir l'adresse cible, puis la prise en charge du domaine et de ses certificats dans la console.
- [ ] **TODO-3.8**: Accès direct : UPnP, guide de la box, test depuis internet ; y compris le port UDP 51820 du VPN WireGuard quand il est installé (TODO-4.6).
- [ ] **TODO-3.9**: Relais : serveur, tunnel sortant (à partir de l'ancien `all-pi/autossh.sh`, historique git), bascule automatique, limites de l'offre gratuite, nombre maximal d'appliances et liste d'attente, offre payante.
- [ ] **TODO-3.10**: Accès par service : public, connectés, maison (8.2).
- [ ] **TODO-3.11**: Console fermée sur internet par défaut (8.3) : Traefik limite désormais `/pi-appliance` aux plages privées IPv4, à la boucle locale et aux plages locales IPv6, avec une priorité basse ; le test Traefik vérifie le refus d'une source hors liste autorisée et laisse passer les routeurs des services. Reste à définir les plages réelles du VPN et du tunnel relais : si le pair du relais a une adresse privée, `ClientIP` l'autorise aussi. L'accès depuis un LAN IPv6 à adresse globale est également refusé. Ne pas cocher avant d'avoir défini ces réseaux et validé leur séparation.
- [ ] **TODO-3.12**: Console ouverte sur internet avec double authentification (8.3).

## 4. Étape 3 — Le reste

- [ ] **TODO-4.1**: Fournisseur d'identité intégré à la console : OpenID Connect, *forward auth* Traefik, double authentification, invitations par e-mail ; rôles et invités (8.1, 8.3). Première étape faite (8.1, `internal/auth`) : comptes utilisateur et invité (date de fin), invitations par lien à transmettre soi-même (interface `auth.Mailer` prête), double authentification TOTP facultative ou exigée par l'administrateur, codes de secours, *forward auth* (`/pi-appliance/api/auth/forward`, middleware Traefik `pi-appliance-auth@file`). Reste : OpenID Connect (fournisseur pour les services, avec une bibliothèque éprouvée), services autorisés par compte, envoi des invitations par e-mail (attend TODO-3.6), relecture de sécurité (11). Le branchement du middleware sur les services « connectés » est TODO-3.10.
- [ ] **TODO-4.2**: Alertes par e-mail, désactivables par type (8.6).
- [x] **TODO-4.3**: Sauvegardes, première étape (9.1, `internal/backup`, `backup-disk.sh`) : configuration et volumes des services (service arrêté le temps de la copie), chaque nuit et à la demande, rotation 7/4/6, disque USB autre que celui des données (formatage ext4 avec accord), chiffrement AES-256-GCM avec une clé protégée par le mot de passe administrateur, restauration d'un service à une date choisie (copie de l'état actuel avant), API et écran « Sauvegardes » ; à vérifier sur un vrai Pi. La suite : TODO-4.10 à 4.14.
- [ ] **TODO-4.4**: Services du projet : relais SMTP OVH (SPF, DKIM, DMARC), sauvegarde hors de la maison, Pi de secours ; installer le service des statistiques (`cmd/pi-appliance-stats`) sous `stats.pi-appliance.com`, journal des accès du reverse proxy coupé (10.2).
- [x] **TODO-4.5**: Mises à jour proposées dans la console (9.2) : correctifs du système listés (sécurité en évidence) et installés à la demande (`updates.sh`, `internal/updates`, API `api/updates`), nouvelle version de pi-appliance signalée sans être installée, résumé dans « Mon Pi » ; à vérifier sur un vrai Pi. Rappel par e-mail des correctifs de sécurité : attend le service d'e-mails (TODO-3.6, 8.6).
- [x] **TODO-4.6**: VPN WireGuard avec QR code (6.3) : manifeste `marketplace/wireguard/` (`linuxserver/wireguard`, port UDP 51820 du Pi par la nouvelle clé `host_ports`, service sans page web), clés tirées sur le Pi, écran « Appareils » avec QR code montré une fois, `AllowedIPs` limités au réseau de la maison ; à vérifier sur un vrai Pi, avec un téléphone hors de la maison. La redirection du port UDP 51820 sur la box relève de TODO-3.8.
- [x] **TODO-4.7**: Statistiques anonymes et leur service de réception (8.4, 10.2) : réglage dans la console avec l'envoi exact sous les yeux, envoi au plus quotidien (`internal/stats`), service de réception sans adresse IP (`cmd/pi-appliance-stats`). Restent l'étape de l'assistant (TODO-2.22) et la mise en ligne du service sur le Pi du projet (TODO-4.4).
- [ ] **TODO-4.8**: Puis : Samba, lecteur réseau à la maison (dossiers commun et personnels), avec le format de manifeste pour un service qui n'est pas un site web (`host_ports`, venu avec TODO-4.6 ; reste l'accès « maison seulement » sans Traefik), l'annuaire LDAP de la console et le stockage externe de Nextcloud (6.3).
- [ ] **TODO-4.9**: Puis : applications caméra (Motion, timelapse).
- [ ] **TODO-4.10**: Sauvegardes vers un stockage en ligne S3 ou OVH Swift (9.1) : mêmes archives chiffrées (`internal/backup`), une destination de plus à côté du disque USB ; chiffrer aussi `manifest.json` (noms des services) avant qu'il quitte la maison, en gardant l'enveloppe de la clé lisible.
- [ ] **TODO-4.11**: Sauvegarde croisée avec un autre pi-appliance (9.1) : jumelage par code, espace réservé fixé par chaque administrateur, archives déjà chiffrées.
- [ ] **TODO-4.12**: Restauration de l'appliance entière sur une nouvelle carte SD depuis l'assistant de premier démarrage (9.1) : choisir le disque, saisir le mot de passe en vigueur à la date de la sauvegarde, remettre `config.enc` puis chaque service.
- [ ] **TODO-4.13**: Restauration d'un fichier en parcourant une sauvegarde (9.1) ; les archives se lisent déjà entrée par entrée (`readArchive`).
- [ ] **TODO-4.14**: Écran des sauvegardes complet : nombres de sauvegardes gardées et arrêt de la sauvegarde de la nuit (déjà dans l'API `PUT /api/backups/settings`), changement du mot de passe administrateur dans la console avec rappel que les sauvegardes déjà faites gardent l'ancien (9.1, à faire avec TODO-4.1) ; un changement de mot de passe doit appeler `backup.Manager.Protect`.

## 5. Site www.pi-appliance.com (13)

- [x] **TODO-5.1**: Charte graphique (framboise, logo « La maison ») appliquée à la console et au site : `docs/charte-graphique.md`.
- [x] **TODO-5.2**: Page d'accueil du site : `docs/index.html`, `docs/site.css`.
- [x] **TODO-5.3**: Pages de documentation à la charte : `docs/_config.yml`, `docs/_layouts/default.html`.
- [x] **TODO-5.4**: GitHub Pages pour `www.pi-appliance.com` (source : branche `master`, dossier `/docs`), zone hébergée `pi-appliance.com` dans AWS Route 53.
- [x] **TODO-5.5**: Guide utilisateur de l'état du prototype, de ses fonctions et des essais restants : `docs/guide-utilisateur.md`.
- [x] **TODO-5.6**: Site mis à jour pour distinguer l'image construite de la version prête à télécharger ; essais sur un vrai Pi toujours à faire.
- [x] **TODO-5.7**: Page marketplace avec les applications présentes, les pistes à venir et les instructions de contribution.
- [x] **TODO-5.8**: Guide de téléchargement et de flashage de l'image expérimentale publiée comme artefact GitHub temporaire (expire le 2026-12-03).
- [ ] **TODO-5.9**: Publication d'une version stable de l'image après les essais sur un vrai Pi ; captures d'écran et FAQ.
- [ ] **TODO-5.10**: Inscription d'une adresse `<nom>.pi-appliance.com`.
- [ ] **TODO-5.11**: Traduction dans les 12 langues.
- [x] **TODO-5.12**: Ajouter à la TODO une option pour masquer et réafficher les actions réalisées.
