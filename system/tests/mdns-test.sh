#!/bin/sh
# This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
# Copyright (c) 2026 Guillaume Moulard.
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published
# by the Free Software Foundation, version 3.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU Affero General Public License for more details.
#
# You should have received a copy of the GNU Affero General Public License
# along with this program.  If not, see <https://www.gnu.org/licenses/>.

# Tests de l'annonce mDNS des services et de leurs certificats (mdns.sh), sans
# Pi ni avahi : services installés dans un dossier temporaire, avahi-publish
# remplacé par un faux, vrai openssl.
#   sh system/tests/mdns-test.sh

# shellcheck source-path=SCRIPTDIR disable=SC2034
set -eu

LIB=$(cd "$(dirname "$0")/../usr/local/lib/pi-appliance" && pwd)
TMP=$(mktemp -d)
failures=0

fail() {
	echo "ÉCHEC $1"
	failures=$((failures + 1))
}

SERVICES_DIR=$TMP/services
LOG=$TMP/published
cat >"$TMP/avahi-publish" <<'FAKE'
#!/bin/sh
echo "$*" >>"$LOG"
exec sleep 600
FAKE
chmod +x "$TMP/avahi-publish"
export LOG
PUBLISH_CMD=$TMP/avahi-publish
CERTS_DIR=$TMP/tls/services
TLS_FILE=$TMP/services-tls.yml
HOSTNAME_CMD=fake_hostname
MDNS_LIB_ONLY=1
# shellcheck source=../usr/local/lib/pi-appliance/mdns.sh
. "$LIB/mdns.sh"
fake_hostname() { echo pi3b-plus; }
pi_address=192.168.1.127
home_address() { echo "$pi_address"; }
trap 'stop_all; rm -rf "$TMP"' EXIT

install_service() {
	mkdir -p "$SERVICES_DIR/$1"
	printf '%s\n' "$2" >"$SERVICES_DIR/$1/instance.json"
}

# Les faux annonceurs écrivent en arrière-plan : leur laisser le temps.
published_now() {
	sleep 0.5
	sort "$LOG" 2>/dev/null || true
}

sync_once
[ -z "$(published_now)" ] || fail "rien d'installé, mais : $(published_now)"

install_service www '{"name": "www", "address": "www-pi3b-plus.local"}'
install_service vpn '{"name": "vpn", "address": "vpn-pi3b-plus.local", "no_web_page": true}'
install_service dehors '{"name": "dehors", "address": "dehors.exemple.fr"}'
install_service casse '{"name": "casse"'
install_service hote '{"name": "hote", "address": "pi3b-plus.local"}'
sync_once
[ "$(published_now)" = "-a -R www-pi3b-plus.local 192.168.1.127" ] || fail "annonce de www : $(published_now)"
[ "$(echo "$publishers" | wc -w)" = 1 ] || fail "annonceurs : $publishers"

: >"$LOG"
sync_once
[ -z "$(published_now)" ] || fail "annonce relancée sans changement : $(published_now)"

install_service wiki '{"name": "wiki", "address": "wiki.salon.local"}'
sync_once
expected="-a -R wiki.salon.local 192.168.1.127
-a -R www-pi3b-plus.local 192.168.1.127"
[ "$(published_now)" = "$expected" ] || fail "nouveau service : $(published_now)"
[ "$(echo "$publishers" | wc -w)" = 2 ] || fail "annonceurs après ajout : $publishers"

: >"$LOG"
pi_address=192.168.1.200
sync_once
expected="-a -R wiki.salon.local 192.168.1.200
-a -R www-pi3b-plus.local 192.168.1.200"
[ "$(published_now)" = "$expected" ] || fail "nouvelle adresse du Pi : $(published_now)"

: >"$LOG"
for pid in $publishers; do kill "$pid"; done
sleep 1
sync_once
[ "$(published_now | wc -l)" = 2 ] || fail "annonces relancées après avahi : $(published_now)"

rm -r "$SERVICES_DIR/wiki" "$SERVICES_DIR/www"
old=$publishers
sync_once
[ -z "$publishers" ] || fail "annonceurs après retrait : $publishers"
for pid in $old; do
	if kill -0 "$pid" 2>/dev/null; then fail "annonceur $pid encore en marche"; fi
