# Installer le socle pi-appliance sans l'image

Le **socle pi-appliance** est composé de Docker, Docker Compose, de la console pi-appliance, de Traefik, des outils du système, de la commande `pia` et des services systemd qui les démarrent. Il est inclus dans l'image pi-appliance ; ce n'est pas une application à installer depuis la marketplace. La personne qui configure le Pi crée son compte administrateur au premier démarrage. Cette procédure, pour les personnes à l'aise avec la ligne de commande, installe le même socle sur un Raspberry Pi OS Lite 64 bits déjà installé.

> Cette procédure installe le prototype local. L’accès public et les certificats Let's Encrypt ne sont pas encore activés : Traefik utilise le certificat auto-signé propre au Pi. Ne publiez pas les ports 80 et 443 tant que l’étape Internet décrite dans [l’avancement](TODO.md) n’est pas terminée.

## Prérequis

- Raspberry Pi 5, Pi 4, Pi 3 B+ ou Pi Zero 2 W ;
- Raspberry Pi OS Lite 64 bits, Trixie ;
- carte micro-SD de 16 Go minimum ;
- écran et clavier pour faciliter la première configuration, ou accès SSH ;
- connexion internet pour télécharger Docker et les paquets ;
- binaire Linux `arm64` de la console.

Avant l’installation, vérifiez l’architecture du Pi :

```shell
uname -m
```

La réponse doit être `aarch64`.

## Construire la console

Depuis un poste de développement équipé de Go 1.26 ou plus :

```shell
GOOS=linux GOARCH=arm64 go build -o pi-appliance-arm64 ./cmd/pi-appliance
```

Copiez ensuite le binaire sur le Pi :

```shell
scp pi-appliance-arm64 pi@pi-appliance.local:
```

Remplacez `pi@pi-appliance.local` par l’utilisateur et l’adresse réels du Pi.

## Installer Docker, Traefik et la console

Sur le Pi, récupérez une copie du dépôt puis lancez l’installation en root :

```shell
cd pi-appliance
sudo sh system/install.sh "$PWD/pi-appliance-arm64"
```

Le second argument est facultatif. Il peut recevoir une archive locale de l’image Traefik :

```shell
sudo sh system/install.sh "$PWD/pi-appliance-arm64" "$PWD/traefik-image.tar"
```

L’archive évite le téléchargement de Traefik au premier démarrage. Sans elle, Traefik est téléchargé par Docker et une connexion internet est nécessaire.

Le script :

1. installe Docker CE et Docker Compose ;
2. installe NetworkManager, Avahi et les paquets nécessaires ;
3. copie les fichiers de configuration sous `/etc/pi-appliance/` ;
4. installe les scripts sous `/usr/local/lib/pi-appliance/` ;
5. installe la console sous `/usr/local/bin/pi-appliance` ;
6. active les services systemd ;
7. démarre Docker, les secrets locaux, Traefik et la console.

Le script ne stocke aucun secret dans l’image ou le dépôt. La clé du Pi et le certificat local sont créés au démarrage dans `/var/lib/pi-appliance/system/`.

## Vérifier l’installation

Contrôlez les unités systemd :

```shell
sudo systemctl status pi-appliance-secrets.service pi-appliance-traefik.service pi-appliance.service
```

Les trois services doivent être actifs. Vérifiez que les ports 80 et 443 sont ouverts :

```shell
sudo ss -ltnp | grep -E ':(80|443)[[:space:]]'
```

Vérifiez aussi l’état de Docker et le réseau partagé :

```shell
sudo docker network inspect pi-appliance
sudo docker compose -f /etc/pi-appliance/traefik/compose.yaml ps
```

Ouvrez depuis le réseau de la maison :

```text
https://pi-appliance.local/pi-appliance/
```

Le nom peut aussi être remplacé par l’adresse IP du Pi. Le navigateur affiche une alerte pour le certificat local auto-signé. Vérifiez que le certificat appartient au Pi avant de poursuivre.

La console écoute uniquement sur `127.0.0.1:8080` ; elle doit être atteinte à travers Traefik, pas directement depuis le réseau.

## Réinstaller ou mettre à jour

Relancez l’installation avec le nouveau binaire :

```shell
cd pi-appliance
sudo sh system/install.sh "$PWD/pi-appliance-arm64"
```

Le script conserve la clé du Pi, les données de `/var/lib/pi-appliance/` et le réglage `data-root` de Docker. Il ne faut pas supprimer ces dossiers pour mettre à jour le socle.

Vérifiez ensuite les fichiers d’état :

```shell
ls -l /var/lib/pi-appliance/system/device.key \
  /var/lib/pi-appliance/system/device.pub \
  /var/lib/pi-appliance/system/tls/local.crt
sudo systemctl restart pi-appliance-secrets.service \
  pi-appliance-traefik.service pi-appliance.service
sudo docker compose -f /etc/pi-appliance/traefik/compose.yaml ps
```

## Dépanner

Afficher les journaux du socle :

```shell
sudo journalctl -u pi-appliance-secrets.service \
  -u pi-appliance-traefik.service -u pi-appliance.service \
  --since "1 hour ago"
```

Redémarrer les composants du socle :

```shell
sudo systemctl restart pi-appliance-secrets.service
sudo systemctl restart pi-appliance-traefik.service
sudo systemctl restart pi-appliance.service
```

Redémarrer Docker peut aussi redémarrer les services installés. Vérifiez leur état dans la console.

## Activation publique

Avant toute ouverture sur internet, il reste à réaliser :

- la configuration du résolveur ACME `letsencrypt` dans `system/etc/pi-appliance/traefik/traefik.yml` ;
- le label `tls.certresolver=letsencrypt` sur chaque routeur de service ;
- la restriction du routeur de la console au réseau de la maison ou au VPN ;
- le DNS, le routage de la box et le test depuis internet.

Ces éléments sont suivis par les tâches [TODO-3.1](TODO.md) à [TODO-3.12](TODO.md). Tant qu’ils ne sont pas réalisés, utilisez uniquement les adresses locales ou le réseau privé du Pi.
