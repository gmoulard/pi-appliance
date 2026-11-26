# Clés de l'API OVH : ce qu'il faut faire

Le workflow « OVH » ([`.github/workflows/ovh.yml`](https://github.com/gmoulard/pi-appliance/blob/main/.github/workflows/ovh.yml)) et le service d'adresses (TODO-3.20) écrivent dans la zone DNS `pi-appliance.com` chez OVH. Les clés actuelles sont valides, mais elles n'ont aucun droit sur la zone. Le workflow s'arrête donc avec :

```
GET /domain/zone/pi-appliance.com : HTTP 403 "This call has not been granted"
```

Il faut créer de nouvelles clés, avec les droits sur la zone. Comptez 5 minutes. **Ne copiez jamais une clé dans ce dépôt, dans un ticket ou dans une conversation** : elles ne vont que dans GitHub, et plus tard sur le Pi du projet.

## 1. Créer les clés chez OVH

1. Ouvrez https://eu.api.ovh.com/createToken/ et connectez-vous avec le compte OVH qui possède `pi-appliance.com`.
2. Remplissez :
   - **Application name** : `pi-appliance` ;
   - **Application description** : `Zone DNS pi-appliance.com` ;
   - **Validity** : `Unlimited`.
3. Dans **Rights**, ajoutez ces 5 lignes, avec le bouton « + » :

   | Méthode | Chemin |
   |---------|--------|
   | `GET` | `/domain/zone/pi-appliance.com` |
   | `GET` | `/domain/zone/pi-appliance.com/*` |
   | `POST` | `/domain/zone/pi-appliance.com/*` |
   | `PUT` | `/domain/zone/pi-appliance.com/*` |
   | `DELETE` | `/domain/zone/pi-appliance.com/*` |

   Ces droits ne touchent qu'à la zone `pi-appliance.com` : ni facturation, ni autres domaines.
4. Cliquez sur **Create keys**. OVH affiche trois valeurs : **Application key**, **Application secret** et **Consumer key**. Gardez la page ouverte : le secret ne s'affiche qu'une fois.

## 2. Les mettre dans GitHub

1. Ouvrez https://github.com/gmoulard/pi-appliance/settings/environments, puis l'environnement **OVH**.
2. Dans **Environment secrets**, remplacez chaque secret par la valeur donnée par OVH (« Edit », coller, « Update secret ») :

   | Secret GitHub | Valeur OVH |
   |---------------|-----------|
   | `OVH_Application_key` | Application key |
   | `OVH_Application_secret` | Application secret |
   | `OVH_Consumer_Key` | Consumer key |
   | `OVH_Application_name` | `pi-appliance` (pas utilisé par l'API, gardé pour mémoire) |

3. Supprimez les anciennes clés chez OVH, si vous le souhaitez : https://eu.api.ovh.com/console/ → `/me/api/application` (elles ne servent plus).

## 3. Essayer

1. Ouvrez https://github.com/gmoulard/pi-appliance/actions/workflows/ovh.yml → **Run workflow**.
2. Commencez par **action** `verifier` (lecture seule) : le résumé doit montrer les 5 droits, la zone, et les serveurs DNS annoncés sur internet.
   - Serveurs `dns*.ovh.net` et `ns*.ovh.net` : la zone est déjà servie par OVH.
   - Serveurs `awsdns` : la zone est encore servie par Route 53. Les entrées créées chez OVH ne répondront sur internet qu'après le passage (étapes dans [environnement.md](environnement.md), TODO-3.19).
3. Puis **action** `dynhost`, **nom** `vdl` : crée `vdl.pi-appliance.com` avec l'adresse IP de `vdl.moulard.org`, ou celle que vous mettez dans **ip**.

Vous pouvez aussi simplement me dire « clés OVH faites » : je relance le workflow et je vous donne le résultat.

## 4. Plus tard : le Pi du projet

Quand le service d'adresses sera installé sur le Pi du projet (TODO-3.5), les mêmes clés y seront copiées dans `/etc/pi-appliance-addresses/ovh`, avec les droits 600, une ligne par clé :

```
OVH_Application_key=...
OVH_Application_secret=...
OVH_Consumer_Key=...
```

Jamais sur les Pi des utilisateurs.
