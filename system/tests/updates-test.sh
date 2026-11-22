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

# Installation d'une version de pi-appliance (updates.sh appliance), avec de
# faux systemctl et curl, dans un faux système de fichiers (ROOT).
work=$(mktemp -d)
PI_STATE=$work/state
UPGRADE_DIR=$PI_STATE/upgrade
UPGRADE_FILE=$PI_STATE/upgrade.json
UPGRADE_LOG=$PI_STATE/upgrade.log
ROOT=$work/root
export ROOT
CONSOLE_DATA=/var/lib/pi-appliance
HEALTH_URL=http://console/health
HEALTH_WAIT=2
mkdir -p "$work/bin" "$UPGRADE_DIR"
printf '#!/bin/sh\nexit 0\n' >"$work/bin/systemctl"
printf '#!/bin/sh\ncat "%s"\n' "$work/health" >"$work/bin/curl"
chmod 0755 "$work/bin/systemctl" "$work/bin/curl"
PATH=$work/bin:$PATH

# reset_root : la version v1.0.0 installée.
reset_root() {
	rm -rf "$ROOT"
	mkdir -p "$ROOT/usr/local/bin" "$ROOT/etc/pi-appliance" "$ROOT$CONSOLE_DATA/services/cloud"
	echo ancien >"$ROOT/usr/local/bin/pi-appliance"
	echo ancien >"$ROOT/etc/pi-appliance/a.conf"
	echo ancien >"$ROOT$CONSOLE_DATA/accounts.json"
	echo données >"$ROOT$CONSOLE_DATA/services/cloud/data"
}

# make_archive <version annoncée par le binaire> : archive de mise à jour dont
# l'install.sh écrit la nouvelle version et change les réglages de la console.
make_archive() {
	a=$work/archive
	rm -rf "$a"
	mkdir -p "$a/system/etc/pi-appliance"
	printf '#!/bin/sh\necho %s\n' "$1" >"$a/pi-appliance"
	echo nouveau >"$a/system/etc/pi-appliance/a.conf"
	echo nouveau >"$a/system/etc/pi-appliance/b.conf"
	cat >"$a/system/install.sh" <<'INSTALL'
set -eu
here=$(cd "$(dirname "$0")" && pwd)
cp "$2" "$ROOT/usr/local/bin/pi-appliance"
cp "$here/etc/pi-appliance/a.conf" "$here/etc/pi-appliance/b.conf" "$ROOT/etc/pi-appliance/"
echo migré >"$ROOT/var/lib/pi-appliance/accounts.json"
INSTALL
	tar -C "$a" -czf "$UPGRADE_DIR/pi-appliance-v1.1.0-arm64.tar.gz" pi-appliance system
}

# check <nom> <attendu> <obtenu>
check() {
	if [ "$3" != "$2" ]; then
		echo "ÉCHEC $1 : $3, attendu $2"
		failures=$((failures + 1))
	fi
}

reset_root
make_archive v1.1.0
echo '{"status": "ok", "version": "v1.1.0"}' >"$work/health"
(appliance "$UPGRADE_DIR/pi-appliance-v1.1.0-arm64.tar.gz" v1.1.0) >/dev/null || true
check "réussite : état" '["done","v1.1.0",false]' "$(jq -c '[.state, .version, .rolled_back]' "$UPGRADE_FILE")"
check "réussite : console" v1.1.0 "$(sh "$ROOT/usr/local/bin/pi-appliance")"
check "réussite : nouveau fichier" nouveau "$(cat "$ROOT/etc/pi-appliance/b.conf")"
check "réussite : archive supprimée" absente "$([ -e "$UPGRADE_DIR/pi-appliance-v1.1.0-arm64.tar.gz" ] && echo présente || echo absente)"

# La nouvelle console ne répond pas avec son numéro : tout est remis.
reset_root
make_archive v1.1.0
echo '{"status": "ok", "version": "v1.0.0"}' >"$work/health"
(appliance "$UPGRADE_DIR/pi-appliance-v1.1.0-arm64.tar.gz" v1.1.0) >/dev/null 2>&1 || true
check "échec : état" '["failed",true]' "$(jq -c '[.state, .rolled_back]' "$UPGRADE_FILE")"
check "échec : console remise" ancien "$(cat "$ROOT/usr/local/bin/pi-appliance")"
check "échec : fichier remis" ancien "$(cat "$ROOT/etc/pi-appliance/a.conf")"
check "échec : nouveau fichier enlevé" absent "$([ -e "$ROOT/etc/pi-appliance/b.conf" ] && echo présent || echo absent)"
check "échec : réglages remis" ancien "$(cat "$ROOT$CONSOLE_DATA/accounts.json")"
check "échec : données des services intactes" données "$(cat "$ROOT$CONSOLE_DATA/services/cloud/data")"

# Archive d'une autre version : rien n'est touché.
reset_root
make_archive v1.0.9
(appliance "$UPGRADE_DIR/pi-appliance-v1.1.0-arm64.tar.gz" v1.1.0) >/dev/null 2>&1 || true
check "mauvaise version : état" '["failed","invalid",false]' "$(jq -c '[.state, .error, .rolled_back]' "$UPGRADE_FILE")"
check "mauvaise version : console intacte" ancien "$(cat "$ROOT/usr/local/bin/pi-appliance")"

for v in v1.2.3 v1.2.3-beta.1; do
	valid_version "$v" || check "version $v" valide invalide
done
for v in 1.2.3 'v1;reboot' "v1 2" ''; do
	if valid_version "$v"; then check "version « $v »" invalide valide; fi
done
rm -rf "$work"

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "updates : tous les tests passent"
