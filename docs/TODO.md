# TODO

Feuille de route de pi-appliance. Le produit visé est décrit dans [specification.md](specification.md) ; les numéros entre parenthèses renvoient à ses sections.

## 0. Ménage du dépôt

- [ ] Retirer les secrets versionnés (mots de passe Wi-Fi de `all-pi/wpa_supplicant.conf`, clés de `all-pi/authorized_keys`, `linux-tool/ssh/`) et les changer : ils restent dans l'historique git (8).
- [ ] Retirer les fichiers générés (`*.log`, `old.ip`, `ipcheck.dat`) et ajouter un `.gitignore`.
- [ ] Supprimer `tmp.uml` (exemple PlantUML sans rapport avec le projet).
- [ ] Choisir une licence et ajouter `LICENSE`.

## 1. Décisions (14)

- [x] Langage de la console : Go (10.1).
- [ ] Format des manifestes de la marketplace.
- [ ] Solution d'accès sans IP publique (relais).
- [ ] Gestion des comptes : SSO commun ou comptes par service.

## 2. Image système (4)

- [ ] Script de construction de l'image à partir de Raspberry Pi OS Lite 64 bits (pi-gen ou personnalisation d'image).
- [ ] Docker et Docker Compose préinstallés.
- [ ] Premier démarrage : nom d'hôte, mDNS `pi-appliance.local`, secrets propres au boîtier, certificat local.
- [ ] Construction automatique de l'image (GitHub Actions) et publication avec somme de contrôle.

## 3. Console (5)

- [x] Squelette Go : page d'accueil sous `/pi-appliance/`, `api/health`, tests, binaire `arm64` construit par GitHub Actions (10.1).
- [ ] Service systemd et passage derrière Traefik, en HTTPS.
- [ ] Authentification, changement obligatoire du mot de passe initial.
- [ ] Assistant de premier démarrage : domaine, exposition, utilisateurs, premiers services.
- [ ] Tableau de bord : état du Pi et des services.
- [ ] Gestion des utilisateurs.

## 4. Marketplace (6)

- [ ] Format de manifeste et validation.
- [ ] Installation, démarrage, arrêt, mise à jour, désinstallation d'un service.
- [ ] Premières applications, à partir des prototypes : Nextcloud (`docker-nextcloud/`), Samba (`docker-samba/`), Apache/PHP (`docker-apache/`).
- [ ] Puis : Motion (`pi-motion/`), timelapse (`pi-timelaps/`).

## 5. Exposition sur internet (7)

- [ ] Traefik : point d'entrée unique, routage par sous-domaine, certificats Let's Encrypt.
- [ ] DNS dynamique OVH, à partir de `linux-tool/dynHost/`.
- [ ] Aide à la redirection de ports ; UPnP si possible.
- [ ] Test d'accès depuis internet dans la console.
- [ ] Repli par tunnel sortant, à partir de `all-pi/autossh.sh`.

## 6. Sauvegarde et mises à jour (9)

- [ ] Sauvegarde de la configuration et des volumes, à partir de `all-pi/bkp.sh`.
- [ ] Restauration sur une nouvelle carte SD.
- [ ] Mise à jour de la couche pi-appliance depuis la console.

## 7. Site et documentation

- [ ] Activer GitHub Pages pour `www.pi-appliance.com` et corriger la zone DNS chez OVH.
- [ ] Guide d'installation pas à pas pour l'utilisateur final.
