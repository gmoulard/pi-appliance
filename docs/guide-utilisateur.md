# Guide du prototype pi-appliance

> **État du projet : expérimental.** Une première image système est disponible comme artefact temporaire de GitHub Actions, mais elle n'a pas encore été testée sur un vrai Raspberry Pi. Elle n'est pas une version stable : utilisez-la uniquement pour des essais et n'y confiez pas de données importantes.

## Ce que contient le prototype

- Une console en français avec création du compte administrateur, connexion et assistant de première mise en route (réseau, test du Wi-Fi, redémarrage).
- La configuration du réseau filaire ou du Wi-Fi depuis la console, ainsi qu'un point d'accès Wi-Fi de configuration au premier démarrage de l'image.
- Un tableau de bord avec l'état du Pi, des graphiques et des alertes de mémoire et de stockage.
- Une marketplace avec les applications installables du catalogue.
- L'installation de plusieurs instances, les commandes de démarrage et d'arrêt, la mise à jour des applications avec retour arrière, et le choix de conserver ou supprimer leurs données à la désinstallation.
- La gestion d'un disque USB pour les données des services.
- Les sauvegardes chiffrées sur un second disque USB, chaque nuit et à la demande, et la restauration d'un service à une date choisie (écran « Sauvegardes » dans « Mon Pi »), pas encore essayées sur un vrai Pi.

Ces fonctions sont présentes dans le code, mais restent à valider sur du matériel réel. La liste des essais restants est dans la page [Avancement](TODO.html).

## Ce qui n'est pas encore disponible

- Une version stable de l'image, testée sur un vrai Raspberry Pi.
- L'accès aux services depuis internet et la réservation d'une adresse `*.pi-appliance.com`.
- Les sauvegardes en ligne ou sur un autre pi-appliance, la restauration de l'appliance entière ou d'un seul fichier, le VPN et les mises à jour de l'appliance depuis la console.

## Domaine personnel chez OVH {#domaine-personnel-chez-ovh}

> **Cette option n'est pas encore disponible.** La console ne permet pas de choisir un domaine personnel et ne fournit pas le nom vers lequel le faire pointer. Ne modifiez pas encore la zone DNS de votre domaine pour pi-appliance.

Quand cette option sera disponible, la configuration se fera en deux endroits :

### Chez OVH

1. Choisissez un sous-domaine de votre domaine, par exemple `ma-maison.exemple.fr`. C'est préférable au domaine seul (`exemple.fr`) : un alias DNS classique ne peut pas toujours être créé à la racine d'un domaine.
2. Dans l'espace client OVH, ouvrez la zone DNS de votre domaine et ajoutez une entrée de type **CNAME** pour ce sous-domaine. La cible sera le nom complet fourni par la console, de la forme `<nom>.pi-appliance.com`.
3. Ne remplacez pas les serveurs DNS du domaine et ne modifiez pas ses autres entrées, notamment celles utilisées pour le site web ou les e-mails. N'utilisez pas la fonction de redirection web d'OVH à la place de l'entrée DNS.
4. Si la console demande d'autres entrées pour les adresses des services, suivez les valeurs qu'elle affiche ; ne les devinez pas.

### Dans la console pi-appliance

La console devra permettre de saisir le sous-domaine choisi, d'afficher la cible à recopier chez OVH, puis de vérifier la configuration avant d'activer le domaine. Ces réglages et cette vérification ne sont pas encore présents.

