#!/bin/sh
# This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
# Copyright (c) 2026 Guillaume Moulard.
#
# Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
# 4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
# of the LICENSE file at the root of the project: this file may be shared and
# integrated into another project unmodified, keeping this notice and a link to
# the project; modified versions may only be published to propose them to the
# official repository. Commercial use requires a separate licence from the
# author: guillaume@moulard.org.
#
# See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.

# Essai de bout en bout des services de la marketplace, comme sur le Pi :
# Traefik (configuration du Pi, réseau de l'hôte), la console (binaire de ce
# dépôt) et Docker. Chaque application est installée par l'API de la console,
# puis sa page est ouverte comme par un navigateur (services-check.py) :
#   - à la maison : https://<service>-<nom du Pi>.local/ depuis 127.0.0.1 ;
#   - sur internet : https://<adresse internet>/<service>/ depuis une adresse
#     publique (réseau Docker 203.0.113.0/24), comme vdl.moulard.org ;
# avec ses feuilles de style, scripts et images, qui doivent rester sous
# /<service>/ sur internet. Puis le service est désinstallé (données
# supprimées), pour tenir dans le disque d'un exécuteur GitHub.
#
# Usage : sudo sh system/tests/services-e2e-test.sh [application…]
#   sans argument : toutes les applications de la marketplace qui ont une page web.
#   PRUNE=1 : images retirées après chaque service (disque d'un exécuteur GitHub).
# Nécessite Docker (ports 80 et 443 de l'hôte libres), Go, curl et jq.
# Résultat : un tableau, et le code 1 si un service a échoué.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
TRAEFIK_IMAGE=$(sed -n 's/^[[:space:]]*image:[[:space:]]*//p' "$ROOT/system/etc/pi-appliance/traefik/compose.yaml" | head -n 1)
PYTHON_IMAGE=python:3.12-alpine
PI_NAME=pi-essai
DOMAIN=$PI_NAME.local
INTERNET=vdl.example.org
TMP=$(mktemp -d)
PREFIX=pi-appliance-e2e-$$
TRAEFIK=$PREFIX-traefik
PUBLIC=$PREFIX-public
PUBLIC_SUBNET=203.0.113.0/24
PUBLIC_GATEWAY=203.0.113.1
CONSOLE_PID=
API=http://127.0.0.1:8080/pi-appliance/api

