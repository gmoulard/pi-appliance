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


# Tests de la configuration SSH (etc/ssh/sshd_config.d/10-pi-appliance.conf),
# avec le vrai sshd (paquet openssh-server) : clés seulement depuis internet,
# mot de passe accepté à la maison, même quand cloud-init a écrit
# « PasswordAuthentication yes » dans 50-cloud-init.conf.
# sh system/tests/sshd-test.sh
set -eu

SSHD=${SSHD:-/usr/sbin/sshd}
CONF=$(cd "$(dirname "$0")/../etc/ssh/sshd_config.d" && pwd)/10-pi-appliance.conf
failures=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

check() {
	if [ "$2" = "$3" ]; then
		echo "ok - $1"
	else
		echo "ÉCHEC - $1 : « $2 », attendu « $3 »"
		failures=$((failures + 1))
	fi
}

mkdir -p "$work/d"
printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication yes\nPermitRootLogin yes\n' >"$work/d/50-cloud-init.conf"
cp "$CONF" "$work/d/"
ssh-keygen -q -t ed25519 -N '' -f "$work/hostkey"
printf 'Include %s/d/*.conf\nHostKey %s/hostkey\n' "$work" "$work" >"$work/sshd_config"

# Valeur d'un réglage pour une connexion venue de l'adresse $1.
setting() {
	"$SSHD" -T -f "$work/sshd_config" -C "addr=$1,user=pi,host=x,laddr=192.168.1.196,lport=22" |
		awk -v k="$2" '$1 == k { print $2 }'
}

for addr in 82.66.246.212 8.8.8.8 2a01:e0a::1; do
	check "internet $addr : mot de passe refusé" "$(setting "$addr" passwordauthentication)" no
	check "internet $addr : clés acceptées" "$(setting "$addr" pubkeyauthentication)" yes
	check "internet $addr : clavier interactif refusé" "$(setting "$addr" kbdinteractiveauthentication)" no
done
for addr in 192.168.1.10 10.0.0.5 172.16.3.4 127.0.0.1 ::1 fd00::5 fe80::1; do
	check "maison $addr : mot de passe accepté" "$(setting "$addr" passwordauthentication)" yes
done
check "root refusé" "$(setting 192.168.1.10 permitrootlogin)" no

if [ "$failures" -ne 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "tous les tests passent"
