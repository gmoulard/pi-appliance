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

# Fonctions communes à ap.sh et network.sh : chemins, verrou du Wi-Fi,
# commandes NetworkManager. Bibliothèque lue par « . » depuis un script POSIX sh.

# shellcheck shell=sh disable=SC2034
set -eu

PI_RUN=${PI_RUN:-/run/pi-appliance}
PI_STATE=${PI_STATE:-/var/lib/pi-appliance/system}
PI_LIB=${PI_LIB:-/usr/local/lib/pi-appliance}
AP_CONNECTION=pi-appliance-ap        # /etc/NetworkManager/system-connections/pi-appliance-ap.nmconnection
WIFI_CONNECTION=pi-appliance-wifi    # Wi-Fi de la maison, créé par « network.sh wifi-join »
WIFI_DEVICE=${WIFI_DEVICE:-wlan0}
NETWORK_CHOICE=$PI_STATE/network-choice  # « wifi » ou « wired », choisi dans le portail
SCAN_FILE=$PI_RUN/wifi-scan.json
STATUS_FILE=$PI_RUN/ap-status
ERROR_FILE=$PI_RUN/network-error
COUNTRY_FILE=$PI_STATE/wifi-country  # pays du Wi-Fi choisi dans le portail
HOME_FILE=$PI_STATE/home-address     # dernière adresse du Pi sur le réseau de la maison (TODO-2.58)

# Une seule commande à la fois sur la puce Wi-Fi (point d'accès, recherche, connexion).
# with_wifi_lock <commande…>
with_wifi_lock() {
	mkdir -p "$PI_RUN"
	exec 9>"$PI_RUN/wifi.lock"
	flock 9
	if "$@"; then status=0; else status=$?; fi
	flock -u 9
	return "$status"
}

uptime_seconds() {
	cut -d. -f1 /proc/uptime
}

is_configured() {
	[ -s "$NETWORK_CHOICE" ]
}

# Réseau actif, hors point d'accès : câble branché et connecté, ou Wi-Fi de la maison.
has_uplink() {
	nmcli -t -f TYPE,STATE,CONNECTION device status 2>/dev/null |
		awk -F: -v ap="$AP_CONNECTION" '
			($1 == "ethernet" || $1 == "wifi") && $2 == "connected" && $3 != ap { found = 1 }
			END { exit !found }'
}

has_ethernet() {
	nmcli -t -f TYPE,STATE device status 2>/dev/null |
		awk -F: '$1 == "ethernet" && $2 == "connected" { found = 1 } END { exit !found }'
}

ap_is_open() {
	nmcli -t -f NAME connection show --active 2>/dev/null | grep -qx "$AP_CONNECTION"
}

ap_open() {
	nmcli --wait 30 connection up "$AP_CONNECTION" >/dev/null
}

ap_close() {
	nmcli --wait 30 connection down "$AP_CONNECTION" >/dev/null 2>&1 || true
}

# Nombre d'appareils connectés au point d'accès.
ap_clients() {
	iw dev "$WIFI_DEVICE" station dump 2>/dev/null | grep -c '^Station' || true
}

# Recherche des Wi-Fi visibles, enregistrée dans $SCAN_FILE (tableau JSON).
# La puce ne cherche pas pendant que le point d'accès est ouvert : l'appelant le ferme avant.
wifi_scan() {
	mkdir -p "$PI_RUN"
	nmcli -t -e yes -f SECURITY,SIGNAL,SSID device wifi list ifname "$WIFI_DEVICE" --rescan yes 2>/dev/null |
		wifi_scan_json "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$SCAN_FILE.tmp"
	mv "$SCAN_FILE.tmp" "$SCAN_FILE"
}

# Liste de nmcli (-t -e yes -f SECURITY,SIGNAL,SSID) vers JSON. Le nom du réseau
# vient en dernier : il peut contenir « : », écrit « \: » par nmcli.
# wifi_scan_json <date>
wifi_scan_json() {
	jq -R -s --arg at "$1" '
		split("\n")
		| map(capture("^(?<security>[^:]*):(?<signal>[0-9]*):(?<ssid>.*)$")
			| .ssid |= gsub("\\\\(?<c>.)"; .c)
			| .signal |= (tonumber? // 0)
			| .security |= (if . == "" or . == "--" then "open"
				elif test("802\\.1X") then "enterprise"
				elif test("WPA3") and (test("WPA2|WPA1") | not) then "wpa3"
				else "wpa" end))
		| map(select(.ssid != ""))
		| group_by(.ssid) | map(max_by(.signal))
		| sort_by(-.signal)
		| {scannedAt: $at, networks: .}'
}

# Nom du Wi-Fi de la maison auquel la puce est reliée ; rien si elle ne l'est pas
# (ou si elle sert au point d'accès). L'état de la puce fait foi : la liste des
# Wi-Fi de nmcli (colonne ACTIVE) reste vide tant qu'aucune recherche n'a eu lieu
# depuis le démarrage, le nom vient alors du réglage enregistré.
home_wifi_ssid() {
	_hw_connection=$(nmcli -t -f DEVICE,TYPE,STATE,CONNECTION device status 2>/dev/null |
		awk -F: -v device="$WIFI_DEVICE" -v ap="$AP_CONNECTION" '$1 == device && $2 == "wifi" && $3 == "connected" && $4 != ap { print $4; exit }')
	[ -n "$_hw_connection" ] || return 0
	_hw_ssid=$(nmcli -t -f ACTIVE,SSID device wifi list ifname "$WIFI_DEVICE" --rescan no 2>/dev/null |
		sed -n 's/^yes://p' | head -n 1 | sed 's/\\\(.\)/\1/g')
	[ -n "$_hw_ssid" ] || _hw_ssid=$(nmcli -g 802-11-wireless.ssid connection show "$_hw_connection" 2>/dev/null || true)
	[ -n "$_hw_ssid" ] || _hw_ssid=$_hw_connection
	printf '%s\n' "$_hw_ssid"
}

# Adresses IPv4 du Pi sur le réseau de la maison (hors point d'accès et Docker).
addresses() {
	nmcli -t -f DEVICE,TYPE,STATE,CONNECTION device status 2>/dev/null |
		awk -F: -v ap="$AP_CONNECTION" '($2 == "ethernet" || $2 == "wifi") && $3 == "connected" && $4 != ap { print $1 }' |
		while read -r device; do
			nmcli -g IP4.ADDRESS device show "$device" 2>/dev/null | tr '|' '\n' | sed 's/ //g; s:/.*::'
		done | sed '/^$/d'
}

# remember_home : garde le nom et les adresses du Pi sur le réseau de la maison,
# pour les redonner si le Wi-Fi de configuration se rouvre (adresse inconnue
# avant la bascule, spécification 4).
remember_home() {
	list=$(addresses)
	[ -n "$list" ] || return 0
	name=$("$PI_LIB/name.sh" get 2>/dev/null || hostname)
	home=$(printf '%s\n' "$list" | jq -R -s -c --arg name "$name" '{name: $name, addresses: (split("\n") | map(select(length > 0)))}')
	if [ "$home" = "$(cat "$HOME_FILE" 2>/dev/null || true)" ]; then
		return 0
	fi
	mkdir -p "$PI_STATE"
	printf '%s\n' "$home" >"$HOME_FILE.tmp"
	mv "$HOME_FILE.tmp" "$HOME_FILE"
}