**Prérequis :** l'appliance doit d'abord pouvoir obtenir et tenir à jour son adresse pi-appliance (`TODO-3.2` et `TODO-3.5`). La prise en charge du domaine dans la console, notamment pour l'utiliser avec les services et leurs certificats, reste aussi à réaliser. L'accès depuis internet n'est pas encore disponible non plus.

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
6. Dans l'assistant, créez le mot de passe administrateur et choisissez le Wi-Fi de la maison ou le réseau filaire. Le navigateur peut signaler le certificat local auto-signé : ne continuez que si vous êtes bien connecté au point d'accès de votre propre Pi. Une fois le réseau configuré, le Wi-Fi de configuration se ferme : reconnectez votre téléphone ou votre ordinateur au Wi-Fi de la maison, puis ouvrez `https://pi-appliance-XXXX.local/pi-appliance/` (`https://pi-appliance.local/pi-appliance/` avec l'image du 19 novembre 2026). Voir [Retrouver la console](#retrouver-la-console) si cette adresse ne s'ouvre pas.

7. De retour dans l'assistant, sur le réseau de la maison, cliquez sur **Tester le Wi-Fi** (ou **Tester le câble réseau**) : la console vérifie que le Pi est bien relié, que la box répond et qu'il se reconnectera tout seul. Si tout est en ordre, cliquez sur **Redémarrer le Pi** : la console affiche l'adresse où le retrouver et redémarre le Pi au bout de 10 secondes (bouton **Ne pas redémarrer** pour annuler). Une à deux minutes plus tard, la page revient d'elle-même sur l'écran de connexion.

8. L'assistant n'installe pas de service. Une fois connecté, faites défiler la page sous **Mes services** jusqu'à la rubrique **Marketplace**. Sélectionnez une application, choisissez son nom puis cliquez sur **Installer**. Une adresse locale comme `www-pi-appliance-XXXX.local` (le nom du service, un tiret, puis le nom du Pi) apparaît dans **Mes services** quand l'installation réussit ; elle s'ouvre depuis tous les appareils de la maison, ordinateurs Windows et téléphones Android compris. La première fois, le navigateur affiche pour cette adresse la même alerte que pour la console : vous pouvez continuer. Les services installés avec une version précédente, à une adresse comme `www.pi-appliance-XXXX.local`, passent d'eux-mêmes à la nouvelle adresse après la mise à jour de pi-appliance, et suivent aussi le nouveau nom du Pi si vous le changez : mettez à jour vos favoris. Pour un site web, les fichiers se déposent par **Nextcloud** : installez aussi Nextcloud depuis la marketplace ; quelques minutes après, un dossier **Site <nom-du-site>** apparaît dans Nextcloud pour son compte administrateur. Les fichiers placés dans ce dossier sont servis directement par le site.

### Ce qui se passe au premier démarrage {#premier-demarrage}

![Premier démarrage : flasher la carte, démarrer le Pi, se connecter au Wi-Fi pi-appliance-XXXX, ouvrir le portail, créer le compte, choisir le Wi-Fi de la maison, retrouver la console](premier-demarrage.svg)

1. **Flashage** : Raspberry Pi Imager écrit l'image sur la carte micro-SD, depuis votre ordinateur.
2. **Démarrage** : au premier démarrage, le Pi agrandit sa partition, se donne son nom `pi-appliance-XXXX` et crée ses clés ; comptez quelques minutes.
3. **Wi-Fi de configuration** : le Pi ouvre le Wi-Fi `pi-appliance-XXXX`, sans mot de passe, pendant 24 heures au plus. Il ne sert qu'à configurer le Pi : il ne donne accès ni à internet ni à votre réseau.
4. **Portail** : une fois connecté à ce Wi-Fi, le téléphone ouvre souvent le portail tout seul ; sinon, ouvrez `https://10.42.0.1/pi-appliance/`. Le navigateur affiche une alerte sur le certificat : vous pouvez continuer.
5. **Compte** : choisissez un identifiant (un nom court, par exemple `marie` : ce n'est pas l'adresse e-mail), votre adresse e-mail et un mot de passe d'au moins 12 caractères. L'assistant « Mise en route » s'ouvre ensuite ; il revient à chaque connexion tant qu'il n'est pas terminé.
6. **Wi-Fi de la maison** : choisissez le pays, puis votre Wi-Fi et son mot de passe (le bouton **Afficher** montre ce que vous tapez), ou le câble réseau s'il est branché sur la box. Avant de couper le Wi-Fi de configuration, la console affiche l'adresse où la retrouver ; avec le câble, elle donne aussi l'adresse IP du Pi.
7. **Retrouver la console** : reconnectez votre téléphone ou votre ordinateur au Wi-Fi de la maison et ouvrez l'adresse indiquée (ci-dessous), puis reconnectez-vous : l'assistant reprend.
8. **Vérification et redémarrage** : **Tester le Wi-Fi** vérifie la connexion sans rien couper ; si tout est en ordre, **Redémarrer le Pi** rappelle l'adresse du Pi, compte 10 secondes et redémarre le Pi, qui doit retrouver votre réseau tout seul. La mise en route est alors terminée ; vos services se choisissent ensuite dans la marketplace.

### Retrouver la console sur le réseau de la maison {#retrouver-la-console}

- Ouvrez `https://pi-appliance-XXXX.local/pi-appliance/`, où `pi-appliance-XXXX` est le nom du Wi-Fi de configuration de votre Pi (ou le nom que vous lui avez donné dans « Mon Pi »).
- Si un autre appareil de la maison porte déjà ce nom, le Pi en prend un autre en arrivant sur votre réseau (`pi-appliance-XXXX-2`, par exemple). Avec le câble, la console vous donne le nouveau nom avant la bascule ; avec le Wi-Fi, cherchez le Pi dans la liste des appareils de votre box.
- Si cette adresse ne s'ouvre pas (certains téléphones Android et ordinateurs Windows ne trouvent pas les adresses en `.local`), utilisez l'adresse IP du Pi : la console la donne avant la bascule si le Pi est relié par câble ; sinon, cherchez l'appareil `pi-appliance-XXXX` dans la liste des appareils connectés de votre box (son interface d'administration, souvent `http://192.168.1.1`). Ouvrez alors `https://<adresse du Pi>/pi-appliance/`, par exemple `https://192.168.1.127/pi-appliance/`. **N'oubliez pas `/pi-appliance/` à la fin.**
- Le navigateur affiche une alerte (« La connexion n'est pas privée », « Avertissement : risque probable de sécurité ») : le Pi protège la connexion avec son propre certificat, que le navigateur ne connaît pas encore. Choisissez « Paramètres avancés », puis « Continuer ».

La partition, le démarrage et le portail de cette image restent à valider sur du matériel réel. L'accès aux applications depuis internet n'est pas encore disponible.

## Mot de passe administrateur oublié : le fichier reset {#fichier-reset}

Si la console répond « identifiant ou mot de passe incorrect », vérifiez d'abord l'identifiant : c'est le nom choisi à la création du compte (par exemple `marie`), **pas l'adresse e-mail**. Si le mot de passe administrateur est vraiment perdu, le fichier `reset` permet d'en choisir un nouveau sans rien perdre : les services installés, leurs données et les autres comptes sont conservés.

1. Éteignez le Pi en débranchant son alimentation, puis retirez la carte micro-SD et insérez-la dans un ordinateur.
2. Ouvrez la partition de démarrage de la carte, qui apparaît comme un disque nommé **`bootfs`**. Si l'ordinateur propose de formater la carte ou de réparer d'autres partitions, refusez : seule `bootfs` est lisible sous Windows, et c'est la bonne.
3. Créez à la racine de `bootfs` un fichier vide nommé exactement **`reset`**, sans extension. Sous Windows, affichez d'abord les extensions (Explorateur de fichiers, **Affichage > Afficher > Extensions de noms de fichiers**) : un « nouveau document texte » renommé `reset` s'appelle en réalité `reset.txt` et ne marche pas. Sous macOS ou Linux, dans un terminal : `touch /Volumes/bootfs/reset` (macOS) ou `touch /media/$USER/bootfs/reset` (Linux).
4. Éjectez proprement la carte, remettez-la dans le Pi et rebranchez son alimentation.
5. Ouvrez la console : elle affiche « Le fichier reset a été trouvé sur la carte SD ». Choisissez le nouveau mot de passe du premier compte administrateur (12 caractères au moins), puis connectez-vous avec votre identifiant et ce mot de passe. Toutes les sessions ouvertes auparavant sont fermées. Si la double authentification était activée, le code vous est encore demandé.

Le Pi efface le fichier `reset` dès qu'il l'a lu, mais garde la demande jusqu'au choix du nouveau mot de passe : un redémarrage entre-temps ne la perd pas (images construites après le 20 novembre 2026 ; avec les précédentes, il faut redéposer le fichier).

## Remettre le Pi à zéro {#remise-a-zero}

Pour repartir de la mise en route, comme au premier démarrage (par exemple avant de donner le Pi à quelqu'un d'autre), ouvrez `https://<adresse du Pi>/pi-appliance/reset-to-factory`, connecté avec un compte administrateur. La page dit ce qui sera effacé (tous les comptes, la mise en route, le choix des statistiques anonymes, le réglage DuckDNS) et ce qui est gardé (le réseau de la maison, le nom du Pi, les sauvegardes, l'adresse pi-appliance.com). Les services installés et leurs données sont gardés, sauf si vous cochez « Effacer aussi les services installés et toutes leurs données ». Il faut redonner votre mot de passe (et le code de vérification si la double authentification est activée), puis cocher « J'ai compris ce qui sera effacé ».

Mot de passe perdu : déposez d'abord le fichier `reset` (ci-dessus) ; tant que le nouveau mot de passe n'est pas choisi, la page de remise à zéro s'ouvre aussi sans mot de passe.

## Diagnostic du Pi sans écran {#diagnostic}

Si un service ou la console se comporte mal, ouvrez `https://<adresse du Pi>/pi-appliance/diagnostic` (lien « Diagnostic du Pi » dans « Mon Pi »), connecté avec un compte administrateur. La page dit si chaque programme du Pi est en marche, si l'alimentation a été trop faible, la place sur la carte SD et les dernières erreurs. **Télécharger les journaux** crée un fichier texte à envoyer à la personne qui vous aide : les mots de passe et les adresses e-mail y sont masqués.

Sans compte administrateur (mot de passe perdu, compte pas encore créé) : déposez sur la partition `bootfs` de la carte SD un fichier vide nommé exactement **`diagnostic`**, comme le fichier `reset` (voir ci-dessus), puis remettez la carte et rebranchez le Pi. La page s'ouvre alors sans compte, depuis la maison, pendant 24 heures ; le bouton « Fermer l'accès sans compte » la referme plus tôt.

Si la console ne s'ouvre plus du tout (programme Traefik en panne), essayez `https://<nom du Pi>.local:8443/pi-appliance/diagnostic` : le Pi ouvre cette console de secours, qui ne montre que le diagnostic, quand Traefik ne répond plus depuis une minute et demie. Si rien ne répond, débranchez puis rebranchez l'alimentation du Pi.

Si vous avez choisi un compte et activé SSH dans les réglages de Raspberry Pi Imager, ils sont gardés : une personne à l'aise avec l'informatique peut aussi se connecter au Pi par SSH. Le Wi-Fi réglé dans Imager, lui, est ignoré : le Wi-Fi de la maison se choisit dans le portail.

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
- [Essais avec plusieurs Pi](essais-plusieurs-pi.html) : procédure technique pour essayer controlTower et d'autres Pi dans une maison, et des Pi dans d'autres maisons joints depuis internet.
- [Marketplace](marketplace.html) : catalogue actuel, applications envisagées et guide de contribution.
- [Spécification](specification.html) : fonctionnement visé par le projet.
- [Avancement](TODO.html) : fonctions réalisées et essais restants.
- [Code source](https://github.com/gmoulard/pi-appliance) : dépôt et workflows.
