# Environnement du projet

Les services hors du dépôt dont dépend pi-appliance, et comment y accéder. Ce fichier est publié sur le site : **aucun secret ici** (identifiants, clés, jetons, numéros de compte).

## Domaine `pi-appliance.com`

Clés de l'API OVH à créer ou à refaire : [clefOVH.md](clefOVH.md).

- **DNS** : zone hébergée publique dans **AWS Route 53**, profil AWS `default`, région `eu-west-3`. Tranché le 2026-11-26 : la zone passe chez **OVH** (TODO-3.19) ; clés de l'API OVH dans les secrets GitHub Actions `OVH_Application_name`, `OVH_Application_key`, `OVH_Application_secret`, `OVH_Consumer_Key`, et sur le Pi du projet pour le service d'adresses (TODO-3.20), jamais sur les Pi des utilisateurs.
  - Identifiant de la zone : `Z01037363SBTB2LIT0ZB4`.
  - Serveurs de noms (délégation vérifiée le 2026-11-19) : `ns-508.awsdns-63.com`, `ns-961.awsdns-56.net`, `ns-1861.awsdns-40.co.uk`, `ns-1453.awsdns-53.org`.
- **Ce n'est pas encore OVH** : tant que les serveurs de noms annoncés sont ceux d'AWS ci-dessus, une modification de zone faite chez OVH n'a aucun effet.

### Passage chez OVH (TODO-3.19)

Étapes, à faire par l'utilisateur (les clés et la délégation ne sont pas dans le dépôt) :

1. Recréer dans la zone OVH les enregistrements de Route 53 : ceux du site (ci-dessous), `app` (application mobile, dépôt pi-appliance-pwa), `adresses` et `stats` (Pi du projet, quand ces services seront installés), ceux d'Amazon SES (trois `CNAME` DKIM, SPF s'il y en a un, `_dmarc`), les sous-domaines de machines encore utiles et les adresses `<nom>` déjà réservées (en `A` vers l'adresse IP de leur box, TODO-3.20).
2. Vérifier les clés et la zone avec le workflow « OVH » (`.github/workflows/ovh.yml`, action « verifier ») ; il écrit aussi une entrée DynHost à la demande (action « dynhost »).
3. Changer les serveurs de noms du domaine pour ceux d'OVH (donnés par le workflow), puis vérifier : `dig NS pi-appliance.com`, www.pi-appliance.com et app.pi-appliance.com en HTTPS, e-mails d'Amazon SES toujours signés DKIM.
4. Supprimer la zone Route 53 et l'utilisateur IAM du service d'adresses s'il a été créé.

### Enregistrements du site

| Nom | Type | Valeur |
|---|---|---|
| `pi-appliance.com` | `A` | `185.199.108.153`, `185.199.109.153`, `185.199.110.153`, `185.199.111.153` |
| `pi-appliance.com` | `AAAA` | `2606:50c0:8000::153`, `2606:50c0:8001::153`, `2606:50c0:8002::153`, `2606:50c0:8003::153` |
| `www.pi-appliance.com` | `CNAME` | `gmoulard.github.io` |

### Autres enregistrements présents

- E-mail par **Amazon SES** : trois `CNAME` DKIM `*._domainkey` vers `*.dkim.amazonses.com`, et `_dmarc` en `TXT` (`v=DMARC1; p=none;`). Tranché le 2026-11-26 : ces entrées et le compte AWS sont à revoir plus tard, après le passage de la zone chez OVH ; d'ici là, rien n'est supprimé (entrées recréées telles quelles chez OVH, compte AWS gardé).
- Sous-domaines de machines : `mac`, `ia4ccm`, `mbpdevrenaudin.home`.
- Probablement à supprimer : `todrop`, `ip-10-0-0-85.eu-west-3.compute.internal`.

### Service d'adresses

Le service d'adresses (`cmd/pi-appliance-addresses`, [spécification 7.1](specification.md)) écrit dans la zone chez OVH les enregistrements `A` de `<nom>.pi-appliance.com` et `*.<nom>.pi-appliance.com`, vers l'adresse IP que chaque appliance lui envoie. Il n'est pas encore installé. Il lui faut :

- les **clés de l'API OVH**, les mêmes que les secrets GitHub Actions `OVH_Application_key`, `OVH_Application_secret`, `OVH_Consumer_Key` (décision de l'utilisateur du 2026-11-26), copiées à l'installation dans `/etc/pi-appliance-addresses/ovh` **sur le Pi du projet seulement** (droits 600, une ligne `NOM=valeur` par clé, aux noms des secrets), jamais dans le dépôt ni sur les Pi des utilisateurs ; leurs droits se lisent avec le workflow « OVH » (action « verifier »). Les noms du projet (`www`, `mail`…) sont refusés par le service lui-même (`internal/addresses/names.go`) ;
- le nom `adresses.pi-appliance.com` vers le Pi du projet, derrière son reverse proxy qui pose `X-Real-IP` et dont le journal des accès est coupé (TODO-4.4).

## Site web

- **GitHub Pages** du dépôt `gmoulard/pi-appliance` : branche `main` (à régler dans Settings → Pages tant que la source y est encore `master`, TODO-0.9), dossier `/docs`, domaine `www.pi-appliance.com` (fichier `docs/CNAME`), HTTPS actif.
- Jekyll : `docs/_config.yml`, gabarit `docs/_layouts/default.html`.

## AWS CLI

La CLI n'est pas installée sur le poste Windows. Elle tourne dans Docker (image `amazon/aws-cli`), avec la configuration AWS dans `%USERPROFILE%\AWS\.aws` (pas dans `%USERPROFILE%\.aws`) :

```powershell
docker run --rm -v "$env:USERPROFILE\AWS\.aws:/root/.aws:ro" amazon/aws-cli route53 list-resource-record-sets --hosted-zone-id Z01037363SBTB2LIT0ZB4
```

- Lecture seule (`:ro`) suffit pour les commandes qui utilisent des clés d'accès. Une connexion SSO (`aws sso login`) écrit son cache : montez alors le dossier sans `:ro`.
- Choisir un autre profil : `-e AWS_PROFILE=<profil>`. Le domaine est sur le profil `default`.

## Poste de développement

- Windows 11, PowerShell ; Go pour le code (`go vet ./... && go test ./...`).
- Binaire du Pi : `GOOS=linux GOARCH=arm64 go build ./cmd/pi-appliance`.
