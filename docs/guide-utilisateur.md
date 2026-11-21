# Guide de pi-appliance

pi-appliance, ce sont quatre parties qui portent le même nom :

- **l'image pi-appliance**, à copier sur la carte SD : le système du Pi, prêt à l'emploi ;
- **le socle pi-appliance**, toujours installé sur le Pi, et sa **console** : la page web d'où l'on règle le Pi et installe ses services ;
- **l'application mobile**, [app.pi-appliance.com](https://app.pi-appliance.com) : ce guide lisible sans réseau, et un raccourci vers vos Pi ;
- **la commande `pia`**, pour les personnes à l'aise avec la ligne de commande ([voir plus bas](#ligne-de-commande)). Elle n'est pas nécessaire : tout se fait depuis la console.

> **État du projet : expérimental.** L'image système est validée sur Raspberry Pi 3 B+ et Raspberry Pi 4 ; le Pi 5 et le Pi Zero 2 W restent à essayer. Ce n'est pas encore une version stable : gardez une copie de vos données importantes ailleurs.

## Ce que contient le prototype

- Une console en français avec création du compte administrateur, connexion et assistant de première mise en route (réseau, test du Wi-Fi, redémarrage).
- La configuration du réseau filaire ou du Wi-Fi depuis la console, ainsi qu'un point d'accès Wi-Fi de configuration au premier démarrage de l'image.
- Un tableau de bord avec l'état du Pi, des graphiques et des alertes de mémoire et de stockage.
- Une marketplace avec les applications installables du catalogue.
- L'installation de plusieurs instances, les commandes de démarrage et d'arrêt, la mise à jour des applications avec retour arrière, et le choix de conserver ou supprimer leurs données à la désinstallation.
- La gestion d'un disque USB pour les données des services.
- Les sauvegardes chiffrées sur un second disque USB, chaque nuit et à la demande, et la restauration d'un service à une date choisie (écran « Sauvegardes » dans « Mon Pi »), pas encore essayées sur un vrai Pi.
- Un code à scanner avec l'[application mobile](#application-mobile), dans « Mon Pi », pour retrouver le Pi et ses services sur le téléphone.

Ces fonctions sont présentes dans le code, mais restent à valider sur du matériel réel. La liste des essais restants est dans la page [Avancement](TODO.html).

## Ce qui n'est pas encore disponible

- Une version stable de l'image, et des essais sur Pi 5 et Pi Zero 2 W.
- L'accès aux services depuis internet et la réservation d'une adresse `*.pi-appliance.com`.
- Les sauvegardes en ligne ou sur un autre pi-appliance et la restauration de l'appliance entière ou d'un seul fichier.

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

## Télécharger et installer l'image {#telecharger-limage-experimentale}

L'image est publiée avec chaque version de pi-appliance, sur la page [dernière version](https://github.com/gmoulard/pi-appliance/releases/latest) : le fichier **`pi-appliance-vX.Y.Z.img.xz`** (environ 720 Mio compressé, 3,3 Gio une fois décompressé) et son fichier de somme de contrôle **`pi-appliance-vX.Y.Z.img.xz.sha256`**, où `X.Y.Z` est le numéro de version (par exemple `0.1.6`). Elle est validée sur Raspberry Pi 3 B+ et Raspberry Pi 4. Un Pi déjà installé n'a pas besoin d'être reflashé : il se met à jour depuis la console (« Mon Pi », « Mises à jour »).

L'image contient le socle pi-appliance et sa console : rien d'autre à installer. Seule la personne qui configure le Pi, avec le compte administrateur créé au premier démarrage, peut l'administrer. Chaque Pi a son propre nom, `pi-appliance-XXXX`, modifiable dans la console (section « Réseau de la maison », « Nom du Pi sur le réseau ») : un Pi nommé `salon` s'ouvre à `https://salon.local/pi-appliance/`.

