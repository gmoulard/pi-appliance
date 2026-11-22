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

# Réseau du Pi, pour la console : état, Wi-Fi visibles, connexion au Wi-Fi de la
# maison, choix du câble. Toujours un objet JSON sur la sortie standard ; code de
# retour 0 si la demande a réussi, 1 sinon (champ « error »).
#
#   network.sh status       état du point d'accès, du câble et du Wi-Fi
#   network.sh wifi-list    dernière liste des Wi-Fi visibles
#   network.sh wifi-scan    nouvelle recherche (ferme le point d'accès quelques secondes)
#   network.sh wifi-join    connexion ; entrée standard : {"ssid": "…", "password": "…", "hidden": false}
#   network.sh wired        garder le câble comme réseau du Pi
#   network.sh country FR   pays du Wi-Fi (code ISO 3166 à deux lettres)
#   network.sh name salon   nom du Pi : <nom>.local et nom du Wi-Fi de configuration (name.sh)
#   network.sh test         vérification du réseau choisi, sans couper le point d'accès
#   network.sh reboot       redémarrage du Pi dans quelques secondes
#
# Le mot de passe du Wi-Fi n'apparaît jamais dans la ligne de commande d'un
# programme (visible de tous dans /proc) : il passe par l'entrée standard.

# shellcheck source-path=SCRIPTDIR
set -eu

PI_LIB=${PI_LIB:-/usr/local/lib/pi-appliance}
REBOOT_DELAY=${REBOOT_DELAY:-5}
# shellcheck source=common.sh
. "$PI_LIB/common.sh"

# fail <code> <message>
fail() {
	jq -n --arg error "$1" --arg message "$2" '{ok: false, error: $error, message: $message}'
	exit 1
}

wifi_list() {
	if [ -s "$SCAN_FILE" ]; then
		cat "$SCAN_FILE"
	else
		jq -n '{scannedAt: null, networks: []}'
	fi
}

status() {
	state=closed
	reason=''
	closes_in=0
	if [ -f "$STATUS_FILE" ]; then
		# shellcheck source=/dev/null
		. "$STATUS_FILE"
	fi
	if ap_is_open; then state=open; fi
	wired=false
	if has_ethernet; then wired=true; fi
	ssid=$(nmcli -t -f ACTIVE,SSID device wifi list ifname "$WIFI_DEVICE" --rescan no 2>/dev/null |
		sed -n 's/^yes://p' | head -n 1 | sed 's/\\\(.\)/\1/g')
	if ap_is_open; then ssid=; fi
	choice=$(cat "$NETWORK_CHOICE" 2>/dev/null || true)
	country=$(wifi_country)
	error=$(cat "$ERROR_FILE" 2>/dev/null || true)
	last_home=$(jq -c 'select(type == "object")' "$HOME_FILE" 2>/dev/null || true)
	[ -n "$last_home" ] || last_home=null
	name=$("$PI_LIB/name.sh" get 2>/dev/null || hostname)
	addresses | jq -R -s \
		--argjson open "$([ "$state" = open ] && echo true || echo false)" \
		--arg reason "$reason" --argjson closesIn "${closes_in:-0}" \
		--arg choice "$choice" --argjson wired "$wired" --arg ssid "$ssid" --arg error "$error" --arg country "$country" --argjson countryChosen "$([ -s "$COUNTRY_FILE" ] && echo true || echo false)" --arg name "$name" --argjson lastHome "$last_home" '
		{
			ok: true,
			name: $name,
			accessPoint: {open: $open, ssid: $name, reason: $reason,
				closesInSeconds: (if $open then $closesIn else 0 end)},
			configured: $choice,
			wired: {connected: $wired},
			wifi: {connected: ($ssid != ""), ssid: $ssid},
			addresses: (split("\n") | map(select(length > 0))),
			country: $country,
			countryChosen: $countryChosen,
			lastHome: $lastHome,
			lastError: $error
		}'
}

wifi_rescan() {
	was_open=0
	if ap_is_open; then was_open=1; ap_close; fi
	wifi_scan || true
	if [ "$was_open" = 1 ]; then ap_open || true; fi
}

# Erreur de nmcli vers un code pour la console.
join_error() {
	case "$1" in
	*[Ss]ecrets*|*802-1[xX]*|*[Pp]assword*|*psk*) echo wrong-password ;;
	*"No network with SSID"*|*"not found"*) echo not-found ;;
	*[Tt]imeout*|*[Tt]imed\ out*) echo timeout ;;
	*) echo failed ;;
	esac
}

