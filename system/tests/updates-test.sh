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

# Tests de la lecture des correctifs du système (updates.sh), sans Pi ni apt :
# sh system/tests/updates-test.sh

# shellcheck source-path=SCRIPTDIR
set -eu

LIB=$(dirname "$0")/../usr/local/lib/pi-appliance
failures=0

UPDATES_LIB_ONLY=1
# shellcheck source=../usr/local/lib/pi-appliance/updates.sh
. "$LIB/updates.sh"

# expect <nom> <filtre jq> <attendu>
expect() {
	got=$(printf '%s\n' "$simulation" | upgrades_json | jq -c "$2")
	if [ "$got" != "$3" ]; then
		echo "ÉCHEC $1 : $got, attendu $3"
		failures=$((failures + 1))
	fi
}

# Sortie de « apt-get -s full-upgrade » : correctif de sécurité de Debian,
# paquet de Raspberry Pi OS, nouveau paquet (sans ancienne version), paquet
# venu de deux dépôts dont celui de sécurité.
simulation='Reading package lists...
Building dependency tree...
The following packages will be upgraded:
   libssl3t64 openssl raspi-firmware
Inst raspi-firmware [1:1.20250430-1] (1:1.20250915-1 Raspberry Pi Foundation:stable [all])
Inst libssl3t64 [3.5.1-1] (3.5.1-1+deb13u1 Debian-Security:13/stable-security [arm64])
Inst linux-image-6.12.47 (1:6.12.47-1+rpt1 Raspberry Pi Foundation:stable [arm64])
Inst openssl [3.5.1-1] (3.5.1-1+deb13u1 Debian:13.2/stable, Debian-Security:13/stable-security [arm64])
Conf raspi-firmware (1:1.20250915-1 Raspberry Pi Foundation:stable [all])
Conf libssl3t64 (3.5.1-1+deb13u1 Debian-Security:13/stable-security [arm64])'

expect "quatre paquets, sécurité d'abord" '[.[] | [.name, .security]]' '[["libssl3t64",true],["openssl",true],["linux-image-6.12.47",false],["raspi-firmware",false]]'
expect "versions" '.[0] | [.from, .to]' '["3.5.1-1","3.5.1-1+deb13u1"]'
expect "nouveau paquet" '.[2] | [.from, .to]' '["","1:6.12.47-1+rpt1"]'

simulation='Reading package lists...
0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.'
expect "système à jour" '.' '[]'

# Avancement d'apt (APT::Status-Fd) vers le fichier d'état.
PI_STATE=$(mktemp -d)
APPLY_FILE=$PI_STATE/updates-apply.json
printf '%s\n' 'dlstatus:1:50.0000:Retrieving file 1 of 2' 'pmstatus:dpkg-exec:0.0000:Running dpkg' \
	'pmstatus:openssl:50.0000:Installing openssl (arm64)' 'pmstatus:openssl:42.0000:Configuring openssl' | progress
got=$(jq -c '[.state, .percent]' "$APPLY_FILE")
if [ "$got" != '["running",65]' ]; then
	echo "ÉCHEC avancement : $got, attendu [\"running\",65]"
	failures=$((failures + 1))
fi
rm -rf "$PI_STATE"

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "updates : tous les tests passent"