done

: >"$LOG"
install_service www '{"name": "www", "address": "www-pi3b-plus.local"}'
pi_address=""
sync_once
[ -z "$(published_now)" ] || fail "annonce sans réseau de la maison : $(published_now)"

# Certificats : un par adresse en .local, même à deux niveaux (service pas
# encore déplacé), aucun pour le Pi lui-même, un service sans page web ou hors .local.
rm -rf "$SERVICES_DIR"
install_service www '{"name": "www", "address": "www-pi3b-plus.local"}'
install_service wiki '{"name": "wiki", "address": "wiki.salon.local"}'
install_service vpn '{"name": "vpn", "address": "vpn-pi3b-plus.local", "no_web_page": true}'
install_service dehors '{"name": "dehors", "address": "dehors.exemple.fr"}'
install_service hote '{"name": "hote", "address": "pi3b-plus.local"}'
sync_certs
[ "$(cd "$CERTS_DIR" && printf '%s ' *)" = "wiki.salon.local.crt wiki.salon.local.key www-pi3b-plus.local.crt www-pi3b-plus.local.key " ] ||
	fail "certificats : $(ls "$CERTS_DIR")"
openssl x509 -noout -ext subjectAltName -in "$CERTS_DIR/www-pi3b-plus.local.crt" | grep -q "DNS:www-pi3b-plus.local$" ||
	fail "nom du certificat de www"
openssl x509 -noout -checkend 2592000 -in "$CERTS_DIR/www-pi3b-plus.local.crt" >/dev/null || fail "certificat de www trop court"
[ "$(stat -c %a "$CERTS_DIR/www-pi3b-plus.local.key")" = 600 ] || fail "droits de la clé de www"
if ! grep -q "certFile: /etc/traefik/tls/services/www-pi3b-plus.local.crt" "$TLS_FILE" ||
	! grep -q "keyFile: /etc/traefik/tls/services/wiki.salon.local.key" "$TLS_FILE"; then
	fail "liste pour Traefik : $(cat "$TLS_FILE")"
fi
before=$(cat "$TLS_FILE")
serial=$(openssl x509 -noout -serial -in "$CERTS_DIR/www-pi3b-plus.local.crt")
sync_certs
[ "$(cat "$TLS_FILE")" = "$before" ] || fail "liste réécrite sans changement"
[ "$(openssl x509 -noout -serial -in "$CERTS_DIR/www-pi3b-plus.local.crt")" = "$serial" ] || fail "certificat refait sans raison"

# Certificat qui expire dans moins de 30 jours : refait, et Traefik le voit.
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 10 \
	-subj "/CN=www-pi3b-plus.local" -addext "subjectAltName=DNS:www-pi3b-plus.local" \
	-keyout "$CERTS_DIR/www-pi3b-plus.local.key" -out "$CERTS_DIR/www-pi3b-plus.local.crt" 2>/dev/null
before=$(cat "$TLS_FILE")
sync_certs
openssl x509 -noout -checkend 2592000 -in "$CERTS_DIR/www-pi3b-plus.local.crt" >/dev/null || fail "certificat bientôt expiré pas refait"
[ "$(cat "$TLS_FILE")" != "$before" ] || fail "certificat refait, liste inchangée"

# Service retiré : son certificat aussi ; plus rien : liste vide, valide.
rm -r "$SERVICES_DIR/wiki"
sync_certs
[ ! -e "$CERTS_DIR/wiki.salon.local.crt" ] && [ ! -e "$CERTS_DIR/wiki.salon.local.key" ] || fail "certificat d'un service retiré"
! grep -q wiki "$TLS_FILE" || fail "service retiré encore dans la liste"
rm -rf "$SERVICES_DIR"
sync_certs
[ -z "$(find "$CERTS_DIR" -type f)" ] || fail "certificats sans service : $(find "$CERTS_DIR" -type f)"
grep -qx "    \[\]" "$TLS_FILE" || fail "liste vide : $(cat "$TLS_FILE")"

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "annonce mDNS et certificats des services : tous les tests passent"
