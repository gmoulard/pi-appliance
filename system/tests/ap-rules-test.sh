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

# Tests des règles du point d'accès (ap-rules.sh) et de la lecture des Wi-Fi
# visibles (common.sh). Sans Pi ni NetworkManager : sh system/tests/ap-rules-test.sh

# shellcheck source-path=SCRIPTDIR disable=SC2034
set -eu

LIB=$(dirname "$0")/../usr/local/lib/pi-appliance
failures=0

reset() {
	AP_WINDOW_START=
	AP_OFFLINE_SINCE=
	AP_EVER_CONNECTED=0
}

# expect <nom> <maintenant> <configuré> <réseau> <attendu> [raison]
expect() {
	ap_step "$2" "$3" "$4"
	if [ "$AP_WANT" != "$5" ] || { [ $# -ge 6 ] && [ "$AP_REASON" != "$6" ]; }; then
		echo "ÉCHEC $1 : $AP_WANT/$AP_REASON, attendu $5${6:+/$6}"
		failures=$((failures + 1))
	fi
}

# shellcheck source=../usr/local/lib/pi-appliance/ap-rules.sh
. "$LIB/ap-rules.sh"

# Premier démarrage, sans réseau choisi : ouvert tout de suite, pour 24 heures.
reset
expect "premier démarrage" 10 0 0 open not-connected
[ "$AP_CLOSES_IN" -eq 86400 ] || { echo "ÉCHEC fermeture dans $AP_CLOSES_IN s"; failures=$((failures + 1)); }
expect "après 23 h" $((10 + 86399)) 0 0 open
expect "après 24 h" $((10 + 86400)) 0 0 closed timeout
expect "reste fermé" $((10 + 90000)) 0 0 closed timeout

# Câble branché mais réseau pas encore choisi dans le portail : reste ouvert.
reset
expect "câble, pas choisi" 10 0 1 open not-connected

# Réseau choisi et actif : fermé.
expect "réseau choisi" 300 1 1 closed connected

# Réseau perdu moins de 10 minutes : reste fermé ; au-delà, rouvert pour 24 heures.
expect "perte courte" 400 1 0 closed waiting-network
expect "perte 9 min" $((400 + 599)) 1 0 closed waiting-network
expect "perte 10 min" $((400 + 600)) 1 0 open not-connected
expect "rouvert 23 h" $((1000 + 86399)) 1 0 open
expect "rouvert 24 h" $((1000 + 86400)) 1 0 closed timeout
expect "réseau revenu" $((1000 + 90000)) 1 1 closed connected
expect "reperdu" $((1000 + 90001)) 1 0 closed waiting-network

# Redémarrage d'un Pi configuré : 2 minutes pour retrouver le réseau, sinon ouvert.
reset
expect "démarrage configuré" 5 1 0 closed waiting-network
expect "réseau à temps" 60 1 1 closed connected
reset
expect "démarrage configuré" 5 1 0 closed waiting-network
expect "pas de réseau après 2 min" 125 1 0 open not-connected

# Lecture des Wi-Fi visibles.
# shellcheck source=../usr/local/lib/pi-appliance/common.sh
. "$LIB/common.sh"
got=$(printf '%s\n' 'WPA2:80:Maison' 'WPA1 WPA2:30:Maison' ':55:Café\:libre' 'WPA3:40:Sûr' \
	'WPA2 802.1X:20:Bureau' 'WPA2:12:' 'WPA2 WPA3:70:a\\b' | wifi_scan_json T | jq -c .networks)
want='[{"security":"wpa","signal":80,"ssid":"Maison"},{"security":"wpa","signal":70,"ssid":"a\\b"},{"security":"open","signal":55,"ssid":"Café:libre"},{"security":"wpa3","signal":40,"ssid":"Sûr"},{"security":"enterprise","signal":20,"ssid":"Bureau"}]'
if [ "$got" != "$want" ]; then
	echo "ÉCHEC liste des Wi-Fi :"
	echo "  obtenu  $got"
	echo "  attendu $want"
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "ok"
