# Essais avec plusieurs Pi, dans une maison et dans plusieurs

Ce guide décrit, commande par commande, les essais sur du vrai matériel des points TODO-2.7, TODO-2.48, TODO-3.2, TODO-3.8 et TODO-4.6 :

- **une maison** : plusieurs Pi pi-appliance sur le même réseau ;
- **plusieurs maisons** : des Pi chez d'autres personnes, joints depuis internet.

Il s'adresse à la personne qui fait les essais, pas au grand public : il utilise la ligne de commande du Pi. Les résultats sont à reporter dans [l'avancement](TODO.md), point par point.

## 1. Ce qui peut être essayé, et ce qui ne peut pas encore l'être

| Sujet | État du code | Essai possible |
|-------|--------------|----------------|
| Nom propre à chaque Pi (`pi-appliance-XXXX`, Wi-Fi, `<nom>.local`) | Écrit | Oui |
| Renommer un Pi (par exemple en `salon`) depuis la console | Écrit | Oui |
| Wi-Fi de configuration sans accès à internet ni à la maison | Écrit (pare-feu `ap-firewall.nft`) | Oui |
| Plusieurs Pi allumés en même temps dans la maison | Écrit | Oui |
| Console fermée depuis internet (réseaux privés seulement) | Écrit (`console.yml`) | Oui |
| DuckDNS tenu à jour par le Pi | Écrit | Oui, avec un compte DuckDNS |
| Services sur internet avec un certificat Let's Encrypt | Écrit, mais adresse des services à régler à la main (section 6.3) | Oui, avec une redirection de ports sur la box |
| VPN WireGuard | Écrit | Oui, avec une redirection du port UDP 51820 |
| Adresse `<nom>.pi-appliance.com` (service d'adresses) | Pas écrit (TODO-3.5) | Non |
| Relais pour les box sans accès direct (CGNAT, 4G) | Pas écrit (TODO-3.9) | Non |
| UPnP, guide de la box dans la console | Pas écrit (TODO-3.8) | Non : la redirection se fait à la main |
| Services du projet (e-mails, statistiques) | Pas installés (TODO-4.4) | Non |

Chaque Pi pi-appliance est indépendant (spécification, section 4) : il a sa propre console et **ne pilote pas** les autres Pi, ni dans sa maison ni ailleurs. Rien n'est donc à essayer « entre » deux Pi, sauf qu'ils ne se gênent pas.

## 2. Matériel et préparation

### 2.1 Matériel

- Au moins **deux Pi** pour la maison du concepteur : l'un sera renommé `salon`, l'autre garde son nom `pi-appliance-XXXX`. Un troisième Pi permet de tester un modèle de plus (Pi 3 B+ ou Zero 2 W).
- **Un Pi par autre maison**, avec une personne sur place qui peut le brancher et accéder à sa box.
- Une carte micro-SD d'au moins 16 Go par Pi.
- Pour les essais : un écran HDMI et un clavier USB (pour ouvrir un terminal sur le Pi, section 2.4), un ordinateur et un téléphone.
- Pour les essais depuis internet : un téléphone en **4G/5G, Wi-Fi coupé**. C'est le moyen le plus simple de venir « de l'extérieur ».

### 2.2 Construire une image récente

**L'image du 19 novembre 2026 (`48c047d`) ne convient pas** : tous les Pi y portent le même nom (`pi-appliance`), et son Wi-Fi de configuration partage la connexion du Pi. C'est la cause probable de l'incident du 2026-11-19 (réseau saturé, alertes de Google). Il faut une image construite depuis `master` après le commit `bed9884`.

Lancer le workflow « Image » depuis le poste de développement :

```powershell
gh workflow run image.yml --ref master
gh run list --workflow image.yml --limit 1
```

Attendre la fin (environ 20 minutes), puis télécharger l'artefact `pi-appliance-image` :

```powershell
gh run download <numéro du run> --name pi-appliance-image --dir image-essais
```

Vérifier la somme de contrôle, décompresser et flasher comme dans le [guide utilisateur](guide-utilisateur.md#telecharger-limage-experimentale). Noter le commit de l'image (dans le nom du fichier) : il figurera dans les résultats.

**Une même image pour toutes les cartes.** Chaque Pi doit quand même avoir un nom, un certificat et une clé différents : c'est l'un des points vérifiés.

### 2.3 Règle de prudence pour le premier démarrage

Tant que l'essai A (section 3) n'a pas réussi sur un premier Pi :

- n'allumer **qu'un seul Pi à la fois** ;
- laisser le **câble Ethernet débranché** jusqu'au choix du réseau dans le portail.

Une fois l'essai A réussi, ces précautions ne sont plus nécessaires : l'essai B les lève justement.

### 2.4 Ouvrir un terminal sur le Pi

L'image n'a ni SSH ni mot de passe : le compte `pi` est verrouillé. Pour les essais :

1. Brancher un écran et un clavier **avant** le premier démarrage. Raspberry Pi OS propose alors de créer un compte (clavier, nom, mot de passe). Choisir par exemple `essai` et un mot de passe propre à ce Pi. Si l'écran ne propose pas de créer de compte, le noter dans les résultats (TODO-2.7) : il n'y a alors aucun moyen d'ouvrir un terminal, et seuls les essais faits depuis l'ordinateur et le téléphone restent possibles.
2. Se connecter sur l'écran. Pour travailler depuis l'ordinateur, ouvrir SSH le temps des essais :

   ```shell
   sudo systemctl enable --now ssh
   ```

   puis, depuis l'ordinateur, une fois le Pi sur le réseau de la maison :

   ```powershell
   ssh essai@pi-appliance-XXXX.local
   ```

3. **À la fin des essais**, refermer SSH : `sudo systemctl disable --now ssh`. Une appliance livrée au grand public n'a pas SSH.

Dans la suite, les commandes `shell` se tapent sur le Pi, et les commandes `powershell` sur l'ordinateur Windows.

### 2.5 Fiche de chaque Pi

Avant de mélanger les Pi, relever pour chacun (étiquette sur le boîtier) :

```shell
cat /var/lib/pi-appliance/system/name        # nom du Pi (pi-appliance-XXXX)
cat /proc/device-tree/model; echo            # modèle
ip -br link | grep -E 'wlan0|eth0'           # adresses matérielles (les 4 derniers chiffres font le XXXX)
ip -br addr                                  # adresses IP
openssl x509 -in /var/lib/pi-appliance/system/tls/local.crt -noout -fingerprint -sha256 -ext subjectAltName
sha256sum /var/lib/pi-appliance/system/device.pub
```

| Pi | Modèle | Nom | MAC Wi-Fi | IP | Empreinte du certificat | Empreinte de la clé | Maison |
|----|--------|-----|-----------|----|-------------------------|---------------------|--------|
| 1 | | | | | | | concepteur |
| 2 | | | | | | | concepteur |
| 3 | | | | | | | autre maison |

**Attendu** : noms, empreintes du certificat et empreintes de la clé **tous différents** d'un Pi à l'autre ; le certificat contient `DNS:<nom>.local`.

## 3. Essai A : un Pi seul, le Wi-Fi de configuration

But : vérifier que le Wi-Fi de configuration n'ouvre que le portail, sans accès à internet ni à la maison (TODO-2.7, TODO-2.48).

1. Démarrer le Pi, câble débranché. Sur le Pi :

   ```shell
   systemctl is-active pi-appliance-secrets pi-appliance-firewall pi-appliance-ap pi-appliance-traefik pi-appliance
   cat /run/pi-appliance/ap-status; echo
   nmcli -f NAME,DEVICE,TYPE connection show --active
   sudo nft list table inet pi_appliance_ap
   ```

   **Attendu** : les cinq services `active` ; le point d'accès `pi-appliance-ap` actif sur `wlan0` ; la table `pi_appliance_ap` présente avec deux règles `drop` (10.42.0.0/24 dans les deux sens).

2. Sur l'ordinateur, chercher les Wi-Fi : **un seul** Wi-Fi `pi-appliance-XXXX`, au nom de la fiche. Aucun Wi-Fi `pi-appliance` sans suffixe.

3. Brancher le câble Ethernet du Pi sur la box (le point d'accès doit rester ouvert : le réseau n'a pas encore été choisi). Connecter l'ordinateur au Wi-Fi `pi-appliance-XXXX`, puis :

   ```powershell
   ipconfig | Select-String "10.42.0"
   curl.exe -k https://10.42.0.1/pi-appliance/api/health
   ping -n 3 8.8.8.8
   ping -n 3 <adresse IP de la box, par exemple 192.168.1.1>
   curl.exe -m 5 -I http://example.com/
   ```

   **Attendu** :
   - une adresse en `10.42.0.x` ;
   - `api/health` répond `{"status":"ok",…}` ;
   - **les deux `ping` échouent** (délai dépassé) : ni internet ni la box ;
   - `http://example.com/` renvoie une redirection (`302` ou `307`) vers `https://10.42.0.1/pi-appliance/` : c'est le portail captif.

4. Sur le Pi, les compteurs des règles `drop` ont augmenté : `sudo nft list table inet pi_appliance_ap`.

5. Avec un téléphone (iPhone et Android) : se connecter au Wi-Fi du Pi. La page de configuration doit s'ouvrir d'elle-même. Ne pas encore choisir de réseau.

6. Choisir le réseau dans le portail (Wi-Fi de la maison ou câble), créer le compte administrateur. **Attendu** : le Wi-Fi `pi-appliance-XXXX` disparaît dans la minute ; sur le Pi, `cat /run/pi-appliance/ap-status` ne dit plus qu'il est ouvert.

7. Depuis l'ordinateur revenu sur le réseau de la maison :

   ```powershell
   ping -n 2 pi-appliance-XXXX.local
   curl.exe -k https://pi-appliance-XXXX.local/pi-appliance/api/health
   ```

   Ouvrir `https://pi-appliance-XXXX.local/pi-appliance/` dans le navigateur. Le certificat affiché doit avoir l'empreinte de la fiche.

**Si un `ping` de l'étape 3 réussit**, arrêter tout de suite : le Wi-Fi de configuration donne accès au réseau. Débrancher le Pi et relever `sudo nft list ruleset` et `journalctl -u pi-appliance-firewall -b`.

## 4. Essai B : plusieurs Pi dans la même maison

But : plusieurs Pi allumés ensemble, chacun joignable à son nom, sans conflit (TODO-2.48).

### 4.1 Renommer un Pi

1. Sur le Pi 1 configuré à l'essai A, ouvrir la console, section « Mon Pi », « Nom du Pi sur le réseau », saisir `salon`, puis « Changer le nom ».
2. Attendre une dizaine de secondes, puis ouvrir `https://salon.local/pi-appliance/`. Le navigateur affiche une nouvelle alerte : le certificat a été refait pour le nouveau nom.
3. Sur le Pi :

   ```shell
   hostname
   cat /var/lib/pi-appliance/system/name
   grep 127.0.1.1 /etc/hosts
   grep '^ssid=' /etc/NetworkManager/system-connections/pi-appliance-ap.nmconnection
   openssl x509 -in /var/lib/pi-appliance/system/tls/local.crt -noout -ext subjectAltName
   ```

   **Attendu** : `salon` partout ; le certificat contient `DNS:salon.local`.

4. Redémarrer le Pi (`sudo reboot`) : il doit garder le nom `salon`.
5. Essayer un nom refusé dans la console (`Salon`, `-abc`, un nom de 33 caractères) : message d'erreur, nom inchangé.

**Point connu à vérifier** : l'adresse des nouveaux services est calculée au démarrage de la console (`<nom>.local`), et le changement de nom ne relance que avahi et Traefik. Après le renommage, installer un service et regarder son adresse dans « Mes services ». Si elle porte encore l'ancien nom, relancer la console (`sudo systemctl restart pi-appliance`) et le noter dans les résultats.

### 4.2 Allumer les autres Pi en même temps

1. Démarrer le Pi 2, et le Pi 3 s'il y en a un, **en même temps**, câbles branchés sur la box.
2. Sur l'ordinateur : chaque Pi a son propre Wi-Fi `pi-appliance-XXXX`, aux noms de la fiche, et aucun Wi-Fi ne porte deux fois le même nom.
3. Configurer chacun par son portail (essai A, étapes 5 à 7).
4. Pendant ce temps, surveiller la maison : la connexion internet des autres appareils reste normale, et Google n'envoie pas d'alerte. Sur la box, la liste des appareils montre un nom et une adresse IP par Pi.

### 4.3 Chaque console à son nom

Depuis l'ordinateur, sur le réseau de la maison :

```powershell
foreach ($n in "salon", "pi-appliance-XXXX", "pi-appliance-YYYY") {
  "$n : " + (curl.exe -sk "https://$n.local/pi-appliance/api/health")
  ping -n 1 "$n.local" | Select-String "Réponse|Reply"
}
```

Sur l'un des Pi, voir ce qu'avahi annonce :

```shell
sudo apt-get install -y avahi-utils   # une fois, pour l'essai
avahi-browse -art | grep -E 'salon|pi-appliance-'
```

**Attendu** :
- chaque nom répond avec sa propre adresse IP (celle de la fiche) ;
- chaque console a son propre compte administrateur : le mot de passe de l'une ne marche pas sur l'autre ;
- aucun nom en `-2.local` (avahi en ajoute un quand deux Pi annoncent le même nom) ;
- dans le journal d'avahi, pas de conflit de nom : `journalctl -u avahi-daemon -b | grep -i conflict` ne renvoie rien.

### 4.4 Les services de chaque Pi

1. Installer un « Site web » sur le Pi `salon` et un autre sur le Pi 2, chacun sous le nom `www`.
2. Les adresses des services sont `www-salon.local` et `www-pi-appliance-XXXX.local` (un seul niveau, TODO-2.61), annoncées par `mdns.sh` : elles doivent s'ouvrir sans toucher au fichier `hosts`, depuis Windows comme depuis un téléphone. Si l'une ne s'ouvre pas, noter l'appareil et le système dans les résultats.
3. Ouvrir les deux sites : chacun affiche son propre contenu, et non celui de l'autre Pi ; le certificat présenté (cadenas du navigateur) porte l'adresse du service.

### 4.5 Après un redémarrage de la box

Redémarrer la box, attendre qu'elle revienne, puis refaire la section 4.3. **Attendu** : les Pi reviennent sur le réseau tout seuls (même si leur adresse IP change) ; le Wi-Fi de configuration se rouvre au bout de 10 minutes sans réseau, puis se referme quand le réseau revient.

## 5. Essai C : la console n'est jamais ouverte sur internet

Cet essai se fait dans la maison du concepteur, avant de mettre un Pi sur internet ailleurs. Il demande la redirection des ports de la section 6.2.

Depuis le téléphone en 4G (Wi-Fi coupé), ou depuis une machine hors de la maison :

```shell
curl -sk -o /dev/null -w '%{http_code}\n' https://<identifiant>.duckdns.org/pi-appliance/
curl -sk -o /dev/null -w '%{http_code}\n' https://<identifiant>.duckdns.org/pi-appliance/api/health
```

**Attendu** : `404` pour les deux. La console ne répond qu'aux adresses des réseaux privés (`console.yml`). Depuis la maison, la même adresse donne bien la console (si la box fait le « NAT loopback »).

## 6. Essai D : un Pi dans une autre maison, joint depuis internet

But : DuckDNS, accès direct par la box, certificat Let's Encrypt, VPN (TODO-3.2, TODO-3.8, TODO-4.6). Il n'y a pas encore d'adresse `<nom>.pi-appliance.com` ni de relais : l'essai passe par un nom DuckDNS.

### 6.1 Vérifier que la box permet l'accès direct

Sur le Pi installé dans l'autre maison (essai A fait sur place) :

```shell
curl -s https://api.ipify.org; echo
```

Comparer avec l'adresse « WAN » ou « internet » affichée dans l'interface de la box :

- **identiques** : accès direct possible, continuer ;
- **différentes**, ou adresse de la box en `100.64.x.x` à `100.127.x.x` : la box est derrière un CGNAT. L'accès direct est impossible et le relais n'existe pas encore : noter « CGNAT » dans les résultats, et faire seulement les sections 6.4 (DuckDNS) et 4.3 dans cette maison.

### 6.2 Réserver l'adresse du Pi et ouvrir les ports

1. Dans la box : donner au Pi une **adresse IP fixe** (bail DHCP réservé, par son adresse matérielle).
2. Rediriger vers cette adresse : **TCP 80** et **TCP 443** ; **UDP 51820** seulement pour l'essai du VPN (section 6.6).
3. Ne rediriger aucun autre port (pas de SSH : port 22 fermé côté internet).

**Pendant tout l'essai, n'installer que des services de test** (un site web avec une page factice). Ils sont joignables par tout internet. À la fin, refermer les redirections (section 6.7).

### 6.3 Donner au Pi son adresse sur internet

1. Créer un compte sur [duckdns.org](https://www.duckdns.org), un sous-domaine (par exemple `essai-maison2`) et relever le jeton. Le jeton est un secret : ne jamais l'écrire dans le dépôt, un ticket ou un message.
2. Dans la console du Pi, écran « Adresse sur internet » : saisir l'identifiant et le jeton, enregistrer.
3. Les services prennent comme adresse le nom du Pi en `.local`. Pour qu'ils prennent l'adresse DuckDNS, le temps de l'essai, régler la console à la main (pas encore d'écran pour cela, TODO-3.5) :

   ```shell
   sudo systemctl edit pi-appliance.service
   ```

   Écrire entre les lignes de commentaire :

   ```ini
   [Service]
   ExecStart=
   ExecStart=/usr/local/bin/pi-appliance -addr 127.0.0.1:8080 -data /var/lib/pi-appliance -reset-file /boot/firmware/reset -domain essai-maison2.duckdns.org
   ```

   puis `sudo systemctl restart pi-appliance`. À la fin de l'essai : `sudo systemctl revert pi-appliance.service && sudo systemctl restart pi-appliance`.

### 6.4 DuckDNS tenu à jour

Sur le Pi :

```shell
journalctl -u pi-appliance -b | grep -i duckdns
getent hosts essai-maison2.duckdns.org
curl -s https://api.ipify.org; echo
```

**Attendu** : le journal dit `DuckDNS : essai-maison2.duckdns.org pointe vers <adresse>`, sans ligne d'erreur `DuckDNS : … (nouvel essai dans …)` ; le nom pointe vers l'adresse publique de la box. Le jeton n'apparaît **jamais** dans le journal : `journalctl -u pi-appliance | grep -c <début du jeton>` doit donner `0`. L'écran « Adresse sur internet » affiche la date de la dernière mise à jour. Si la box change d'adresse (redémarrage de la box chez certains fournisseurs), le nom suit en moins de 10 minutes.

### 6.5 Un service sur internet avec un vrai certificat

1. Installer un « Site web » nommé `www`. Son adresse est `www.essai-maison2.duckdns.org` (DuckDNS fait pointer les sous-noms vers la même adresse).
2. Depuis le téléphone en 4G, ouvrir `https://www.essai-maison2.duckdns.org/` : pas d'alerte de certificat. Le premier accès peut prendre une minute, le temps que Traefik obtienne le certificat Let's Encrypt (défi par le port 80).
3. Vérifier le certificat depuis une machine extérieure :

   ```shell
   echo | openssl s_client -connect www.essai-maison2.duckdns.org:443 -servername www.essai-maison2.duckdns.org 2>/dev/null | openssl x509 -noout -issuer -dates
   ```

   **Attendu** : émetteur Let's Encrypt.
4. En cas d'échec, sur le Pi : `sudo docker logs pi-appliance-traefik 2>&1 | grep -i -E 'acme|certificate' | tail -20`, et vérifier que le port 80 est bien redirigé.
5. Refaire l'essai C (section 5) avec ce nom : la console reste en `404` depuis internet.

### 6.6 VPN WireGuard

1. Rediriger le port **UDP 51820** vers le Pi (section 6.2).
2. Dans la marketplace, installer « VPN WireGuard », ajouter un appareil, et photographier le QR code avec l'application WireGuard du téléphone. Le QR code n'est montré qu'une fois.
3. Téléphone en 4G, VPN activé : ouvrir `https://<IP du Pi dans la maison>/pi-appliance/`. **Attendu** : la console s'ouvre (le téléphone est « dans la maison »). VPN coupé : la même adresse ne répond pas.
4. VPN activé, vérifier que seul le réseau de la maison passe par le VPN : un site internet quelconque s'ouvre normalement, et `https://api.ipify.org` sur le téléphone donne l'adresse de l'opérateur mobile, pas celle de la box.

### 6.7 Fin de l'essai dans l'autre maison

1. Désinstaller les services de test.
2. Refermer les redirections de ports dans la box (80, 443, 51820).
3. Retirer le réglage de la section 6.3 (`systemctl revert`).
4. Effacer les réglages DuckDNS dans la console si le Pi ne reste pas en essai, et supprimer le sous-domaine sur duckdns.org.
5. Refermer SSH (`sudo systemctl disable --now ssh`).

## 7. Plusieurs maisons en même temps

Avec un Pi chez le concepteur (`salon`) et un Pi dans chaque autre maison :

- Les noms en `.local` ne valent que dans leur maison : deux Pi de deux maisons peuvent porter le même nom sans gêne. **À vérifier** : un Pi d'une autre maison nommé lui aussi `salon`, joint par son adresse DuckDNS, n'a aucun effet sur celui du concepteur.
- Chaque Pi a son propre nom DuckDNS : jamais deux Pi sur le même sous-domaine.
- Depuis la maison du concepteur, ouvrir `https://www.<sous-domaine de chaque autre maison>.duckdns.org/` : chaque site affiche son propre contenu.
- Le Pi du concepteur n'a aucun accès aux autres Pi : aucune console d'une autre maison n'est joignable depuis la maison du concepteur (essai C sur chaque nom DuckDNS).

## 8. Résultats à reporter

Pour chaque essai : date, commit de l'image, modèle de Pi, maison, résultat (réussi / échoué / non fait) et, en cas d'échec, la sortie des commandes. Les reporter dans [l'avancement](TODO.md) :

| Essai | Point du TODO |
|-------|---------------|
| A : Wi-Fi de configuration, portail, secrets différents | TODO-2.7 |
| B : noms uniques, Pi renommé, plusieurs Pi allumés, consoles et services séparés | TODO-2.48 |
| C : console fermée depuis internet | TODO-3.11 |
| D 6.1 à 6.5 : DuckDNS, accès direct, Let's Encrypt | TODO-3.2, TODO-3.8 |
| D 6.6 : VPN | TODO-4.6 |
| Box en CGNAT | TODO-3.9 (relais) |

Pour récupérer les journaux d'un Pi en cas de problème :

```shell
journalctl -b -u pi-appliance-secrets -u pi-appliance-firewall -u pi-appliance-ap \
  -u pi-appliance-traefik -u pi-appliance -u avahi-daemon -u NetworkManager > ~/journal-essai.txt
```

Relire ce fichier avant de le partager : il peut contenir des noms de Wi-Fi et des adresses de la maison.
