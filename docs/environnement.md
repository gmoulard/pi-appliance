# Environnement du projet

Les services hors du dépôt dont dépend pi-appliance, et comment y accéder. Ce fichier est publié sur le site : **aucun secret ici** (identifiants, clés, jetons, numéros de compte).

## Domaine `pi-appliance.com`

- **DNS** : zone hébergée publique dans **AWS Route 53**, profil AWS `default`, région `eu-west-3`.
  - Identifiant de la zone : `Z01037363SBTB2LIT0ZB4`.
  - Serveurs de noms (délégation vérifiée le 2026-11-19) : `ns-508.awsdns-63.com`, `ns-961.awsdns-56.net`, `ns-1861.awsdns-40.co.uk`, `ns-1453.awsdns-53.org`.
- **Ce n'est pas OVH** : une modification de zone faite chez OVH n'a aucun effet.

### Enregistrements du site

| Nom | Type | Valeur |
|---|---|---|
| `pi-appliance.com` | `A` | `185.199.108.153`, `185.199.109.153`, `185.199.110.153`, `185.199.111.153` |
| `pi-appliance.com` | `AAAA` | `2606:50c0:8000::153`, `2606:50c0:8001::153`, `2606:50c0:8002::153`, `2606:50c0:8003::153` |
| `www.pi-appliance.com` | `CNAME` | `gmoulard.github.io` |

### Autres enregistrements présents

- E-mail par **Amazon SES** : trois `CNAME` DKIM `*._domainkey` vers `*.dkim.amazonses.com`, et `_dmarc` en `TXT` (`v=DMARC1; p=none;`).
- Sous-domaines de machines : `mac`, `ia4ccm`, `mbpdevrenaudin.home`.
- Probablement à supprimer : `todrop`, `ip-10-0-0-85.eu-west-3.compute.internal`.

## Site web

- **GitHub Pages** du dépôt `gmoulard/pi-appliance` : branche `master`, dossier `/docs`, domaine `www.pi-appliance.com` (fichier `docs/CNAME`), HTTPS actif.
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