L'image est destinée aux Raspberry Pi 5, 4, 3 B+ et Zero 2 W. Il faut une carte micro-SD de 16 Go ou plus, un ordinateur et [Raspberry Pi Imager](https://www.raspberrypi.com/software/). Le flashage efface entièrement la carte SD.

1. Sur la page de la [dernière version](https://github.com/gmoulard/pi-appliance/releases/latest), téléchargez dans le même dossier le fichier `.img.xz` et son fichier `.img.xz.sha256`. L'archive `-arm64.tar.gz` sert aux mises à jour depuis la console : inutile de la télécharger.
2. **Vérifiez le téléchargement avant de flasher.** Sous Windows PowerShell, depuis le dossier décompressé :

   ```powershell
   $image = ".\pi-appliance-v0.1.6.img.xz"   # remplacez par le nom de votre fichier
   $expected = (Get-Content "$image.sha256").Split()[0]
   $actual = (Get-FileHash $image -Algorithm SHA256).Hash
   if ($actual -ne $expected) { throw "Somme SHA-256 invalide : ne flashez pas cette image." }
   ```

   Sous Linux ou macOS, dans ce même dossier :

   ```shell
   sha256sum -c pi-appliance-v*.img.xz.sha256
   ```

   Ne flashez pas l'image si la vérification échoue.
3. Décompressez le fichier `.img.xz` avec 7-Zip pour obtenir le fichier `.img`.
4. Téléchargez et ouvrez [Raspberry Pi Imager](https://www.raspberrypi.com/software/). Choisissez le modèle de Pi, puis **Choisir l'OS > Utiliser une image personnalisée** et sélectionnez le `.img`. Choisissez la carte SD, puis lancez l'écriture. Confirmez l'effacement demandé par Imager.
5. Insérez la carte dans le Raspberry Pi et démarrez-le. Au premier démarrage, le Pi ouvre un réseau Wi-Fi de configuration **`pi-appliance-XXXX`** (quatre caractères qui changent d'un Pi à l'autre), sans mot de passe, pour une durée maximale de 24 heures. Il ne donne accès ni à internet ni à votre réseau : seulement au portail. Connectez-y votre téléphone ou ordinateur, puis ouvrez `https://10.42.0.1/pi-appliance/` si le portail ne s'affiche pas automatiquement.
6. Dans l'assistant, créez le mot de passe administrateur et choisissez le Wi-Fi de la maison ou le réseau filaire. Le navigateur peut signaler le certificat local auto-signé : ne continuez que si vous êtes bien connecté au point d'accès de votre propre Pi. Une fois le réseau configuré, le Wi-Fi de configuration se ferme : reconnectez votre téléphone ou votre ordinateur au Wi-Fi de la maison, puis ouvrez `https://pi-appliance-XXXX.local/pi-appliance/`. Voir [Retrouver la console](#retrouver-la-console) si cette adresse ne s'ouvre pas.

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

La partition, le premier démarrage, le Wi-Fi de configuration et la mise à jour depuis la console sont validés sur Raspberry Pi 3 B+ et Raspberry Pi 4 ; l'ouverture automatique du portail sur téléphone reste à essayer. L'accès aux applications depuis internet n'est pas encore disponible.

### Retrouver son Pi sur son téléphone : l'application mobile {#application-mobile}

L'[application mobile](https://app.pi-appliance.com) garde la liste de vos Pi et de leurs services sur le téléphone, avec ce guide lisible sans réseau.

1. Sur le téléphone, ouvrez `https://app.pi-appliance.com` et ajoutez l'application à l'écran d'accueil.
2. Dans la console, ouvrez « Mon Pi » : la carte « Application mobile » montre un code à scanner (seulement pour l'administrateur).
3. Dans l'application, touchez « Scanner le code » et visez ce code. Vous pouvez aussi copier le lien de la carte et l'ouvrir sur le téléphone, par exemple en vous l'envoyant par message.

Scanner le code depuis l'application **appaire** aussi le téléphone à votre compte : ensuite, l'application ouvre la console sans mot de passe, à la maison. Le code change toutes les quelques minutes et ne sert qu'une fois : ne le montrez qu'à vos propres téléphones. Le lien à copier, lui, n'appaire pas le téléphone. Les téléphones appairés sont listés dans « Mon compte », rubrique « Appareils » : retirez-y un téléphone perdu ou que vous n'utilisez plus. Depuis le téléphone lui-même, « Désappairer » dans l'application fait la même chose.

Le code et le lien ne contiennent aucun mot de passe : seulement le nom du Pi, ses adresses et celles de ses services (et, pour le code, le code d'appairage de quelques minutes). Pour lire l'état du Pi, l'application demande au Pi son nom, sa version et la liste de ses services ; n'importe qui qui connaît l'adresse du Pi peut lire ces informations, rien d'autre.

## Administrer le Pi depuis internet {#console-sur-internet}

La console est fermée sur internet tant que vous ne l'ouvrez pas. Pour l'ouvrir :

1. À la maison, dans « Mon compte », activez la **double authentification** : depuis internet, seuls les comptes qui l'ont activée peuvent se connecter.
2. Donnez une adresse internet au Pi, dans « Adresse sur internet » (DuckDNS, puis votre adresse pi-appliance.com).
3. Dans votre box, faites suivre les ports **80** et **443** vers le Pi : c'est ce qui permet d'obtenir le certificat de sécurité de l'adresse.
4. Dans « Adresse sur internet », carte « Console sur internet », touchez **Ouvrir la console sur internet**. La carte donne l'adresse à utiliser, par exemple `https://monpi.pi-appliance.com/pi-appliance/`.

Depuis internet, la console demande votre mot de passe puis le code de votre application d'authentification. Certaines actions restent possibles seulement à la maison : remettre le Pi à zéro, réinitialiser le mot de passe par le fichier `reset`, appairer un téléphone. Vous pouvez refermer la console à tout moment, même depuis internet.

## Ligne de commande du Pi (SSH) {#ligne-de-commande}

Pour les personnes à l'aise avec SSH, le Pi a une commande `pia` (nom long : `pi-appliance`). Tout ce qu'elle fait se fait aussi dans la console. Connectez-vous au Pi (`ssh <compte>@<nom-du-pi>.local`), puis :

```sh
pia aide                             # toutes les commandes
pia version                          # version du socle : pi-appliance, système, Docker, Traefik
sudo pia etat                        # la console répond-elle, état des services
sudo pia catalogue                   # applications installables
sudo pia catalogue nextcloud         # détail et paramètres d'une application
sudo pia installer nextcloud         # installer (--nom, --acces, --param nom=valeur)
sudo pia services                    # services installés
sudo pia redemarrer cloud            # demarrer, arreter, redemarrer
sudo pia desinstaller cloud          # données gardées ; --effacer-donnees pour les effacer
pia ssh                              # SSH : en marche ? mots de passe ? clés ?
sudo pia ssh cle ajouter "ssh-ed25519 AAAA… moi@portable"
sudo pia ssh mot-de-passe desactiver # clé SSH seulement (refusé tant qu'il n'y a pas de clé)
```

Seules `version`, `aide` et `ssh` (pour regarder) se lancent sans `sudo` : les autres passent par la console ou changent le système. Après avoir coupé les mots de passe, gardez votre session ouverte et vérifiez dans un autre terminal que votre clé marche.

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

- [Installer le socle sans l'image](installation.html) : Docker, Traefik, console et services systemd sur un Raspberry Pi OS existant.
- [Essais avec plusieurs Pi](essais-plusieurs-pi.html) : procédure technique pour essayer plusieurs Pi dans une maison, et des Pi dans d'autres maisons joints depuis internet.
- [Marketplace](marketplace.html) : catalogue actuel, applications envisagées et guide de contribution.
- [Spécification](specification.html) : fonctionnement visé par le projet.
- [Avancement](TODO.html) : fonctions réalisées et essais restants.
- [Code source](https://github.com/gmoulard/pi-appliance) : dépôt et workflows.
