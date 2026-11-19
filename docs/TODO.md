# TODO

Feuille de route de pi-appliance, dans l'ordre de construction de la section 16 de la [spécification](specification.md). Les numéros entre parenthèses renvoient à ses sections.

## 0. Ménage du dépôt

- [ ] Retirer les secrets versionnés (mots de passe Wi-Fi de `all-pi/wpa_supplicant.conf`, clés de `all-pi/authorized_keys`, `linux-tool/ssh/`) et les changer : ils restent dans l'historique git (8.4).
- [ ] Retirer les fichiers générés (`*.log`, `old.ip`, `ipcheck.dat`) et ajouter un `.gitignore`.
- [ ] Supprimer `tmp.uml` (exemple PlantUML sans rapport avec le projet).
- [x] Choisir une licence et ajouter `LICENSE` : AGPL-3.0 (1.2).
- [ ] Ajouter l'en-tête de licence aux fichiers de code existants.
- [ ] Vérifier l'origine et la licence des fichiers repris d'ailleurs (`docker-apache/`, d'après gwendal-orinel ; `linux-tool/dynHost/ipcheck.py`) avant de les intégrer au produit.

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
- [ ] Questions ouvertes de la section 15 : prix du relais, débit du Pi du projet, longueur du mot de passe administrateur, garde-fous du Compose libre, manifestes.

## 2. Étape 1 — Local

### Image système (4)

- [ ] Construction de l'image à partir de Raspberry Pi OS Lite 64 bits (`pi-gen`), une seule image pour les quatre modèles.
- [ ] Docker et Docker Compose préinstallés.
- [ ] Premier démarrage : nom d'hôte, mDNS `pi-appliance.local`, secrets propres au boîtier, certificat local.
- [ ] Point d'accès Wi-Fi « pi-appliance » et portail de configuration.
- [ ] Construction automatique de l'image (GitHub Actions) et publication avec somme de contrôle.

### Console (5)

- [ ] Service web sous `/pi-appliance`, en HTTPS, derrière Traefik.
- [ ] Authentification, changement obligatoire du mot de passe initial, e-mail vérifié.
- [ ] Fichier `reset` sur la carte SD (8.5).
- [ ] Assistant de premier démarrage.
- [ ] Tableau de bord : état du Pi et des services, graphiques sur 30 jours.
- [ ] Canaux de version stable et bêta.
- [ ] Traduction dans les 12 langues, arabe et persan de droite à gauche.
- [ ] Disque USB : détection, déplacement des données, alerte de remplissage de la carte SD.

### Marketplace (6)

- [ ] Format de manifeste et validation.
- [ ] Dépôt GitHub du catalogue et règles de contribution.
- [ ] Installation, démarrage, arrêt, désinstallation (question sur les données), plusieurs instances.
- [ ] Mise à jour d'un service avec retour arrière automatique.
- [ ] Avertissement mémoire sur les petits Pi.
- [ ] Applications : Nextcloud, Samba (dossiers commun et personnels), site web Apache/PHP (dépôt par Nextcloud, Samba ou ZIP), DokuWiki, Etherpad (liens de partage).
- [ ] Fichiers communs à Nextcloud et Samba.
- [ ] Réglages avancés et Docker Compose libre (6.4).

## 3. Étape 2 — Internet (7)

- [ ] Traefik : point d'entrée unique, routage par sous-domaine, certificats Let's Encrypt.
- [ ] DuckDNS tenu à jour par l'appliance.
- [ ] Service d'adresses du projet : réservation liée à la clé du Pi, alias vers DuckDNS, signes de vie, libération après 90 jours, noms interdits (7.1, 10.2).
- [ ] Service d'envoi d'e-mails du projet : codes de vérification, récupération, alertes (8.5, 8.6, 10.2).
- [ ] Domaine personnel : guide OVH, à partir de `linux-tool/dynHost/`.
- [ ] Accès direct : UPnP, guide de la box, test depuis internet.
- [ ] Relais : serveur, tunnel sortant (à partir de `all-pi/autossh.sh`), bascule automatique, limites de l'offre gratuite, offre payante.
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

- [ ] Activer GitHub Pages pour `www.pi-appliance.com` et corriger la zone DNS chez OVH.
- [ ] Présentation, téléchargement, documentation, FAQ, catalogue.
- [ ] Inscription d'une adresse `<nom>.pi-appliance.com`.
- [ ] Traduction dans les 12 langues.
