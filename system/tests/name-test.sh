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

# Tests du nom du Pi (name.sh), sans Pi : nom tiré de l'adresse matérielle,
# noms refusés, fichiers écrits dans un dossier temporaire.
#   sh system/tests/name-test.sh

# shellcheck source-path=SCRIPTDIR disable=SC2034
set -eu

LIB=$(cd "$(dirname "$0")/../usr/local/lib/pi-appliance" && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
failures=0

fail() {
	echo "ÉCHEC $1"
	failures=$((failures + 1))
}

PI_STATE=$TMP/state
AP_KEYFILE=$TMP/ap.nmconnection
HOSTNAME_FILE=$TMP/hostname
HOSTS_FILE=$TMP/hosts
NET_CLASS=$TMP/net
HOSTNAME_CMD=true
NAME_LIB_ONLY=1
# shellcheck source=../usr/local/lib/pi-appliance/name.sh
. "$LIB/name.sh"

for name in a pi-appliance-3f2a controltower salon2 "$(printf 'a%.0s' $(seq 32))"; do
	valid_name "$name" || fail "nom refusé à tort : $name"
done
for name in "" -salon salon- Salon "le salon" "a.b" "$(printf 'a%.0s' $(seq 33))" "pi_appliance" "x;reboot"; do
	if valid_name "$name"; then fail "nom accepté à tort : « $name »"; fi
done

mkdir -p "$NET_CLASS/wlan0" "$NET_CLASS/eth0"
echo "D8:3A:DD:12:3F:2A" >"$NET_CLASS/wlan0/address"
echo "d8:3a:dd:12:99:01" >"$NET_CLASS/eth0/address"
[ "$(default_name)" = pi-appliance-3f2a ] || fail "nom du Wi-Fi : $(default_name)"
rm -r "$NET_CLASS/wlan0"
[ "$(default_name)" = pi-appliance-9901 ] || fail "nom du câble : $(default_name)"
rm -r "$NET_CLASS/eth0"
default_name | grep -Eqx 'pi-appliance-[0-9a-f]{4}' || fail "nom au hasard : $(default_name)"

# Premier démarrage, puis démarrage suivant : le nom est gardé.
mkdir -p "$NET_CLASS/wlan0"
echo "d8:3a:dd:12:3f:2a" >"$NET_CLASS/wlan0/address"
printf '[wifi]\nmode=ap\nssid=pi-appliance\nchannel=6\n' >"$AP_KEYFILE"
printf '127.0.0.1\tlocalhost\n127.0.1.1\tpi-appliance\n' >"$HOSTS_FILE"
boot >/dev/null
echo "d8:3a:dd:00:00:01" >"$NET_CLASS/wlan0/address"
boot >/dev/null
[ "$(cat "$PI_STATE/name")" = pi-appliance-3f2a ] || fail "nom non gardé : $(cat "$PI_STATE/name")"
[ "$(cat "$HOSTNAME_FILE")" = pi-appliance-3f2a ] || fail "nom d'hôte : $(cat "$HOSTNAME_FILE")"
grep -qx 'ssid=pi-appliance-3f2a' "$AP_KEYFILE" || fail "nom du Wi-Fi de configuration : $(cat "$AP_KEYFILE")"
[ "$(grep -c '^127\.0\.1\.1' "$HOSTS_FILE")" = 1 ] || fail "/etc/hosts : $(cat "$HOSTS_FILE")"
grep -q '^127\.0\.1\.1[[:space:]]pi-appliance-3f2a$' "$HOSTS_FILE" || fail "/etc/hosts : $(cat "$HOSTS_FILE")"
grep -q '^127\.0\.0\.1[[:space:]]localhost$' "$HOSTS_FILE" || fail "/etc/hosts abîmé : $(cat "$HOSTS_FILE")"

# Nom choisi dans la console (sans nmcli, secrets.sh ni systemd-run ici).
apply_name controltower
save_name controltower
[ "$(current_name)" = controltower ] || fail "nom actuel : $(current_name)"
grep -qx 'ssid=controltower' "$AP_KEYFILE" || fail "Wi-Fi après changement : $(cat "$AP_KEYFILE")"

# Un fichier /etc/hosts sans ligne 127.0.1.1 en reçoit une.
printf '127.0.0.1\tlocalhost\n' >"$HOSTS_FILE"
apply_name salon
grep -q '^127\.0\.1\.1[[:space:]]salon$' "$HOSTS_FILE" || fail "ligne 127.0.1.1 ajoutée : $(cat "$HOSTS_FILE")"

# Nom déjà pris sur le réseau de la maison (name.sh check) : avahi simulé.
CHECK_DELAY=0
taken=""
renamed=""
renamed_to=""
own_addresses() { echo 192.168.1.20; echo 172.17.0.1; }
resolve_host() {
	case " $taken " in
	*" $1 "*) echo 192.168.1.50 ;;
	*) if [ "$1" = "$(current_name)" ]; then echo 192.168.1.20; fi ;;
	esac
}
resolve_address() { echo "${renamed:-$(current_name)}.local"; }
set_name() { save_name "$1"; renamed_to=$1; }

save_name salon
[ "$(check)" = salon ] || fail "nom libre changé"
[ -z "$renamed_to" ] || fail "nom libre renommé en $renamed_to"

taken="salon"
check >/dev/null 2>&1
[ "$renamed_to" = salon-2 ] || fail "nom pris : $renamed_to au lieu de salon-2"

save_name salon
renamed_to=""
taken="salon salon-2 salon-3"
check >/dev/null 2>&1
[ "$renamed_to" = salon-4 ] || fail "suffixe suivant : $renamed_to au lieu de salon-4"

save_name salon
renamed_to=""
taken="salon salon-2 salon-3 salon-4"
check >/dev/null 2>&1
printf '%s\n' "$renamed_to" | grep -Eqx 'pi-appliance-[0-9a-f]{4}' || fail "nom au hasard : $renamed_to"

save_name salon
renamed_to=""
taken=""
renamed="salon-2"
check >/dev/null 2>&1
[ "$renamed_to" = salon-2 ] || fail "renommage d'avahi : $renamed_to au lieu de salon-2"
renamed=""

long="$(printf 'a%.0s' $(seq 32))"
valid_name "$(candidate "$long" 4)" || fail "nom suivant trop long : $(candidate "$long" 4)"
[ "$(candidate pi-appliance-3f2a 2)" = pi-appliance-3f2a-2 ] || fail "nom suivant : $(candidate pi-appliance-3f2a 2)"

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "nom du Pi : tous les tests passent"
