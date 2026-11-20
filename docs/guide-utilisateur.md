# Guide du prototype pi-appliance

> **État du projet : expérimental.** Une première image système est disponible comme artefact temporaire de GitHub Actions, mais elle n'a pas encore été testée sur un vrai Raspberry Pi. Elle n'est pas une version stable : utilisez-la uniquement pour des essais et n'y confiez pas de données importantes.

## Ce que contient le prototype

- Une console en français avec création du compte administrateur, connexion et assistant de première mise en route.
- La configuration du réseau filaire ou du Wi-Fi depuis la console, ainsi qu'un point d'accès Wi-Fi de configuration au premier démarrage de l'image.
- Un tableau de bord avec l'état du Pi, des graphiques et des alertes de mémoire et de stockage.
- Une marketplace avec les applications installables du catalogue.
- L'installation de plusieurs instances, les commandes de démarrage et d'arrêt, la mise à jour des applications avec retour arrière, et le choix de conserver ou supprimer leurs données à la désinstallation.
- La gestion d'un disque USB pour les données des services.

Ces fonctions sont présentes dans le code, mais restent à valider sur du matériel réel. La liste des essais restants est dans la page [Avancement](TODO.html).

## Ce qui n'est pas encore disponible

- Une version stable de l'image, testée sur un vrai Raspberry Pi.
- L'accès aux services depuis internet et la réservation d'une adresse `*.pi-appliance.com`.
- Les sauvegardes, le VPN et les mises à jour de l'appliance depuis la console.

## Télécharger et installer l'image expérimentale {#telecharger-limage-experimentale}

