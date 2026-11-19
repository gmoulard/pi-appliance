# TODO

Feuille de route de pi-appliance, dans l'ordre de construction de la section 16 de la [spécification](specification.md). Les numéros entre parenthèses renvoient à ses sections.

## 0. Ménage du dépôt

- [x] Retirer les anciens prototypes (`all-pi/`, `linux-tool/`, `docker-*/`, `pi-motion/`, `pi-timelaps/`, `RaspController/`, `pypy/`) : ils restent dans l'historique git, au commit `4ee13f9` (spécification, section 12).
- [ ] Changer les secrets qui étaient versionnés (mots de passe Wi-Fi de `all-pi/wpa_supplicant.conf`, clés de `all-pi/authorized_keys`, `linux-tool/ssh/`) : retirés du dépôt, ils restent dans l'historique git (8.4).
- [x] Retirer les fichiers générés (`*.log`, `old.ip`, `ipcheck.dat`) et ajouter un `.gitignore`.
- [x] Supprimer `tmp.uml` (exemple PlantUML sans rapport avec le projet).
- [x] Choisir une licence et ajouter `LICENSE` : AGPL-3.0 (1.2).
- [ ] Ajouter l'en-tête de licence aux fichiers de code existants (Go et interface web).
- [ ] Vérifier l'origine et la licence des fichiers repris d'ailleurs (`docker-apache/`, d'après gwendal-orinel ; `linux-tool/dynHost/ipcheck.py`) avant de les reprendre depuis l'historique git.

## 1. Décisions

- [x] Public visé : grand public (1.1).
- [x] Objectif : Nextcloud sur internet en 15 minutes, sans aide (1.3).
- [x] Langage de la console : Go (10.1).
- [x] Matériel : Pi 5, 4, 3 B+, Zero 2 W ; avertissement pour les applications trop lourdes (4).
- [x] Premier réseau : point d'accès Wi-Fi de configuration (4).
- [x] Stockage : carte SD, disque USB proposé (4).
- [x] Catalogue : dépôt GitHub du projet, contributions par pull request (6.1).
- [x] Applications v1 : Nextcloud, Samba, site web, DokuWiki, Etherpad, WireGuard (6.3).
- [x] Désinstallation : toujours demander pour les données (6.2).
- [x] Adresse : `<nom>.pi-appliance.com`, alias vers un DNS dynamique gratuit ; domaine personnel en option (7.1).
- [x] Accès : direct (UPnP ou guide), relais du projet sinon (7.2, 7.3).
- [x] Connexion unique ; rôles administrateur, utilisateur, invité (8.1).
- [x] Accès choisi par service : public, connectés, maison (8.2).
- [x] Console sur internet seulement avec double authentification (8.3).
- [x] Statistiques anonymes sur accord explicite (8.4).
- [x] Sauvegardes : disque USB, cloud, autre pi-appliance (9.1).
- [x] Mises à jour proposées, jamais automatiques (9.2).
- [x] Interface et site dans les 12 langues de Wikipedia2Book (5.3, 13).
- [x] Point d'accès Wi-Fi au premier démarrage même avec un câble Ethernet (4).
- [x] E-mail de l'administrateur obligatoire et vérifié (5.1).
- [x] DNS dynamique : DuckDNS ; adresse liée à l'appliance, libérée après 90 jours (7.1).
- [x] Relais gratuit avec limites, payant au-delà (7.3).
- [x] Fournisseur d'identité intégré à la console (8.1).
- [x] Récupération par e-mail, fichier `reset` sur la carte SD en dernier recours (8.5).
- [x] Alertes par e-mail (8.6).
- [x] Sauvegarde chaque nuit, 7 quotidiennes, 4 hebdomadaires, 6 mensuelles (9.1).
- [x] Correctifs de sécurité proposés comme le reste, rappelés par e-mail (9.2).
- [x] Services du projet hébergés sur un Pi du propriétaire du projet (10.2).
- [x] E-mails envoyés par le relais SMTP d'OVH ; sauvegarde et Pi de secours pour les services du projet ; minimum de données personnelles (10.2).
- [x] Relais gratuit : 5 Mbit/s, 20 Go par mois, sans garantie (7.3).
- [x] LDAP seulement avec Samba ; invitation par e-mail ; mots de passe de 8 caractères ; double authentification facultative ; pas de limite de comptes (8.1).
- [x] Installation réservée aux administrateurs, plusieurs instances, retour arrière automatique (6.2).
- [x] Réglages avancés et Docker Compose libre (6.4).
- [x] Samba (commun + personnel), fichiers partagés avec Nextcloud, dépôt du site web, Etherpad (6.3).
- [x] Clé des sauvegardes protégée par le mot de passe administrateur, jumelage par code, restauration appliance/service/fichier, formatage ext4 avec accord (9.1).
- [x] Graphiques sur 30 jours ; une console par appliance (5.2) ; canaux stable et bêta (5.4).
- [x] Format des manifestes : Docker Compose + section `x-pi-appliance` (6.5).
- [x] Relais : nombre d'appliances relayées limité, liste d'attente (7.3).
- [x] Mot de passe administrateur de 12 caractères au moins (8.1).
- [x] Compose libre : options dangereuses signalées, pas bloquées (6.4).
- [x] Pas d'identifiant initial : compte administrateur créé dans le portail (5.1).
- [x] Point d'accès Wi-Fi ouvert, limité à 24 heures (4).
- [x] E-mail de l'administrateur vérifié dès que le service d'e-mails est joignable (5.1).
- [x] Certificat auto-signé avant Let's Encrypt, alerte expliquée (8.4).
- [ ] Questions ouvertes de la section 15 : prix et paiement du relais, nombre maximal d'appliances relayées (à l'étape Internet).