wifi_join() {
	input=$(cat)
	ssid=$(printf '%s' "$input" | jq -er '.ssid | strings | select(length > 0 and utf8bytelength <= 32)' 2>/dev/null) ||
		fail invalid-ssid "Nom de Wi-Fi manquant ou trop long (32 octets au plus)."
	password=$(printf '%s' "$input" | jq -r '.password // "" | strings' 2>/dev/null) ||
		fail invalid-password "Mot de passe illisible."
	hidden=$(printf '%s' "$input" | jq -r 'if .hidden == true then "yes" else "no" end' 2>/dev/null) || hidden=no
	if [ -n "$password" ]; then
		case "$password" in
		*"
"*) fail invalid-password "Le mot de passe ne peut pas contenir de retour à la ligne." ;;
		esac
		length=$(printf '%s' "$password" | wc -c)
		if [ "$length" -lt 8 ] || [ "$length" -gt 64 ]; then
			fail invalid-password "Le mot de passe d'un Wi-Fi fait de 8 à 63 caractères."
		fi
	fi
	security=$(wifi_list | jq -r --arg ssid "$ssid" '.networks[] | select(.ssid == $ssid) | .security' | head -n 1)
	if [ "$security" = enterprise ]; then
		fail unsupported "Ce Wi-Fi demande un identifiant d'entreprise : il n'est pas encore pris en charge."
	fi
	if [ "$security" = wpa3 ]; then key_mgmt=sae; else key_mgmt=wpa-psk; fi

	new=$WIFI_CONNECTION-new
	nmcli connection delete "$new" >/dev/null 2>&1 || true
	nmcli connection add type wifi con-name "$new" ifname "$WIFI_DEVICE" ssid "$ssid" \
		wifi.hidden "$hidden" connection.autoconnect no >/dev/null ||
		fail failed "Impossible de préparer la connexion à ce Wi-Fi."
	if [ -n "$password" ]; then
		# printf est une commande interne du shell : le mot de passe ne passe par
		# aucune ligne de commande. autoconnect=no : nmcli enregistre sans poser de question.
		printf 'set wifi-sec.key-mgmt %s\nset wifi-sec.psk %s\nsave persistent\nquit\n' "$key_mgmt" "$password" |
			nmcli connection edit "$new" >/dev/null 2>&1 ||
			{ nmcli connection delete "$new" >/dev/null 2>&1 || true; fail failed "Impossible d'enregistrer le mot de passe."; }
	fi

	was_open=0
	if ap_is_open; then was_open=1; ap_close; fi
	if output=$(nmcli --wait 45 connection up "$new" 2>&1); then
		nmcli connection delete "$WIFI_CONNECTION" >/dev/null 2>&1 || true
		nmcli connection modify "$new" connection.id "$WIFI_CONNECTION" connection.autoconnect yes >/dev/null
		mkdir -p "$PI_STATE"
		echo wifi >"$NETWORK_CHOICE"
		rm -f "$ERROR_FILE"
		jq -n --arg ssid "$ssid" '{ok: true, ssid: $ssid}'
		return 0
	fi
	code=$(join_error "$output")
	nmcli connection delete "$new" >/dev/null 2>&1 || true
	echo "$code" >"$ERROR_FILE"
	# Revenir comme avant : Wi-Fi précédent, ou point d'accès pour réessayer.
	nmcli --wait 30 connection up "$WIFI_CONNECTION" >/dev/null 2>&1 || true
	if [ "$was_open" = 1 ] || ! is_configured; then ap_open || true; fi
	case "$code" in
	wrong-password) fail "$code" "Le Wi-Fi a refusé le mot de passe." ;;
	not-found) fail "$code" "Ce Wi-Fi est introuvable : est-il allumé et assez proche ?" ;;
	timeout) fail "$code" "Le Wi-Fi ne répond pas." ;;
	*) fail "$code" "La connexion à ce Wi-Fi a échoué." ;;
	esac
}

wired() {
	if ! has_ethernet; then
		fail no-cable "Aucun câble réseau branché, ou la box ne répond pas."
	fi
	mkdir -p "$PI_STATE"
	echo wired >"$NETWORK_CHOICE"
	rm -f "$ERROR_FILE"
	"$PI_LIB/name.sh" check >/dev/null 2>&1 || true
	jq -n --arg name "$("$PI_LIB/name.sh" get 2>/dev/null || hostname)" '{ok: true, name: $name}'
}

# Pays du Wi-Fi : celui choisi dans le portail, sinon celui du noyau (FR dans l'image).
wifi_country() {
	if [ -s "$COUNTRY_FILE" ]; then
		cat "$COUNTRY_FILE"
		return
	fi
	iw reg get 2>/dev/null | sed -n 's/^country \([A-Z][A-Z]\):.*/\1/p' | head -n 1
}

# Le pays fixe les canaux et la puissance permis. Le point d'accès reste sur le
# canal 6, permis partout : changer de pays ne coupe pas le portail.
set_country() {
	code=${1:-}
	case "$code" in
	[A-Z][A-Z]) ;;
	*) fail invalid-country "Code de pays invalide : deux lettres majuscules, comme FR." ;;
	esac
	if command -v raspi-config >/dev/null 2>&1; then
		# Garde le pays après un redémarrage (cfg80211.ieee80211_regdom).
		raspi-config nonint do_wifi_country "$code" >/dev/null 2>&1 ||
			fail failed "Impossible de changer le pays du Wi-Fi."
	else
		iw reg set "$code" 2>/dev/null || fail failed "Impossible de changer le pays du Wi-Fi."
	fi
	mkdir -p "$PI_STATE"
	echo "$code" >"$COUNTRY_FILE"
	jq -n --arg country "$code" '{ok: true, country: $country}'
}

