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

# Point d'accès Wi-Fi « pi-appliance » : l'ouvre et le ferme selon les règles
# d'ap-rules.sh (spécification, section 4). Lancé par pi-appliance-ap.service.

# shellcheck source-path=SCRIPTDIR
set -eu

PI_LIB=${PI_LIB:-/usr/local/lib/pi-appliance}
# shellcheck source=common.sh
. "$PI_LIB/common.sh"
# shellcheck source=ap-rules.sh
. "$PI_LIB/ap-rules.sh"

INTERVAL=${AP_INTERVAL:-15}  # secondes entre deux vérifications
RETRY=${AP_RETRY:-600}       # Wi-Fi de la maison retenté toutes les 10 minutes, si personne n'est connecté au point d'accès
STATE_FILE=$PI_RUN/ap-state  # état des règles, perdu au redémarrage : la fenêtre de 24 heures repart
last_retry=0
name_checked=0

save_state() {
	cat >"$STATE_FILE.tmp" <<-STATE
		AP_WINDOW_START=$AP_WINDOW_START
		AP_OFFLINE_SINCE=$AP_OFFLINE_SINCE
		AP_EVER_CONNECTED=$AP_EVER_CONNECTED
	STATE
	mv "$STATE_FILE.tmp" "$STATE_FILE"
	# Lu par « network.sh status » pour la console.
	cat >"$STATUS_FILE.tmp" <<-STATUS
		state=$1
		reason=$AP_REASON
		closes_in=$AP_CLOSES_IN
	STATUS
	mv "$STATUS_FILE.tmp" "$STATUS_FILE"
}

step() {
	now=$(uptime_seconds)
	configured=0
	uplink=0
	if is_configured; then configured=1; fi
	if has_uplink; then uplink=1; fi
	ap_step "$now" "$configured" "$uplink"
	if [ "$AP_WANT" = open ]; then
		# Pi configuré en Wi-Fi mais réseau perdu : la puce sert le point d'accès et ne
		# voit plus le Wi-Fi de la maison. Le retenter de temps en temps, si personne
		# n'utilise le point d'accès.
		if [ "$(cat "$NETWORK_CHOICE" 2>/dev/null || true)" = wifi ] &&
			[ $((now - last_retry)) -ge "$RETRY" ] && [ "$(ap_clients)" -eq 0 ]; then
			last_retry=$now
			ap_close
			if nmcli --wait 40 connection up "$WIFI_CONNECTION" >/dev/null 2>&1; then
				ap_step "$(uptime_seconds)" 1 1
				save_state closed
				return 0
			fi
		fi
		if ! ap_is_open; then
			# Dernière liste des Wi-Fi visibles, pour le portail, avant d'occuper la puce.
			wifi_scan || true
			ap_open || echo "point d'accès : ouverture impossible" >&2
		fi
		save_state open
	else
		if ap_is_open; then
			ap_close
		fi
		save_state closed
		# Une fois par démarrage, dès que le Pi est sur le réseau de la maison :
		# nom <nom>.local déjà pris par un autre appareil ? (name.sh check)
		if [ "$name_checked" = 0 ] && [ "$uplink" = 1 ]; then
			name_checked=1
			"$PI_LIB/name.sh" check >/dev/null || echo "nom du Pi : vérification en échec" >&2
		fi
	fi
}

mkdir -p "$PI_RUN"
if [ -f "$STATE_FILE" ]; then
	# shellcheck source=/dev/null
	. "$STATE_FILE"
fi
# La puce Wi-Fi peut apparaître quelques secondes après NetworkManager.
tries=0
until nmcli -t -f DEVICE device status 2>/dev/null | grep -qx "$WIFI_DEVICE"; do
	tries=$((tries + 1))
	if [ "$tries" -ge 12 ]; then
		echo "pas de Wi-Fi ($WIFI_DEVICE) : point d'accès désactivé" >&2
		exit 0
	fi
	sleep 5
done
nmcli radio wifi on >/dev/null 2>&1 || true
while :; do
	with_wifi_lock step || echo "point d'accès : vérification en échec" >&2
	sleep "$INTERVAL"
done
