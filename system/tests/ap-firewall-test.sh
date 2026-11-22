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

# Test du pare-feu du Wi-Fi de configuration (ap-firewall.nft), sans Pi :
# un client du point d'accès, le Pi qui partage sa connexion comme
# NetworkManager (« ipv4.method=shared » : transfert et NAT) et la box, dans
# trois espaces réseau dédiés ; rien ne change sur la machine qui le lance.
# Sans le pare-feu, le client atteint la box ; avec lui, seulement le Pi.
#
#   sudo sh system/tests/ap-firewall-test.sh     (root, ip, nft, ping)

set -eu

RULES=$(cd "$(dirname "$0")/.." && pwd)/etc/pi-appliance/ap-firewall.nft
failures=0

cleanup() {
	for ns in pitest-client pitest-pi pitest-box; do ip netns del "$ns" 2>/dev/null || true; done
}
trap cleanup EXIT
cleanup

pi() { ip netns exec pitest-pi "$@"; }
for ns in pitest-client pitest-pi pitest-box; do ip netns add "$ns"; done

# Client du point d'accès (10.42.0.2) <-> Pi (10.42.0.1).
ip link add pt-ap type veth peer name pt-client
ip link set pt-ap netns pitest-pi
ip link set pt-client netns pitest-client
pi ip addr add 10.42.0.1/24 dev pt-ap
pi ip link set pt-ap up
ip netns exec pitest-client ip addr add 10.42.0.2/24 dev pt-client
ip netns exec pitest-client ip link set pt-client up
ip netns exec pitest-client ip route add default via 10.42.0.1

# Pi (192.168.1.196) <-> box (192.168.1.1).
ip link add pt-eth type veth peer name pt-box
ip link set pt-eth netns pitest-pi
ip link set pt-box netns pitest-box
pi ip addr add 192.168.1.196/24 dev pt-eth
pi ip link set pt-eth up
ip netns exec pitest-box ip addr add 192.168.1.1/24 dev pt-box
ip netns exec pitest-box ip link set pt-box up

# Partage de connexion, comme NetworkManager : transfert et NAT.
pi sh -c "echo 1 >/proc/sys/net/ipv4/ip_forward"
pi nft add table ip nm-shared
pi nft add chain ip nm-shared post '{ type nat hook postrouting priority 100; }'
pi nft add rule ip nm-shared post ip saddr 10.42.0.0/24 oifname pt-eth masquerade
pi nft add chain ip nm-shared allow '{ type filter hook forward priority 0; policy accept; }'
pi nft add rule ip nm-shared allow iifname pt-ap accept

reach() {
	ip netns exec pitest-client ping -c 1 -W 1 "$1" >/dev/null 2>&1
}

# expect <nom> <adresse> <oui|non>
expect() {
	if reach "$2"; then got=oui; else got=non; fi
	if [ "$got" != "$3" ]; then
		echo "ÉCHEC $1 : joint=$got, attendu $3"
		failures=$((failures + 1))
	fi
}

expect "sans pare-feu, la box (le défaut corrigé)" 192.168.1.1 oui
pi nft -f "$RULES"
pi nft -f "$RULES" # rechargement sans erreur
expect "avec pare-feu, la box" 192.168.1.1 non
expect "avec pare-feu, le portail du Pi" 10.42.0.1 oui
# Dans l'autre sens, la maison n'atteint pas les clients du point d'accès.
ip netns exec pitest-box ip route add 10.42.0.0/24 via 192.168.1.196
if ip netns exec pitest-box ping -c 1 -W 1 10.42.0.2 >/dev/null 2>&1; then
	echo "ÉCHEC la box atteint un client du point d'accès"
	failures=$((failures + 1))
fi

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "pare-feu du point d'accès : tous les tests passent"
