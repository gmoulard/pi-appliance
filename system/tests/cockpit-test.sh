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

# Tests de l'installation de Cockpit (cockpit.sh), sans Pi ni apt : de faux
# apt-get, dpkg-query, systemctl, systemd-run, useradd, chpasswd… notent ce
# qui leur est demandé. sh system/tests/cockpit-test.sh

# Corps des faux programmes entre apostrophes : développés quand ils tournent.
# shellcheck disable=SC2016
set -eu

SCRIPT=$(cd "$(dirname "$0")/../usr/local/lib/pi-appliance" && pwd)/cockpit.sh
failures=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
bin=$work/bin
fake=$work/fake
mkdir -p "$bin" "$fake"

# fake <commande> <corps du script>
fake() {
	printf '#!/bin/sh\nFAKE=%s\n%s\n' "$fake" "$2" >"$bin/$1"
	chmod +x "$bin/$1"
}
fake apt-get 'echo "apt-get $*" >>"$FAKE/calls"
case "$*" in
*" install "*) [ -e "$FAKE/apt-fails" ] && exit 100; touch "$FAKE/installed" ;;
*" purge "*) rm -f "$FAKE/installed" ;;
esac
exit 0'
fake dpkg-query '[ -e "$FAKE/installed" ] && printf "install ok installed"; exit 0'
fake systemctl 'echo "systemctl $*" >>"$FAKE/calls"
case "$1" in is-active) [ -e "$FAKE/installed" ] ;; *) exit 0 ;; esac'
# systemd-run : lance tout de suite, avec l'environnement demandé.
fake systemd-run 'echo "systemd-run $*" >>"$FAKE/calls"
while [ $# -gt 0 ]; do
	case "$1" in
	--setenv=*) export "${1#--setenv=}" ;;
	--*) ;;
	*) break ;;
	esac
	shift
done
"$@" >/dev/null 2>&1 || true'
fake id '[ -e "$FAKE/user" ]'
fake useradd 'echo "useradd $*" >>"$FAKE/calls"; touch "$FAKE/user"'
fake userdel 'echo "userdel $*" >>"$FAKE/calls"; rm -f "$FAKE/user"'
fake loginctl 'exit 0'
fake chpasswd 'cat >"$FAKE/chpasswd"'

export PATH="$bin:$PATH" PI_STATE="$work/state" SSHD_CONF="$work/ssh/pi-appliance-cockpit.conf"

# check <nom> <valeur> <attendu>
check() {
	if [ "$2" != "$3" ]; then
		echo "ÉCHEC $1 : $2, attendu $3"
		failures=$((failures + 1))
	fi
}

run() {
	sh "$SCRIPT" "$@" || true
}

check "pas installé" "$(run status | jq -c '[.installed, .active, .account, .user, .job]')" '[false,false,false,"pi-admin",null]'
check "mot de passe trop court" "$(echo court | run install | jq -r .error)" invalid
check "rien créé" "$(test -e "$fake/user" && echo oui || echo non)" non

check "installation" "$(echo 'un mot de passe solide' | run install | jq -c '[.ok, .started]')" '[true,true]'
check "mot de passe transmis" "$(cat "$fake/chpasswd")" 'pi-admin:un mot de passe solide'
check "compte sudo" "$(grep -c '^useradd .*--groups sudo .*pi-admin$' "$fake/calls")" 1
check "refusé par SSH" "$(grep -c '^DenyUsers pi-admin$' "$SSHD_CONF")" 1
check "sans les paquets recommandés ni PackageKit" \
	"$(grep -c '^apt-get .*--no-install-recommends .* install cockpit-ws cockpit-system cockpit-bridge cockpit-networkmanager cockpit-storaged$' "$fake/calls")" 1
check "PackageKit absent" "$(grep -c packagekit "$fake/calls" || true)" 0
check "socket démarré" "$(grep -c '^systemctl enable --now cockpit.socket$' "$fake/calls")" 1
check "installé" "$(run status | jq -c '[.installed, .active, .account, .job.action, .job.state]')" '[true,true,true,"install","done"]'
check "mot de passe pas gardé" "$(grep -rl 'mot de passe solide' "$PI_STATE" || true)" ""

check "nouveau mot de passe" "$(echo 'un autre mot de passe' | run password | jq -c .ok)" true
check "nouveau mot de passe transmis" "$(cat "$fake/chpasswd")" 'pi-admin:un autre mot de passe'

check "retrait" "$(run remove | jq -c .started)" true
check "paquets retirés" "$(grep -c '^apt-get .* purge cockpit-ws' "$fake/calls")" 1
check "compte supprimé" "$(grep -c '^userdel --remove pi-admin$' "$fake/calls")" 1
check "SSH remis" "$(test -e "$SSHD_CONF" && echo oui || echo non)" non
check "retiré" "$(run status | jq -c '[.installed, .account, .job.action, .job.state]')" '[false,false,"remove","done"]'
check "mot de passe sans compte" "$(echo 'un mot de passe solide' | run password | jq -r .error)" invalid

# Échec d'apt : l'état le dit en langage courant.
touch "$fake/apt-fails"
echo 'un mot de passe solide' | run install >/dev/null
check "échec" "$(run status | jq -c '[.installed, .job.state, .job.error]')" '[false,"failed","failed"]'

# Opération en cours alors que son service n'existe plus (Pi éteint).
jq -n '{action: "install", state: "running"}' >"$PI_STATE/cockpit-job.json"
check "interrompue" "$(run status | jq -r .job.error)" interrupted

if [ "$failures" -ne 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "cockpit.sh : tout est bon"