Le workflow GitHub Actions a construit l'image avec succès le 19 novembre 2026. L'artefact **`pi-appliance-image`** comprend l'image et son fichier de somme SHA-256 ; il expire le **3 décembre 2026**. [Télécharger directement l'image et sa somme de contrôle](https://github.com/gmoulard/pi-appliance/actions/runs/44578711083/artifacts/14559608001). Il s'agit d'un artefact temporaire, pas d'une version stable. Le fichier image est `pi-appliance-2026-11-19-48c047d.img.xz` (environ 720 Mio compressé, 3,3 Gio une fois décompressé).

Dans cette documentation, **controlTower** désigne le service de contrôle : la console d'administration de pi-appliance. Inclus dans l'image, il n'est pas installé depuis la marketplace et son administration est réservée au compte créé par la personne qui configure l'appliance. Avec l'image expérimentale du 19 novembre 2026, le Raspberry Pi garde le nom réseau `pi-appliance` ; l'adresse de la console est donc `https://pi-appliance.local/pi-appliance/`. Avec les images suivantes, chaque Pi a son propre nom, `pi-appliance-XXXX`, modifiable dans la console (section « Réseau de la maison », « Nom du Pi sur le réseau ») : un Pi nommé `controltower` s'ouvre à `https://controltower.local/pi-appliance/`.

L'image est destinée aux Raspberry Pi 5, 4, 3 B+ et Zero 2 W. Il faut une carte micro-SD de 16 Go ou plus, un ordinateur et [Raspberry Pi Imager](https://www.raspberrypi.com/software/). Le flashage efface entièrement la carte SD.

1. Sur la page de l'exécution GitHub, téléchargez l'artefact `pi-appliance-image` (un fichier ZIP), puis décompressez-le. Vous devez obtenir le fichier `.img.xz` et son fichier `.img.xz.sha256`.
2. **Vérifiez le téléchargement avant de flasher.** Sous Windows PowerShell, depuis le dossier décompressé :

   ```powershell
   $image = ".\pi-appliance-2026-11-19-48c047d.img.xz"
   $expected = (Get-Content "$image.sha256").Split()[0]
   $actual = (Get-FileHash $image -Algorithm SHA256).Hash
   if ($actual -ne $expected) { throw "Somme SHA-256 invalide : ne flashez pas cette image." }
   ```

   Sous Linux ou macOS, dans ce même dossier :

   ```shell
   sha256sum -c pi-appliance-2026-11-19-48c047d.img.xz.sha256
   ```

   Ne flashez pas l'image si la vérification échoue.
3. Décompressez le fichier `.img.xz` avec 7-Zip pour obtenir le fichier `.img`.
4. Téléchargez et ouvrez [Raspberry Pi Imager](https://www.raspberrypi.com/software/). Choisissez le modèle de Pi, puis **Choisir l'OS > Utiliser une image personnalisée** et sélectionnez le `.img`. Choisissez la carte SD, puis lancez l'écriture. Confirmez l'effacement demandé par Imager.
5. Insérez la carte dans le Raspberry Pi et démarrez-le. Au premier démarrage, le Pi ouvre un réseau Wi-Fi de configuration **`pi-appliance`**, sans mot de passe, pour une durée maximale de 24 heures. Connectez-y votre téléphone ou ordinateur, puis ouvrez `https://10.42.0.1/pi-appliance/` si le portail ne s'affiche pas automatiquement. Avec les images construites à partir du 20 novembre 2026, ce Wi-Fi porte le nom propre du Pi, `pi-appliance-XXXX` (quatre caractères qui changent d'un Pi à l'autre), et ne donne plus accès à internet ni à votre réseau : seulement au portail.
6. Dans l'assistant, créez le mot de passe administrateur et choisissez le Wi-Fi de la maison ou le réseau filaire. Le navigateur peut signaler le certificat local auto-signé : ne continuez que si vous êtes bien connecté au point d'accès de votre propre Pi. Une fois le réseau configuré, ouvrez `https://pi-appliance.local/pi-appliance/` depuis le réseau de la maison.

7. Dans la console, faites défiler la page sous **Mes services** jusqu'à la rubrique **Marketplace**. Si vous êtes encore dans l'assistant de première mise en route, choisissez **Vos premiers services** : il affiche cette même rubrique. Sélectionnez une application, choisissez son nom puis cliquez sur **Installer**. Une adresse locale comme `www.pi-appliance.local` apparaît dans **Mes services** quand l'installation réussit. Pour un site web, trois méthodes permettent de déposer les fichiers :

   - **ZIP** : connectez-vous comme administrateur, ouvrez la carte du site puis choisissez **Déposer un ZIP**. Envoyez une archive de 256 Mio maximum. Son contenu remplace intégralement les fichiers du site ; les chemins dangereux et les liens symboliques sont refusés.
   - **Nextcloud** : depuis un compte administrateur Nextcloud, ajoutez dans les paramètres de stockage externe le dossier `/srv/pi-appliance/services/<nom-du-site>/www`, accessible au conteneur Nextcloud. Les fichiers placés dans ce dossier sont servis directement par Apache.
   - **FTP** : utilisez le compte `pi`, le port `2121` par défaut et le mot de passe FTP affiché une seule fois à la fin de l'installation. Un autre port peut être choisi au moment de l'installation du site.

Le compte FTP est réservé au contenu du site. Si vous perdez son mot de passe, désinstallez puis réinstallez le site avec **Garder les données** pour créer une nouvelle installation avec de nouvelles données.

La partition, le démarrage et le portail de cette image restent à valider sur du matériel réel. L'accès aux applications depuis internet n'est pas encore disponible.

## Lancer la console depuis les sources

Cette procédure est destinée aux contributeurs, pas à l'installation d'une appliance. Elle nécessite Go 1.24 ou plus. Docker et Docker Compose sont nécessaires pour installer des services depuis la console.

À la racine du dépôt :

```shell
go test ./...
go run ./cmd/pi-appliance -data ./tmp
```

Ouvrir ensuite **http://localhost:8080/pi-appliance/** dans le navigateur et créer le compte administrateur. Les données de cette instance de développement sont écrites dans `./tmp`.

Pour construire l'image système, utiliser le workflow [Image](https://github.com/gmoulard/pi-appliance/actions/workflows/image.yml). Il est réservé aux essais et à la contribution tant que les tests sur un vrai Pi ne sont pas terminés.

## Pour aller plus loin

- [Installation du plan de contrôle](installation.html) : Docker, Traefik, console et services systemd sur un Raspberry Pi OS existant.
- [Marketplace](marketplace.html) : catalogue actuel, applications envisagées et guide de contribution.
- [Spécification](specification.html) : fonctionnement visé par le projet.
- [Avancement](TODO.html) : fonctions réalisées et essais restants.
- [Code source](https://github.com/gmoulard/pi-appliance) : dépôt et workflows.
