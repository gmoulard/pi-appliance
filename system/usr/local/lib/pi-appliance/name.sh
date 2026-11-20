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

# Nom du Pi sur le réseau (spécification 4) : nom d'hôte, adresse <nom>.local
# et nom du Wi-Fi de configuration. Propre à chaque Pi, pour que plusieurs
# appliances vivent dans la même maison (TODO-2.48) : pi-appliance-XXXX au
# premier démarrage (fin de l'adresse matérielle, stable d'un démarrage à
# l'autre), modifiable ensuite dans la console.
#
#   name.sh boot        au démarrage, avant NetworkManager et avahi (pi-appliance-secrets.service)
#   name.sh get         nom actuel
#   name.sh set <nom>   nouveau nom, choisi dans la console (par « network.sh name »)
#
# Un nom : 1 à 32 caractères, minuscules, chiffres et tirets, ni au début ni à
# la fin (nom d'hôte et nom de Wi-Fi valides).

set -eu

PI_STATE=${PI_STATE:-/var/lib/pi-appliance/system}
PI_LIB=${PI_LIB:-/usr/local/lib/pi-appliance}
NAME_FILE=$PI_STATE/name
AP_KEYFILE=${AP_KEYFILE:-/etc/NetworkManager/system-connections/pi-appliance-ap.nmconnection}
HOSTNAME_FILE=${HOSTNAME_FILE:-/etc/hostname}
HOSTS_FILE=${HOSTS_FILE:-/etc/hosts}
NET_CLASS=${NET_CLASS:-/sys/class/net}
HOSTNAME_CMD=${HOSTNAME_CMD:-hostname}

valid_name() {
	printf '%s\n' "$1" | grep -Eqx '[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?'
}

# default_name : « pi-appliance- » et les 4 derniers chiffres de l'adresse
# matérielle du Wi-Fi (ou du câble) ; au hasard sans carte réseau.
default_name() {
	for device in wlan0 eth0 end0; do
		if [ -r "$NET_CLASS/$device/address" ]; then
			suffix=$(tr -d ':\n' <"$NET_CLASS/$device/address" | tr 'A-F' 'a-f' | tail -c 4)
			if printf '%s\n' "$suffix" | grep -Eqx '[0-9a-f]{4}' && [ "$suffix" != 0000 ]; then
				echo "pi-appliance-$suffix"
				return 0
			fi
		fi
	done
	echo "pi-appliance-$(od -An -N2 -tx1 /dev/urandom | tr -d ' \n')"
}

current_name() {
	if [ -s "$NAME_FILE" ] && valid_name "$(cat "$NAME_FILE")"; then
		cat "$NAME_FILE"
	else
		"$HOSTNAME_CMD"
	fi
}

# apply_name <nom> : nom d'hôte, /etc/hosts et nom du Wi-Fi de configuration.
apply_name() {
	printf '%s\n' "$1" >"$HOSTNAME_FILE"
	if grep -q '^127\.0\.1\.1[[:space:]]' "$HOSTS_FILE"; then
		sed -i "s/^127\.0\.1\.1[[:space:]].*/127.0.1.1	$1/" "$HOSTS_FILE"
	else
		printf '127.0.1.1\t%s\n' "$1" >>"$HOSTS_FILE"
	fi
	if [ -f "$AP_KEYFILE" ]; then
		sed -i "s/^ssid=.*/ssid=$1/" "$AP_KEYFILE"
	fi
	"$HOSTNAME_CMD" "$1"
}

save_name() {
	mkdir -p "$PI_STATE"
	printf '%s\n' "$1" >"$NAME_FILE.tmp"
	mv "$NAME_FILE.tmp" "$NAME_FILE"
}

boot() {
	if [ -s "$NAME_FILE" ] && valid_name "$(cat "$NAME_FILE")"; then
		name=$(cat "$NAME_FILE")
	else
		name=$(default_name)
		save_name "$name"
	fi
	apply_name "$name"
	echo "nom du Pi : $name"
}

set_name() {
	if ! valid_name "$1"; then
		echo "nom invalide : $1" >&2
		exit 2
	fi
	save_name "$1"
	apply_name "$1"
	# NetworkManager relit le Wi-Fi de configuration ; s'il est ouvert, il
	# repart sous le nouveau nom (le téléphone doit s'y reconnecter).
	if command -v nmcli >/dev/null 2>&1; then
		nmcli connection reload >/dev/null 2>&1 || true
		if nmcli -t -f NAME connection show --active 2>/dev/null | grep -qx pi-appliance-ap; then
			nmcli --wait 30 connection up pi-appliance-ap >/dev/null 2>&1 || true
		fi
	fi
	# Certificat local refait pour le nouveau nom (secrets.sh le voit), puis
	# avahi et Traefik relancés quelques secondes après la réponse à la console.
	"$PI_LIB/secrets.sh" >/dev/null
	if command -v systemd-run >/dev/null 2>&1; then
		systemd-run --quiet --collect --on-active=5 \
			/bin/systemctl restart avahi-daemon.service pi-appliance-traefik.service >/dev/null 2>&1 || true
	fi
	echo "$1"
}

# Lu par « . » depuis les tests : les fonctions seulement.
# shellcheck disable=SC2317
if [ "${NAME_LIB_ONLY:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

case "${1:-}" in
boot) boot ;;
get) current_name ;;
set) set_name "${2:-}" ;;
*)
	echo "usage : name.sh boot | get | set <nom>" >&2
	exit 2
	;;
esac
