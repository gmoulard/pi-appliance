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

# Tests de « network.sh test », « status » et « reboot », sans Pi : de faux
# nmcli, ping, getent, avahi-resolve-host-name, systemctl et systemd-run
# notent ce qui leur est demandé. sh system/tests/network-test.sh

# Corps des faux programmes entre apostrophes : développés quand ils tournent.
# shellcheck disable=SC2016
set -eu

failures=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# Scripts exécutables comme sur le Pi (install.sh les installe en 0755).
LIB=$work/lib
cp -r "$(dirname "$0")/../usr/local/lib/pi-appliance" "$LIB"
chmod +x "$LIB"/*.sh
SCRIPT=$LIB/network.sh
bin=$work/bin
fake=$work/fake
mkdir -p "$bin" "$fake" "$work/state"

# fake <commande> <corps du script>
fake() {
	printf '#!/bin/sh\nFAKE=%s\n%s\n' "$fake" "$2" >"$bin/$1"
	chmod +x "$bin/$1"
}
# Fichiers du faux réseau : devices (sortie de « device status »), ap (point
# d'accès ouvert), autoconnect, box (la box répond), internet, mdns.
fake nmcli 'echo "nmcli $*" >>"$FAKE/calls"
case "$*" in
*"device status"*) cat "$FAKE/devices" 2>/dev/null ;;
*"connection show --active"*) [ -e "$FAKE/ap" ] && echo pi-appliance-ap ;;
*"device wifi list"*) [ -e "$FAKE/no-scan" ] || echo "yes:Maison\\:Salon" ;;
*"802-11-wireless.ssid connection show"*) echo "Maison:Salon" ;;
*"IP4.ADDRESS device show"*) echo "192.168.1.20/24" ;;
*"IP4.GATEWAY device show"*) echo "192.168.1.1" ;;
*"connection.autoconnect"*) cat "$FAKE/autoconnect" 2>/dev/null ;;
*" up "*|*" down "*|*rescan\ yes*) echo "nmcli interdit : $*" >>"$FAKE/forbidden" ;;
esac
exit 0'
fake ping 'echo "ping $*" >>"$FAKE/calls"; [ -e "$FAKE/box" ]'
fake getent '[ -e "$FAKE/internet" ]'
fake avahi-resolve-host-name '[ -e "$FAKE/mdns" ] && printf "%s\t192.168.1.20\n" "$2"; exit 0'
fake systemctl 'echo "systemctl $*" >>"$FAKE/calls"
case "$*" in *is-active*) [ -e "$FAKE/timer" ] ;; *) exit 0 ;; esac'
fake systemd-run 'echo "systemd-run $*" >>"$FAKE/calls"; [ -e "$FAKE/run-fails" ] && exit 1; touch "$FAKE/timer"'
fake sync 'exit 0'
fake iw 'exit 0'

export PATH="$bin:$PATH" PI_LIB="$LIB" PI_STATE="$work/state" PI_RUN="$work/run" REBOOT_DELAY=5
echo maison >"$work/state/name"

# check <nom> <valeur> <attendu>
check() {
	if [ "$2" != "$3" ]; then
		echo "ÉCHEC $1 : $2, attendu $3"
		failures=$((failures + 1))
	fi
}

# run <commande> : sortie dans $work/out, code de retour dans $code
run() {
	if sh "$SCRIPT" "$@" >"$work/out" 2>"$work/err"; then code=0; else code=$?; fi
}

run test
check "sans réseau choisi : code" "$code" 1
check "sans réseau choisi : erreur" "$(jq -r .error "$work/out")" not-configured

# Wi-Fi de la maison en place : tout va bien.
echo wifi >"$work/state/network-choice"
printf 'wlan0:wifi:connected:pi-appliance-wifi\nlo:loopback:connected (externally):lo\n' >"$fake/devices"
echo yes >"$fake/autoconnect"
touch "$fake/box" "$fake/internet" "$fake/mdns"
run test
check "Wi-Fi : code" "$code" 0
check "Wi-Fi : réussi" "$(jq -r .passed "$work/out")" true
check "Wi-Fi : nom du Wi-Fi" "$(jq -r .ssid "$work/out")" "Maison:Salon"
check "Wi-Fi : adresse" "$(jq -r '.addresses | join(",")' "$work/out")" 192.168.1.20
check "Wi-Fi : nom du Pi" "$(jq -r .name "$work/out")" maison
check "Wi-Fi : vérifications" "$(jq -c .checks "$work/out")" '{"connected":true,"address":true,"box":true,"internet":true,"saved":true,"localName":true}'
check "Wi-Fi : box essayée sur wlan0" "$(grep -c 'ping .*-I wlan0 192.168.1.1' "$fake/calls")" 1
check "Wi-Fi : adresse gardée" "$(jq -r '.addresses[0]' "$work/state/home-address")" 192.168.1.20
[ ! -e "$fake/forbidden" ] || { echo "ÉCHEC le test touche à la puce Wi-Fi : $(cat "$fake/forbidden")"; failures=$((failures + 1)); }

# Liste des Wi-Fi encore vide (aucune recherche depuis le démarrage) : le Pi
# reste relié au Wi-Fi de la maison, dont le nom vient du réglage enregistré.
touch "$fake/no-scan"
run test
check "sans recherche : nom du Wi-Fi" "$(jq -r .ssid "$work/out")" "Maison:Salon"
run status
check "état sans recherche : relié" "$(jq -r .wifi.connected "$work/out")" true
check "état sans recherche : nom du Wi-Fi" "$(jq -r .wifi.ssid "$work/out")" "Maison:Salon"
check "état sans recherche : adresse" "$(jq -r '.addresses | join(",")' "$work/out")" 192.168.1.20
rm -f "$fake/no-scan"

# Sans internet ni nom en .local : le test passe quand même (le Pi marche à la maison).
rm -f "$fake/internet" "$fake/mdns"
run test
check "sans internet : réussi" "$(jq -r .passed "$work/out")" true
check "sans internet : internet" "$(jq -r .checks.internet "$work/out")" false
check "sans internet : nom" "$(jq -r .checks.localName "$work/out")" false

# La box ne répond pas, ou le Wi-Fi ne serait pas retrouvé après un redémarrage.
rm -f "$fake/box"
run test
check "box muette : réussi" "$(jq -r .passed "$work/out")" false
check "box muette : box" "$(jq -r .checks.box "$work/out")" false
touch "$fake/box"
echo no >"$fake/autoconnect"
run test
check "non enregistré : réussi" "$(jq -r .passed "$work/out")" false
check "non enregistré : saved" "$(jq -r .checks.saved "$work/out")" false
echo yes >"$fake/autoconnect"

# Point d'accès rouvert sur la puce : le Pi n'est pas relié au Wi-Fi de la maison.
touch "$fake/ap"
printf 'wlan0:wifi:connected:pi-appliance-ap\n' >"$fake/devices"
run test
check "point d'accès : code" "$code" 0
check "point d'accès : relié" "$(jq -r .checks.connected "$work/out")" false
check "point d'accès : réussi" "$(jq -r .passed "$work/out")" false
check "point d'accès : nom du Wi-Fi" "$(jq -r .ssid "$work/out")" ""
[ ! -e "$fake/forbidden" ] || { echo "ÉCHEC le test touche à la puce Wi-Fi : $(cat "$fake/forbidden")"; failures=$((failures + 1)); }
rm -f "$fake/ap"

# Câble choisi : le Wi-Fi n'entre pas en compte.
echo wired >"$work/state/network-choice"
printf 'eth0:ethernet:connected:Wired connection 1\nwlan0:wifi:disconnected:\n' >"$fake/devices"
echo no >"$fake/autoconnect"
run test
check "câble : réussi" "$(jq -r .passed "$work/out")" true
check "câble : réseau" "$(jq -r .network "$work/out")" wired
check "câble : box essayée sur eth0" "$(grep -c 'ping .*-I eth0' "$fake/calls")" 1

# Redémarrage : programmé une fois, dans quelques secondes.
: >"$fake/calls"
run reboot
check "redémarrage : code" "$code" 0
check "redémarrage : délai" "$(jq -r .inSeconds "$work/out")" 5
check "redémarrage : programmé" "$(grep -c 'systemd-run .*--on-active=5 systemctl reboot' "$fake/calls")" 1
run reboot
check "redémarrage demandé deux fois : code" "$code" 0
check "redémarrage demandé deux fois : programmé une fois" "$(grep -c 'systemd-run' "$fake/calls")" 1
rm -f "$fake/timer"
touch "$fake/run-fails"
run reboot
check "redémarrage impossible : code" "$code" 1
check "redémarrage impossible : erreur" "$(jq -r .error "$work/out")" failed

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "network.sh test, status et reboot : tout est bon"
