# Guide utilisateur de pi-appliance

Ce guide explique comment installer pi-appliance sur un Raspberry Pi, puis comment s'en servir pour installer des services comme Nextcloud.

> **Version d'essai.** L'image pi-appliance prête à flasher n'existe pas encore (voir le [TODO](TODO.md)). En attendant, l'installation se fait à la main, en ligne de commande : elle s'adresse aux personnes à l'aise avec SSH. Quand l'image sera disponible, ce guide se résumera à « flasher la carte SD et ouvrir la console ».

## 1. Ce que fait cette version

- Une console web, en français, pour installer et désinstaller des services.
- Une marketplace avec une première application : **Nextcloud** (fichiers, photos, agenda, contacts).
- Plusieurs services de la même application possibles, chacun sous son propre nom.
- À la désinstallation, le choix de garder ou de supprimer les données.

Pas encore : connexion par mot de passe à la console, accès depuis internet, adresse `<nom>.pi-appliance.com`, sauvegardes, mises à jour depuis la console, autres langues. En attendant la connexion par mot de passe, installer et désinstaller ne sont possibles que **depuis le Pi lui-même** (ou à travers un tunnel SSH, section 7).

## 2. Ce qu'il faut

- Un Raspberry Pi 5, Pi 4, Pi 3 B+ ou Pi Zero 2 W. Pour Nextcloud, 2 Go de mémoire ou plus sont conseillés ; avec moins, la console prévient que le service risque d'être lent.
- Une carte micro-SD de 16 Go au moins (32 Go conseillés) et son alimentation officielle.
- Un ordinateur sur le même réseau que le Pi, avec [Raspberry Pi Imager](https://www.raspberrypi.com/software/).
- Une connexion à internet : le Pi télécharge Docker puis les services.

## 3. Préparer la carte SD

1. Ouvrir Raspberry Pi Imager et choisir le modèle de Pi.
2. Système : **Raspberry Pi OS (other) > Raspberry Pi OS Lite (64-bit)**.
3. Choisir la carte SD, puis **Modifier les réglages** :
   - nom d'hôte : `pi-appliance` ;
   - un nom d'utilisateur et un mot de passe (dans la suite : `pi`) ;
   - le Wi-Fi de la maison, si le Pi n'est pas branché par câble ;
   - onglet **Services** : activer **SSH**.
4. Écrire la carte, la mettre dans le Pi et le brancher. Le premier démarrage prend quelques minutes.

## 4. Se connecter au Pi

Depuis l'ordinateur, dans un terminal (PowerShell sous Windows) :

```shell
ssh pi@pi-appliance.local
```

Si `pi-appliance.local` ne répond pas, utiliser l'adresse IP du Pi, affichée dans l'interface de la box. Mettre ensuite le système à jour :

```shell
sudo apt-get update && sudo apt-get full-upgrade -y
```

## 5. Installer Docker

pi-appliance a besoin de Docker et de Docker Compose (commande `docker compose`). Les installer depuis le dépôt officiel de Docker, sur le Pi :

```shell
sudo apt-get install -y ca-certificates curl
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
```

Vérifier :

```shell
sudo docker compose version
```

Les paquets `docker.io` et `docker-compose` de Raspberry Pi OS ne conviennent pas : ils n'ont pas la commande `docker compose` récente dont pi-appliance a besoin.

## 6. Installer la console pi-appliance

### 6.1 Obtenir le programme

La console est un seul fichier, `pi-appliance-arm64`. Deux façons de l'obtenir :

- **Depuis GitHub** (compte GitHub nécessaire) : onglet [Actions](https://github.com/gmoulard/pi-appliance/actions/workflows/go.yml) du dépôt, dernière exécution réussie sur `master`, artefact **pi-appliance-arm64** (un fichier ZIP à décompresser).
- **En le construisant** sur l'ordinateur, avec [Go](https://go.dev/dl/) 1.24 ou plus, depuis une copie du dépôt :

  ```shell
  GOOS=linux GOARCH=arm64 go build -o pi-appliance-arm64 ./cmd/pi-appliance
  ```

  (sous PowerShell : `$env:GOOS="linux"; $env:GOARCH="arm64"; go build -o pi-appliance-arm64 ./cmd/pi-appliance`).

Copier le fichier sur le Pi, depuis l'ordinateur :

```shell
scp pi-appliance-arm64 pi@pi-appliance.local:
```

puis, sur le Pi :

```shell
sudo install -m 755 pi-appliance-arm64 /usr/local/bin/pi-appliance
```

### 6.2 Démarrage automatique

Créer le service système, sur le Pi :

```shell
sudo tee /etc/systemd/system/pi-appliance.service > /dev/null <<'EOF'
[Unit]
Description=Console pi-appliance
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/pi-appliance -addr 127.0.0.1:8080 -data /var/lib/pi-appliance -domain pi-appliance.local
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now pi-appliance
```

Vérifier qu'elle répond :

```shell
curl http://127.0.0.1:8080/pi-appliance/api/health
```

La réponse ressemble à `{"status":"ok","version":"…"}`. La console démarre désormais toute seule avec le Pi.

Options de la console :

| Option | Rôle | Valeur dans ce guide |
|--------|------|----------------------|
| `-addr` | Adresse d'écoute | `127.0.0.1:8080` : la console n'est joignable que depuis le Pi, tant qu'elle n'a pas de mot de passe |
| `-data` | Dossier des services installés | `/var/lib/pi-appliance` |
| `-domain` | Adresse de l'appliance : chaque service est publié à `<nom du service>.<domain>` | `pi-appliance.local` (voir section 9) |

## 7. Ouvrir la console

Le Pi n'a pas d'écran dans ce guide : on ouvre la console depuis l'ordinateur, à travers un tunnel SSH. Dans un terminal de l'ordinateur, laissé ouvert :

```shell
ssh -L 8080:127.0.0.1:8080 pi@pi-appliance.local
```

puis, dans le navigateur de l'ordinateur : **http://localhost:8080/pi-appliance/**

La page affiche « Appliance en service », la liste **Mes services** et la **Marketplace**. La version de la console est en bas de page.

## 8. Installer un service

1. Dans **Marketplace**, sous **Nextcloud**, garder le nom proposé (`cloud`) ou en choisir un autre : lettres minuscules, chiffres et tirets. Ce nom devient le début de l'adresse du service.
2. Cliquer sur **Installer**. La première installation télécharge Nextcloud et sa base de données (près de 1 Go) : compter plusieurs minutes, sans fermer la page.
3. Quand c'est fini, la console affiche l'adresse du service et le **mot de passe de l'administrateur de Nextcloud** (compte `admin`). **Le noter** : il n'est affiché qu'une fois.
4. Le service apparaît dans **Mes services**.

Pour une seconde instance (un Nextcloud pour l'association, par exemple), recommencer avec un autre nom.

Si la console affiche un avertissement de mémoire, le service est installé mais risque d'être lent sur ce Pi.

## 9. Ouvrir le service (avant-première)

Le point d'entrée des services, Traefik, n'est pas encore installé par pi-appliance : l'adresse affichée par la console ne répond donc pas encore. En attendant, on peut l'installer à la main. Cette section est une avant-première, pas encore vérifiée sur un Pi.

1. Donner à la console une adresse qui se résout vers le Pi. Le service gratuit [nip.io](https://nip.io) transforme `192-168-1-20.nip.io` en `192.168.1.20` : remplacer, dans `/etc/systemd/system/pi-appliance.service`, `-domain pi-appliance.local` par `-domain 192-168-1-20.nip.io` (avec l'adresse IP du Pi, points remplacés par des tirets), puis :

   ```shell
   sudo systemctl daemon-reload
   sudo systemctl restart pi-appliance
   ```

   Un service déjà installé garde son ancienne adresse : le désinstaller en **gardant les données**, puis le réinstaller sous le même nom.

2. Démarrer Traefik sur le Pi :

   ```shell
   sudo docker network create pi-appliance 2>/dev/null || true
   sudo docker run -d --name traefik --restart unless-stopped \
     -p 80:80 -p 443:443 --network pi-appliance \
     -v /var/run/docker.sock:/var/run/docker.sock:ro \
     traefik:v3.3 \
     --providers.docker=true --providers.docker.exposedbydefault=false \
     --entrypoints.web.address=:80 --entrypoints.websecure.address=:443 \
     --entrypoints.web.http.redirections.entrypoint.to=websecure
   ```

3. Ouvrir `https://cloud.192-168-1-20.nip.io` depuis un appareil de la maison. Le navigateur prévient que le certificat n'est pas reconnu (certificat provisoire de Traefik) : accepter, puis se connecter à Nextcloud avec `admin` et le mot de passe noté à la section 8.

## 10. Désinstaller un service

Dans **Mes services**, **Désinstaller**, puis répondre aux questions :

- **Garder les données** (OK) : les fichiers et la base de données restent sur le Pi ; une réinstallation sous le même nom les retrouve, avec le même mot de passe.
- **Les supprimer** (Annuler, puis confirmer) : tout est effacé définitivement.

## 11. Mettre à jour la console

Obtenir le nouveau `pi-appliance-arm64` (section 6.1), le copier sur le Pi, puis :

```shell
sudo install -m 755 pi-appliance-arm64 /usr/local/bin/pi-appliance
sudo systemctl restart pi-appliance
```

Les services installés ne sont pas touchés.

## 12. En cas de problème

| Ce qu'on voit | Quoi faire |
|---------------|------------|
| La page ne s'ouvre pas | Vérifier que le tunnel SSH (section 7) est ouvert, puis l'état de la console : `sudo systemctl status pi-appliance`. |
| « Cette action n'est possible que depuis le Pi lui-même pour l'instant. » | La console a été ouverte par l'adresse réseau du Pi : passer par le tunnel SSH et `http://localhost:8080/pi-appliance/`. |
| « … n'a pas pu démarrer : … » | La fin du message est celle de Docker. Vérifier la connexion à internet et la place libre (`df -h`), puis réessayer. |
| « Un service s'appelle déjà … » | Choisir un autre nom, ou désinstaller l'ancien service. |
| « Appliance injoignable » | La console s'est arrêtée : `sudo journalctl -u pi-appliance -n 50` affiche ses derniers messages. |

Journal de la console en direct : `sudo journalctl -u pi-appliance -f`. Services Docker en cours : `sudo docker compose ls`.

## 13. Où sont les données

- `/var/lib/pi-appliance/services/<nom>/` : description du service et fichier `.env` avec ses mots de passe (lisible par l'administrateur du Pi seulement). Ne pas le publier ni le copier sans précaution.
- Volumes Docker `pi-<nom>_…` : fichiers et base de données du service (`sudo docker volume ls`).

## 14. Tout retirer

Désinstaller chaque service en supprimant ses données (section 10), puis sur le Pi :

```shell
sudo systemctl disable --now pi-appliance
sudo rm /etc/systemd/system/pi-appliance.service /usr/local/bin/pi-appliance
sudo rm -r /var/lib/pi-appliance
sudo docker rm -f traefik 2>/dev/null; sudo docker network rm pi-appliance 2>/dev/null
```
