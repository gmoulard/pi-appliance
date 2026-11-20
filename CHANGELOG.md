# Historique des versions

Une ligne par version publiée, la plus récente en bas : numéro, date, auteur, ce qui change pour l'utilisateur. Procédure : skill `release` et [mémo Git](docs/manGit.md#publier-une-version-de-pi-appliance).

- **0.1.0** — 2026-11-20 — GMoulard — Première version expérimentale : image pour Raspberry Pi, console d'administration, marketplace (Nextcloud, site web, DokuWiki, Etherpad, PostgreSQL, WireGuard, générateurs de sites), sauvegardes, mises à jour proposées depuis la console.
- **0.1.1** — 2026-11-20 — GMoulard — Accès réglable par service (tout le monde, comptes connectés, maison seulement) ; Cockpit pour administrer le système du Pi depuis la console ; adresse `<nom>.pi-appliance.com` ; site web sans FTP ni dépôt par ZIP, qui empêchaient son installation.
- **0.1.2** — 2026-11-20 — GMoulard — Corrections du premier essai sur un Pi 3B+ : la réinitialisation par le fichier `reset` survit à un redémarrage ; mise en route plus claire (accueil, pays du Wi-Fi expliqué, double authentification et changement de nom hors de l'assistant) ; nom du Pi vérifié libre à l'arrivée sur le réseau de la maison ; adresse de la console donnée avant la bascule ; logo framboise dans la console.