# Nom du Pi (name.sh) : <nom>.local, nom du Wi-Fi de configuration, certificat local.
set_name() {
	if new=$("$PI_LIB/name.sh" set "${1:-}" 2>/dev/null); then
		jq -n --arg name "$new" '{ok: true, name: $name}'
		return 0
	else
		code=$?
	fi
	if [ "$code" = 2 ]; then
		fail invalid-name "Ce nom ne convient pas : lettres minuscules, chiffres et tirets, 32 caractères au plus, sans tiret au début ni à la fin."
	fi
	fail failed "Le nom du Pi n'a pas pu être changé."
}

# Vérification du réseau choisi dans le portail (TODO-2.11). Elle ne touche
# jamais à la puce Wi-Fi : elle regarde la connexion déjà établie.
# NE PAS fermer le point d'accès ni relancer de recherche ici : la page qui
# demande le test peut passer par ce point d'accès (même puce Wi-Fi).
network_test() {
	choice=$(cat "$NETWORK_CHOICE" 2>/dev/null || true)
	case "$choice" in
	wifi) type=wifi ;;
	wired) type=ethernet ;;
	*) fail not-configured "Le Pi n'est pas encore relié à votre réseau : choisissez d'abord le Wi-Fi ou le câble." ;;
	esac
	device=$(nmcli -t -f DEVICE,TYPE,STATE,CONNECTION device status 2>/dev/null |
		awk -F: -v type="$type" -v ap="$AP_CONNECTION" '$2 == type && $3 == "connected" && $4 != ap { print $1; exit }')
	ssid=''
	if [ "$choice" = wifi ] && ! ap_is_open; then
		ssid=$(nmcli -t -f ACTIVE,SSID device wifi list ifname "$WIFI_DEVICE" --rescan no 2>/dev/null |
			sed -n 's/^yes://p' | head -n 1 | sed 's/\\\(.\)/\1/g')
	fi
	address=''
	gateway=''
	if [ -n "$device" ]; then
		address=$(nmcli -g IP4.ADDRESS device show "$device" 2>/dev/null | tr '|' '\n' | sed 's/ //g; s:/.*::' | sed '/^$/d' | head -n 1)
		gateway=$(nmcli -g IP4.GATEWAY device show "$device" 2>/dev/null | sed 's/ //g; /^--$/d' | head -n 1)
	fi
	box=false
	if [ -n "$gateway" ] && ping -c 2 -W 2 -I "$device" "$gateway" >/dev/null 2>&1; then box=true; fi
	internet=false
	if [ -n "$address" ] && getent ahostsv4 www.pi-appliance.com >/dev/null 2>&1; then internet=true; fi
	saved=true
	if [ "$choice" = wifi ]; then
		[ "$(nmcli -g connection.autoconnect connection show "$WIFI_CONNECTION" 2>/dev/null)" = yes ] || saved=false
	fi
	name=$("$PI_LIB/name.sh" get 2>/dev/null || hostname)
	local_name=false
	if [ -n "$address" ] &&
		[ "$(avahi-resolve-host-name -4 "$name.local" 2>/dev/null | awk '{ print $2; exit }')" = "$address" ]; then
		local_name=true
	fi
	if [ -n "$address" ]; then remember_home || true; fi
	jq -n --arg network "$choice" --arg ssid "$ssid" --arg name "$name" --arg address "$address" \
		--argjson connected "$([ -n "$device" ] && echo true || echo false)" \
		--argjson box "$box" --argjson internet "$internet" --argjson saved "$saved" --argjson localName "$local_name" '
		{
			ok: true,
			network: $network,
			ssid: $ssid,
			name: $name,
			addresses: (if $address == "" then [] else [$address] end),
			checks: {connected: $connected, address: ($address != ""), box: $box,
				internet: $internet, saved: $saved, localName: $localName},
			passed: ($connected and $address != "" and $box and $saved)
		}'
}

# Redémarrage demandé par la console à la fin de la mise en route (TODO-2.12) :
# dans quelques secondes, pour que la réponse arrive d'abord au navigateur.
reboot_pi() {
	if ! systemctl is-active --quiet pi-appliance-reboot.timer 2>/dev/null; then
		remember_home || true
		sync
		systemd-run --quiet --unit=pi-appliance-reboot --on-active="$REBOOT_DELAY" systemctl reboot >/dev/null 2>&1 ||
			fail failed "Le Pi n'a pas pu redémarrer. Débranchez-le puis rebranchez-le."
	fi
	jq -n --argjson seconds "$REBOOT_DELAY" '{ok: true, inSeconds: $seconds}'
}

umask 077
case "${1:-}" in
status) status ;;
wifi-list) wifi_list ;;
wifi-scan) with_wifi_lock wifi_rescan; wifi_list ;;
wifi-join) with_wifi_lock wifi_join ;;
wired) wired ;;
country) set_country "${2:-}" ;;
name) set_name "${2:-}" ;;
test) network_test ;;
reboot) reboot_pi ;;
*) fail usage "usage : network.sh status | wifi-list | wifi-scan | wifi-join | wired | country <code> | name <nom> | test | reboot" ;;
esac
