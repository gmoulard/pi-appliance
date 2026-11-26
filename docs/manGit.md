# Github

## Rappel

**Ne pas oublier la base : l'aide en ligne de commande.
Il s'agit de la meilleur documentation.**

```shell
git help config
git help push
git help pull
git help branch
```

## Configuration

```shell
# Identity Name
git config --global user.name "aquelito"

# Identity Email
git config --global user.email "axel@aquelito.fr"

# Editor Tool
git config --global core.editor subl

# Diff Tool
git config --global merge.tool filemerge
```

Liste des globals

```shell
git config --list
```

## Principales commandes

Status des fichiers

```shell
git status
```

Lister les branchs

```shell
git branch -a
```

`*`sur la branche courante.

Créer une branch

```shell
# Deux lignes: créer et basculer sur la nouvelle branch
git branch nom_de_ma_branch_nouvelle
git checkout nom_de_ma_branch_nouvelle

# Une seule ligne: créer et basculer
git checkout -b nom_de_ma_branch_nouvelle
```

Supprimer une branch

```shell
# Si la branch est local et n'est pas créée sur le repo distant
git branch -d nom_de_ma_branch_local

# Si la branch est présente sur le repo distant
git push origin --delete nom_de_ma_branch_distante
```

Changer de branch

```shell
git checkout nom_de_ma_branch
```

Premier commit

```shell
git add .
git commit -m "initial commit"
```

Commit suivant

```shell
git add chemin_vers_mon_fichier
git commit -m "message du commit"
```

Annuler le dernier commit et modifs

```shell
git reset --hard md5_commit
git push --force
```

Antidaté un commit.

```shell
git add .
GIT_AUTHOR_DATE="2015-12-12 08:32 +100" git commit -m "Commit antidaté"
```

Mettre à jour le dépôt local

```shell
git pull
```

Mettre à jour le dépôt local d'une branch spécifique

```shell
git pull origin MA_BRANCH
```

Envoyer ses commits vers le dépôt distant

```shell
git push
```

Envoyer ses commits vers le dépôt distant sur une branch spécifique

```shell
git push origin MA_BRANCH
```

Supprimer un fichier du répertoire de travail et de l'index

```shell
git rm nom_du_fichier
```

Supprimer un fichier de l'index

```shell
git rmg --cached nom_du_fichier
```

## Diff

```shell
# Affiche la différence entre le contenu du dernier commit et celui du 
# répertoire de travail. Cela correspond à ce qui serait commité par git commit -a.
git diff HEAD

# Affiche la différence entre le contenu pointé par A et celui pointé par B.
git diff A B

# Diff entre un dossier présent sur deux branches
git diff main..MA_BRANCH chemin/vers/mon_dossier
```

## Log

```shell
# Classique
git log

# Affiche X derniers commits
git log -n X

# Affiche les commits concernant un dossier
git log --oneline -- chemin/vers/mon_dossier

# Affiche un ensemble de commits par date
git log --since=date --until=date

# Représentation de l’historique à partir de HEAD (commit / branch)
git log --oneline --graph --decorate

# Représentation de l’historique à partir d'un fichier (commit / branch)
git log --oneline --graph --decorate nom_du_fichier
```

## Annuler commits (soft)

Seul le commit est retiré de Git ; vos fichiers, eux, restent modifiés. Vous pouvez alors à nouveau changer vos fichiers si besoin est et refaire un commit.

Annuler le dernier commit

```shell
git reset HEAD^
```

Pour indiquer à quel commit on souhaite revenir, il existe plusieurs notations :

* HEAD : dernier commit ;
* HEAD^ : avant-dernier commit ;
* HEAD^^ : avant-avant-dernier commit ;
* HEAD~2 : avant-avant-dernier commit (notation équivalente) ;
* d6d98923868578a7f38dea79833b56d0326fcba1 : indique un numéro de commit ;
* d6d9892 : indique un numéro de commit version courte.

## Annuler commits (hard)

Si vous voulez annuler votre dernier commit et les changements effectués dans les fichiers, il faut faire un reset hard. *Cela annulera sans confirmation tout votre travail !*

Annuler les commits et perdre tous les changements

```shell
git reset --hard HEAD^
```

*Annuler les modifications d’un fichier avant un commit*

Si vous avez modifié plusieurs fichiers mais que vous n’avez pas encore envoyé le commit et que vous voulez restaurer un fichier tel qu’il était au dernier commit :

```shell
git checkout nom_du_fichier
```

*Annuler/Supprimer un fichier avant un commit*

Supposer que vous venez d’ajouter un fichier à Git avec `git add` et que vous vous apprêtez à le « commiter ». Cependant, vous vous rendez compte que ce fichier est une mauvaise idée et vous voulez annuler votre `git add`.

Il est possible de retirer un fichier qui avait été ajouté pour être « commité » en procédant comme suit :

```shell
git reset HEAD -- nom_du_fichier_a_supprimer
```

## Publier une version de pi-appliance

Une version se publie en poussant une étiquette `v…` : le workflow « Image » construit alors l'image et l'archive de mise à jour, puis crée lui-même la version GitHub avec ces fichiers et leurs sommes de contrôle (spécification, section 9.2). Ne pas créer la version depuis l'interface de GitHub : le workflow échouerait au moment de publier. Les sessions Claude Code dans le cloud ne peuvent pas pousser d'étiquette : le faire depuis son poste.

Première version, `v0.1.0`, sur le commit `ce785b2` de `master` (aujourd'hui `main`) :

```shell
git fetch origin
git tag -a v0.1.0 -m "pi-appliance v0.1.0" ce785b2df668676976f05d81f1774df954f58405
git push origin v0.1.0
```

Une préversion, proposée seulement aux Pi réglés sur le canal bêta (spécification 5.4), porte un suffixe : `1.3.0-beta.1` dans `VERSION`, étiquette `v1.3.0-beta.1` ; le workflow la publie marquée « préversion ».

Pour les suivantes (skill `release`) : mettre le nouveau numéro dans le fichier `VERSION` et ajouter une ligne en bas de `CHANGELOG.md`, dans le même commit, puis étiqueter ce commit avec le même numéro précédé de `v` (`origin/main` à la place de l'empreinte). Le workflow « Image » s'arrête si l'étiquette diffère de `VERSION` : ne jamais déplacer l'étiquette, publier une nouvelle version. Les Pi installés avec une image `v0.1.0` ou plus récente proposent ensuite la nouvelle version dans « Mon Pi ».
