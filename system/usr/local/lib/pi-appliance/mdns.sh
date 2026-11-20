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

# Adresses des services à la maison (spécification 4 et 6) : avahi n'annonce
# que <nom>.local, et mDNS n'a pas de caractère générique. Ce script annonce
# l'adresse de chaque service installé (<service>.<nom>.local, lue dans
# <données>/services/*/instance.json) vers l'adresse du Pi sur le réseau de la
# maison, un « avahi-publish -a -R » par nom. Il revérifie toutes les
# 15 secondes : service installé ou retiré, adresse IP ou nom du Pi changé,
# avahi relancé (name.sh set).
#
#   mdns.sh   lancé par pi-appliance-mdns.service

set -eu

SERVICES_DIR=${SERVICES_DIR:-/var/lib/pi-appliance/services}
INTERVAL=${MDNS_INTERVAL:-15}
PUBLISH_CMD=${PUBLISH_CMD:-avahi-publish}
HOSTNAME_CMD=${HOSTNAME_CMD:-hostname}
# 10.42.0.1 : le Wi-Fi de configuration (pi-appliance-ap.nmconnection), pas la maison.
AP_PREFIX=10.42.0.

publishers=""
published=""

# home_address : première adresse IPv4 du câble, sinon du Wi-Fi de la maison.
# Un nom mDNS ne peut être annoncé que vers une adresse par avahi-publish.
home_address() {
	for device in eth0 end0 eth1 wlan0; do
		ip -4 -o address show dev "$device" 2>/dev/null |
			awk '{ sub("/.*", "", $4); print $4 }' | grep -v "^$AP_PREFIX" | head -n 1
	done | head -n 1
}

# wanted : lignes « <nom> <adresse> » à annoncer.
wanted() {
	home=$(home_address)
	if [ -z "$home" ]; then return 0; fi
	own="$("$HOSTNAME_CMD").local"
	for state in "$SERVICES_DIR"/*/instance.json; do
		if [ -f "$state" ]; then
			jq -r 'select(.no_web_page != true) | .address // empty' "$state" 2>/dev/null || true
		fi
	done | grep -Ex '([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+local' | grep -vxF "$own" | sort -u |
		while read -r name; do printf '%s %s\n' "$name" "$home"; done
}

stop_all() {
	for pid in $publishers; do
		kill "$pid" 2>/dev/null || true
		wait "$pid" 2>/dev/null || true
	done
	publishers=""
}

# NE PAS se fier à « kill -0 » seul : un annonceur arrêté par la relance
# d'avahi reste zombie tant que le shell ne l'a pas ramassé.
alive() {
	for pid in $publishers; do
		state=$(awk '{ print $3 }' "/proc/$pid/stat" 2>/dev/null || true)
		if [ -z "$state" ] || [ "$state" = Z ]; then return 1; fi
	done
}

sync_once() {
	want=$(wanted)
	if [ "$want" = "$published" ] && alive; then return 0; fi
	stop_all
	published=$want
	if [ -z "$want" ]; then return 0; fi
	while read -r name target; do
		"$PUBLISH_CMD" -a -R "$name" "$target" >/dev/null 2>&1 &
		publishers="$publishers $!"
	done <<-LIST
		$want
	LIST
}

# Lu par « . » depuis les tests : les fonctions seulement.
# shellcheck disable=SC2317
if [ "${MDNS_LIB_ONLY:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

trap 'stop_all; exit 0' INT TERM
if ! command -v "$PUBLISH_CMD" >/dev/null 2>&1; then
	echo "annonce mDNS des services : $PUBLISH_CMD introuvable (paquet avahi-utils), aucune adresse annoncée" >&2
fi
while :; do
	sync_once || echo "annonce mDNS des services : vérification en échec" >&2
	sleep "$INTERVAL" &
	wait $! || true
done