## 2. Étape 1 — Local

### Image système (4)

- [ ] Construction de l'image à partir de Raspberry Pi OS Lite 64 bits (`pi-gen`), une seule image pour les quatre modèles.
- [ ] Docker et Docker Compose préinstallés.
- [ ] Premier démarrage : nom d'hôte, mDNS `pi-appliance.local`, secrets propres au boîtier, certificat local.
- [ ] Point d'accès Wi-Fi « pi-appliance » ouvert, limité à 24 heures, et portail de configuration avec création du compte administrateur.
- [ ] Construction automatique de l'image (GitHub Actions) et publication avec somme de contrôle.

### Console (5)

- [x] Squelette Go : page d'accueil sous `/pi-appliance/`, `api/health`, tests, binaire `arm64` construit par GitHub Actions (10.1).
- [ ] Service systemd et passage derrière Traefik, en HTTPS.
- [x] Authentification : compte administrateur créé au premier démarrage, connexion, sessions, limitation des tentatives (5.1, 8.1, 8.4).
- [ ] Vérification de l'e-mail par un code, dès que le service d'e-mails est joignable (5.1, étape 2).
- [x] Fichier `reset` sur la carte SD (8.5).
- [ ] Assistant de premier démarrage.
- [ ] Tableau de bord : état du Pi et des services, graphiques sur 30 jours.
- [ ] Canaux de version stable et bêta.
- [ ] Traduction dans les 12 langues, arabe et persan de droite à gauche.
- [ ] Disque USB : détection, déplacement des données, alerte de remplissage de la carte SD.

### Marketplace (6)

- [x] Format de manifeste et validation (6.5).
- [ ] Dépôt GitHub du catalogue et règles de contribution ; lecture en ligne (aujourd'hui intégré au binaire).
- [x] Installation, désinstallation (question sur les données), plusieurs instances (6.5).
- [ ] Démarrer, arrêter, redémarrer un service ; état vert/orange/rouge.
- [ ] Mise à jour d'un service avec retour arrière automatique.
- [x] Avertissement mémoire sur les petits Pi.
- [x] Ouvrir l'installation au réseau une fois l'authentification faite : réservée aux administrateurs connectés.
- [x] Application Nextcloud (manifeste `marketplace/nextcloud/`).
- [ ] Applications : Samba (dossiers commun et personnels), site web Apache/PHP (dépôt par Nextcloud, Samba ou ZIP), DokuWiki, Etherpad (liens de partage).
- [ ] Fichiers communs à Nextcloud et Samba.
- [ ] Réglages avancés et Docker Compose libre (6.4).

## 3. Étape 2 — Internet (7)

- [ ] Traefik : point d'entrée unique, routage par sous-domaine, certificats Let's Encrypt.
- [ ] DuckDNS tenu à jour par l'appliance.
- [ ] Service d'adresses du projet : réservation liée à la clé du Pi, alias vers DuckDNS, signes de vie, libération après 90 jours, noms interdits (7.1, 10.2).
- [ ] Service d'envoi d'e-mails du projet : codes de vérification, récupération, alertes (8.5, 8.6, 10.2).
- [ ] Domaine personnel : guide OVH, à partir de l'ancien `linux-tool/dynHost/` (historique git).
- [ ] Accès direct : UPnP, guide de la box, test depuis internet.
- [ ] Relais : serveur, tunnel sortant (à partir de l'ancien `all-pi/autossh.sh`, historique git), bascule automatique, limites de l'offre gratuite, nombre maximal d'appliances et liste d'attente, offre payante.
- [ ] Accès par service : public, connectés, maison (8.2).
- [ ] Console ouverte sur internet avec double authentification (8.3).

## 4. Étape 3 — Le reste

- [ ] Fournisseur d'identité intégré à la console : OpenID Connect, *forward auth* Traefik, LDAP pour Samba, double authentification, invitations par e-mail ; rôles et invités (8.1, 8.3).
- [ ] Alertes par e-mail, désactivables par type (8.6).
- [ ] Sauvegardes : chaque nuit, rotation 7/4/6, disque USB, cloud, autre pi-appliance (jumelage par code), chiffrement, restauration de l'appliance, d'un service ou d'un fichier (9.1).
- [ ] Services du projet : relais SMTP OVH (SPF, DKIM, DMARC), sauvegarde hors de la maison, Pi de secours (10.2).
- [ ] Mises à jour proposées dans la console (9.2).
- [ ] VPN WireGuard avec QR code (6.3).
- [ ] Statistiques anonymes et leur service de réception (8.4, 10.2).
- [ ] Puis : applications caméra (Motion, timelapse).

## 5. Site www.pi-appliance.com (13)

- [x] Charte graphique (framboise, logo « La maison ») appliquée à la console et au site : `docs/charte-graphique.md`.
- [x] Page d'accueil du site : `docs/index.html`, `docs/site.css`.
- [x] Pages de documentation à la charte : `docs/_config.yml`, `docs/_layouts/default.html`.
- [ ] Activer GitHub Pages pour `www.pi-appliance.com` (source : branche `master`, dossier `/docs`) et corriger la zone DNS chez OVH.
- [x] Guide utilisateur provisoire avec manuel d'installation manuelle : `docs/guide-utilisateur.md`.
- [ ] Captures d'écran, téléchargement de l'image, FAQ, catalogue complet.
- [ ] Inscription d'une adresse `<nom>.pi-appliance.com`.
- [ ] Traduction dans les 12 langues.