# shellcheck disable=SC2329 # appelée par trap
cleanup() {
	status=$?
	if [ -n "$CONSOLE_PID" ]; then
		kill "$CONSOLE_PID" 2>/dev/null || true
	fi
	if [ "$status" -ne 0 ] && [ -f "$TMP/console.log" ]; then
		echo "--- journal de la console (fin)" >&2
		tail -n 40 "$TMP/console.log" >&2
	fi
	for dir in "$TMP"/data/services/*/; do
		[ -f "$dir/compose.yaml" ] || continue
		(cd "$dir" && docker compose down --volumes --remove-orphans >/dev/null 2>&1) || true
	done
	docker rm -f "$TRAEFIK" >/dev/null 2>&1 || true
	docker network rm "$PUBLIC" >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup 0 HUP INT TERM

for tool in docker go curl jq openssl; do
	command -v "$tool" >/dev/null || { echo "$tool manquant" >&2; exit 1; }
done

echo "Console : construction"
(cd "$ROOT" && go build -o "$TMP/pi-appliance" ./cmd/pi-appliance)

# Configuration de Traefik du Pi, avec une autorité Let's Encrypt injoignable :
# l'adresse internet d'essai garde le certificat local.
mkdir -p "$TMP/traefik/dynamic" "$TMP/tls" "$TMP/acme" "$TMP/data"
sed 's#^\(      storage: /var/lib/traefik/acme.json\)$#\1\n      caServer: https://127.0.0.1:1/directory#' \
	"$ROOT/system/etc/pi-appliance/traefik/traefik.yml" >"$TMP/traefik/traefik.yml"
grep -q 'caServer: https://127.0.0.1:1/directory' "$TMP/traefik/traefik.yml"
cp "$ROOT"/system/etc/pi-appliance/traefik/dynamic/*.yml "$TMP/traefik/dynamic/"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 2 \
	-subj "/CN=$DOMAIN" -addext "subjectAltName=DNS:$DOMAIN,DNS:*.$DOMAIN,DNS:$INTERNET" \
	-keyout "$TMP/tls/local.key" -out "$TMP/tls/local.crt" 2>/dev/null

docker network inspect pi-appliance >/dev/null 2>&1 || docker network create pi-appliance >/dev/null
docker network create --subnet "$PUBLIC_SUBNET" --gateway "$PUBLIC_GATEWAY" "$PUBLIC" >/dev/null
docker pull -q "$TRAEFIK_IMAGE" >/dev/null
docker pull -q "$PYTHON_IMAGE" >/dev/null
docker run -d --name "$TRAEFIK" --network host \
	-e TRAEFIK_CERTIFICATESRESOLVERS_LETSENCRYPT_ACME_EMAIL=essai@example.org \
	-v "$TMP/traefik/traefik.yml:/etc/traefik/traefik.yml:ro" \
	-v "$TMP/traefik/dynamic:/etc/traefik/dynamic:ro" \
	-v "$TMP/tls:/etc/traefik/tls:ro" \
	-v "$TMP/acme:/var/lib/traefik" \
	-v /var/run/docker.sock:/var/run/docker.sock:ro \
	"$TRAEFIK_IMAGE" --configFile=/etc/traefik/traefik.yml >/dev/null

# Adresse internet du Pi (« Mon Pi », adresse personnelle), écrite d'avance :
# la console vérifie sinon qu'elle existe dans le DNS.
printf '{"domain": "%s"}\n' "$INTERNET" >"$TMP/data/internet-console.json"
"$TMP/pi-appliance" -addr 127.0.0.1:8080 -data "$TMP/data" -domain "$DOMAIN" \
	-reset-file "" -network-script "" -stats-url "" -storage-script "" -backup-script "" \
	-updates-script "" -diagnostic-script "" -diagnostic-file "" -rescue-addr "" -tls-dir "$TMP/tls" \
	-cockpit-script "" -release-repo "" -catalog-url "" -cli-token-file "$TMP/token" \
	-traefik-services "$TMP/traefik/dynamic/services-internet.yml" \
	-traefik-internet "$TMP/traefik/dynamic/console-internet.yml" \
	-addresses-url "" -upnp=false -mail-relay=false >"$TMP/console.log" 2>&1 &
CONSOLE_PID=$!

attempt=0
until [ -s "$TMP/token" ] && curl -fsS --noproxy '*' -o /dev/null "$API/hello"; do
	attempt=$((attempt + 1))
	[ "$attempt" -lt 60 ] || { echo "la console ne répond pas" >&2; exit 1; }
	sleep 1
done
TOKEN=$(cat "$TMP/token")

# Compte administrateur créé comme au premier démarrage, à travers Traefik :
# sa session ouvre les services réservés aux personnes connectées.
SESSION=$(curl -sS -k --noproxy '*' --resolve "$DOMAIN:443:127.0.0.1" -H 'X-Pi-Appliance: 1' -H 'Content-Type: application/json' \
	--data '{"name":"essai","email":"essai@example.org","password":"Essai-de-bout-en-bout-2026","lang":"fr"}' \
	-D - -o /dev/null "https://$DOMAIN/pi-appliance/api/setup" | sed -n 's/^[Ss]et-[Cc]ookie: pi_session=\([^;]*\);.*/\1/p')
[ -n "$SESSION" ] || { echo "compte administrateur non créé" >&2; exit 1; }

# api <méthode> <chemin> [corps JSON] : appel de la console comme la ligne de commande du Pi.
api() {
	curl -sS --noproxy '*' -X "$1" -H "Authorization: Bearer $TOKEN" -H 'X-Pi-Appliance: 1' -H 'Content-Type: application/json' \
		${3:+--data "$3"} -w '\n%{http_code}' "$API$2"
}


# check <adresse> <attendu> [public|session] : page ouverte par services-check.py,
# depuis la maison (réseau de l'hôte, 127.0.0.1), connecté à la console
# (session) ou non, ou depuis internet (adresse publique).
check() {
	case "${3:-}" in
	public)
		docker run --rm --network "$PUBLIC" -v "$ROOT/system/tests/services-check.py:/check.py:ro" \
			"$PYTHON_IMAGE" python /check.py "$PUBLIC_GATEWAY" "$1" "$2"
		;;
	session)
		docker run --rm --network host -v "$ROOT/system/tests/services-check.py:/check.py:ro" \
			"$PYTHON_IMAGE" python /check.py 127.0.0.1 "$1" "$2" "$SESSION"
		;;
	*)
		docker run --rm --network host -v "$ROOT/system/tests/services-check.py:/check.py:ro" \
			"$PYTHON_IMAGE" python /check.py 127.0.0.1 "$1" "$2"
		;;
	esac
}

