# Marketplace pi-appliance

La marketplace regroupe les applications que l'on peut installer et gérer depuis la console pi-appliance. Le catalogue est encore intégré au programme : proposer une application ne la rendra disponible qu'après validation de la contribution et publication d'une nouvelle version de la console.

> **Version expérimentale.** L'image est validée sur Raspberry Pi 3 B+ et Raspberry Pi 4 ; chaque application reste à essayer une à une sur un vrai Pi. L'accès aux services depuis internet n'est pas encore disponible.

## Le service de contrôle controlTower

**controlTower** désigne la console d'administration pi-appliance et les composants qui la font fonctionner. Ce service système est fourni avec l'image, pas proposé dans le catalogue : il est réservé au concepteur ou à la personne responsable de l'appliance, au moyen du compte administrateur créé au premier démarrage. Les opérations de gestion nécessitent cette session administrateur.

## Applications déjà dans le catalogue

<div id="marketplace-catalog" data-catalog-index="{{ '/static/pi-appliance-catalog.json' | relative_url }}">
<p id="marketplace-catalog-status" role="status">Chargement du catalogue…</p>
<div id="marketplace-tags" class="todo-filters" role="group" aria-label="Filtrer par étiquette" hidden></div>
<table id="marketplace-catalog-table" hidden>
<thead><tr><th>Application</th><th>Description</th><th>Catégorie</th><th>Étiquettes</th><th>Mémoire</th><th>Disque</th></tr></thead>
<tbody></tbody>
</table>
</div>

Cette liste est chargée depuis le catalogue JSON généré à partir des manifestes de [`marketplace/`](https://github.com/gmoulard/pi-appliance/tree/master/marketplace). Les consoles lisent ce même fichier dans le dépôt GitHub : une application acceptée apparaît sur les Pi sans nouvelle version de pi-appliance. Après toute modification d'un manifeste ou de `marketplace/planned.yaml`, régénérez le fichier avec `go run ./cmd/marketplace-json` depuis la racine du dépôt ; les tests (`go test ./...`) échouent sinon. Si vous l'oubliez, une action GitHub le régénère sur `master` après l'acceptation. Le nom du fichier contient le MD5 de son contenu ; l'index `docs/static/pi-appliance-catalog.json` indique la version courante.

La console gère plusieurs instances, leur démarrage et leur arrêt, les mises à jour avec retour arrière, ainsi que la conservation ou la suppression des données à la désinstallation.

## Ce qui est prévu ou à l'étude

Ces applications ne sont pas encore installables depuis la marketplace. La liste vient du même catalogue JSON (`marketplace/planned.yaml`) :

<ul id="marketplace-planned" hidden></ul>

Bases de données : PostgreSQL est disponible, avec l'interface web Adminer. MongoDB (Pi 5 seulement), Redis, MSSQL (pas d'image pour le Pi) et SQLite (un simple fichier) ne sont pas proposés seuls : voir la [spécification](specification.html), section 6.3.

Les liens publics de partage Etherpad sont aussi une amélioration prévue. Consultez la [feuille de route](TODO.html) pour connaître leur avancement.

## Ajouter une application : tout le monde peut contribuer

Les contributions sont ouvertes. Vous pouvez proposer une application libre utile à la maison, améliorer un manifeste existant, ou signaler un problème. Une proposition passe par une pull request ; elle sera relue et testée avant d'être intégrée.

### 1. Créer un manifeste Docker Compose

Faites un fork du [dépôt pi-appliance](https://github.com/gmoulard/pi-appliance), puis créez `marketplace/<identifiant>/compose.yaml`. L'identifiant doit être en minuscules, commencer par une lettre et ne contenir que des lettres, chiffres et tirets.

Le fichier est un manifeste Docker Compose classique avec une section `x-pi-appliance` décrivant l'application. Exemple de structure (remplacez les valeurs et l'image par celles de votre application) :

```yaml
x-pi-appliance:
  name: Mon application
  description: Une description claire pour les utilisateurs.
  icon: M4 5h16v14H4z
  category: Outils
  tags: [administration]
  license: AGPL-3.0
  subdomain: outil
  service: app
  port: 8080
  memory_mb: 256
  disk_mb: 512
  login: application
  access: users
  parameters:
    - name: ADMIN_PASSWORD
      label: Mot de passe administrateur
      generate: true

services:
  app:
    image: editeur/image:1.2.3
    restart: unless-stopped
    environment:
      ADMIN_PASSWORD: ${ADMIN_PASSWORD}
    volumes:
      - data:/var/lib/application

volumes:
  data:
```

### 2. Respecter les règles du catalogue

- Choisissez une image Docker avec une **version précise** (pas `latest` ou `stable`) et compatible avec `linux/arm64`, l'architecture des Raspberry Pi visés.
- Déclarez le service publié dans `service`, son port interne dans `port`, ainsi que des estimations réalistes en Mo pour `memory_mb` et `disk_mb`.
- Le champ `icon` est un tracé SVG (`d`) sur une grille 24 × 24. Les champs `login` acceptent `application` ou `console` ; `access` accepte `public`, `users`, `home` ou `admins` (administrateurs, à la maison : réservé aux applications qui ont la main sur tout le Pi, comme Portainer).
- Déclarez les paramètres en majuscules dans `parameters`. Pour les secrets, utilisez `generate: true` plutôt que de mettre un mot de passe dans le manifeste ou dans une image.
- Stockez les données persistantes dans des volumes Docker nommés. Ne mettez ni secret, ni donnée personnelle, ni configuration propre à un utilisateur dans le dépôt.
- Renseignez la licence de l'application et vérifiez que sa redistribution est compatible avec le projet.
- N'utilisez que les clés documentées dans `x-pi-appliance` : une clé inconnue fait refuser le manifeste.
- Évitez les options qui donnent la main sur tout le Pi (conteneur privilégié, accès à Docker, dossier du Pi, réseau de l'hôte, droits ajoutés) : une nouvelle application qui en a ne sera pas installable depuis le catalogue en ligne, seulement après une nouvelle version de pi-appliance. Une application qui a accès à Docker ou qui est un conteneur privilégié doit en plus déclarer `access: admins`, et la console fait confirmer son installation.
- Une application prévue mais pas encore prête va dans `marketplace/planned.yaml` (nom, description, icône, catégorie et ligne de la feuille de route) : le site l'affiche « À venir ».

La définition complète des champs et les règles de validation sont dans la [spécification, section 6.5](specification.html).

### 3. Vérifier et proposer

Depuis la racine du dépôt, lancez :

```shell
go run ./cmd/marketplace-json
go test ./...
```

Les tests vérifient notamment que les manifestes sont valides, que les images ont une version fixée et que les variables Compose sont déclarées. Si possible, essayez aussi l'installation, l'arrêt, le redémarrage et la désinstallation de votre application avec Docker sur une machine de test.

Ouvrez ensuite une pull request vers `master`, avec une description de l'application, les ressources nécessaires, les étapes de test et les éventuelles limites connues. Une fois la pull request acceptée sur `master`, les appliances lisent le nouveau catalogue en ligne dans les 6 heures, sans nouvelle version de pi-appliance.

## Liens utiles

- [Code source de la marketplace](https://github.com/gmoulard/pi-appliance/tree/master/marketplace)
- [Spécification des manifestes](specification.html)
- [Feuille de route](TODO.html)
- [Dépôt pi-appliance](https://github.com/gmoulard/pi-appliance) : ouvrez une pull request depuis votre fork pour proposer votre contribution.