# wait_page <adresse> <attendu> [public] : comme check, en laissant au service
# le temps de finir son premier démarrage (base de données, installation).
wait_page() {
	tries=0
	while ! output=$(check "$@" 2>&1); do
		tries=$((tries + 1))
		if [ "$tries" -ge 24 ]; then
			echo "$output"
			return 1
		fi
		sleep 5
	done
	echo "$output"
}

catalog=$(api GET /catalog | sed '$d')
if [ "$#" -gt 0 ]; then
	apps=$*
else
	apps=$(echo "$catalog" | jq -r '.apps[] | select(.web) | .id')
fi

: >"$TMP/results"
failed=0
for app in $apps; do
	entry=$(echo "$catalog" | jq -c --arg id "$app" '.apps[] | select(.id == $id)')
	if [ -z "$entry" ]; then
		echo "$app : pas dans la marketplace" >&2
		failed=1
		continue
	fi
	name=$(echo "$entry" | jq -r .subdomain)
	access=$(echo "$entry" | jq -r '.access // "public"')
	echo "=== $app ($name, accès $access)"
	started=$(date +%s)
	response=$(api POST /services "{\"app\":\"$app\",\"name\":\"$name\",\"confirm_risks\":true}")
	code=$(echo "$response" | tail -n 1)
	home=- internet=-
	if [ "$code" != 201 ]; then
		echo "ÉCHEC installation (HTTP $code) : $(echo "$response" | sed '$d' | cut -c1-600)"
		install=ÉCHEC
		failed=1
	else
		install=ok
		address=$(echo "$response" | sed '$d' | jq -r .instance.address)
		published=$(api GET /services | sed '$d' | jq -c --arg n "$name" '.services[] | select(.name == $n)')
		internet_address=$(echo "$published" | jq -r '.internet_address // ""')
		reason=$(echo "$published" | jq -r '.internet_reason // ""')
		# Installation réussie pour la console (docker compose up --wait) mais
		# conteneur qui redémarre en boucle : Traefik perd le routeur du service.
		sleep 10
		restarting=$(docker ps -a --filter "label=com.docker.compose.project=pi-$name" --format '{{.Names}} {{.Status}}' | grep -E 'Restarting|Exited' || true)
		if [ -n "$restarting" ]; then
			echo "ÉCHEC conteneur arrêté ou qui redémarre : $restarting"
			docker logs --tail 15 "${restarting%% *}" 2>&1 | sed 's/^/    /'
			install="ÉCHEC (redémarre)"
			failed=1
		fi
		# Service réservé aux personnes connectées : renvoi vers la connexion,
		# puis la page, une fois connecté.
		home=ok
		case "$access" in
		users | admins)
			check "https://$address/" login || { home="ÉCHEC (sans connexion)"; failed=1; }
			wait_page "https://$address/" page session || { home=ÉCHEC; failed=1; }
			;;
		*)
			wait_page "https://$address/" page || { home=ÉCHEC; failed=1; }
			;;
		esac
		if [ -n "$internet_address" ]; then
			if wait_page "$internet_address" page public; then internet=ok; else internet=ÉCHEC; failed=1; fi
		else
			internet="non publié ($reason)"
			# Non publié : l'adresse internet ne doit pas mener au service.
			if ! check "https://$INTERNET/$name/" refused public; then internet="ÉCHEC (ouvert sans être publié)"; failed=1; fi
		fi
		api DELETE "/services/$name?data=delete" >/dev/null
	fi
	printf '%s\t%s\t%s\t%s\t%ss\n' "$app" "$install" "$home" "$internet" "$(($(date +%s) - started))" >>"$TMP/results"
	if [ "${PRUNE:-}" = 1 ]; then docker image prune -af >/dev/null 2>&1 || true; fi
done

echo
printf 'Application\tInstallation\tMaison\tInternet\tDurée\n'
cat "$TMP/results"
exit "$failed"
