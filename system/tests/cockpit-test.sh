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
# Comptes du Pi : un fichier par compte dans $FAKE/users, avec ses groupes.
mkdir -p "$fake/users" "$fake/comments"
fake id 'case "$1" in
-u) [ -e "$FAKE/users/$2" ] ;;
-nG) [ -e "$FAKE/users/$2" ] && cat "$FAKE/users/$2" ;;
*) exit 1 ;;
esac'
fake useradd 'echo "useradd $*" >>"$FAKE/calls"
groups=""; comment=""
while [ $# -gt 1 ]; do
	case "$1" in
	--groups) groups=$(echo "$2" | tr , " "); shift ;;
	--comment) comment=$2; shift ;;
	esac
	shift
done
echo "$1 $groups" >"$FAKE/users/$1"; echo "$comment" >"$FAKE/comments/$1"'
fake usermod 'echo "usermod $*" >>"$FAKE/calls"
for name; do :; done
[ -e "$FAKE/users/$name" ] || exit 6
echo "$(cat "$FAKE/users/$name") $(echo "$4" | tr , " ")" >"$FAKE/users/$name"'
fake userdel 'echo "userdel $*" >>"$FAKE/calls"
for name; do :; done
rm -f "$FAKE/users/$name" "$FAKE/comments/$name"'
fake getent 'case "$1" in
group)
	[ -e "$FAKE/group" ] || exit 2
	list=$(grep -lw "$2" "$FAKE"/users/* 2>/dev/null | xargs -n1 basename 2>/dev/null | sort | paste -sd, -)
	echo "$2:x:999:$list" ;;
passwd)
	[ -e "$FAKE/users/$2" ] || exit 2
	echo "$2:x:1000:1000:$(cat "$FAKE/comments/$2" 2>/dev/null):/home/$2:/bin/bash" ;;
esac'
fake groupadd 'echo "groupadd $*" >>"$FAKE/calls"; touch "$FAKE/group"'
fake groupdel 'echo "groupdel $*" >>"$FAKE/calls"; rm -f "$FAKE/group"'
fake loginctl 'exit 0'
fake chpasswd 'cat >>"$FAKE/chpasswd"'

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

check "pas installé" "$(run status | jq -c '[.installed, .active, .accounts, .job]')" '[false,false,[],null]'

# Compte du Pi qui porte le nom d'un administrateur, et compte pi-admin des
# versions d'avant la connexion unique.
echo "guillaume guillaume sudo" >"$fake/users/guillaume"
echo "pi-admin pi-admin sudo" >"$fake/users/pi-admin"
echo "Administration du systeme (pi-appliance)" >"$fake/comments/pi-admin"

check "installation" "$(run install | jq -c '[.ok, .started]')" '[true,true]'
check "groupe créé" "$(grep -c '^groupadd --system pia-cockpit$' "$fake/calls")" 1
check "refusé par SSH" "$(grep -c '^DenyGroups pia-cockpit$' "$SSHD_CONF")" 1
check "sans les paquets recommandés ni PackageKit" \
	"$(grep -c '^apt-get .*--no-install-recommends .* install cockpit-ws cockpit-system cockpit-bridge cockpit-networkmanager cockpit-storaged$' "$fake/calls")" 1
check "PackageKit absent" "$(grep -c packagekit "$fake/calls" || true)" 0
check "socket démarré" "$(grep -c '^systemctl enable --now cockpit.socket$' "$fake/calls")" 1
check "installé" "$(run status | jq -c '[.installed, .active, .job.action, .job.state]')" '[true,true,"install","done"]'

pw1=motdepasse-tire-au-hasard-1
pw2=motdepasse-tire-au-hasard-2
check "comptes des administrateurs" \
	"$(printf 'marie %s\nguillaume %s\n' "$pw1" "$pw2" | run sync | jq -c '[.ok, .accounts, .conflicts]')" \
	'[true,{"marie":"marie","guillaume":"pia-guillaume"},[]]'
check "mots de passe transmis" "$(cat "$fake/chpasswd")" "marie:$pw1
pia-guillaume:$pw2"
check "comptes sudo et Cockpit" "$(cat "$fake/users/marie")" "marie sudo pia-cockpit"
check "compte du Pi intact" "$(cat "$fake/users/guillaume")" "guillaume guillaume sudo"
check "ancien pi-admin supprimé" "$(test -e "$fake/users/pi-admin" && echo oui || echo non)" non
check "mot de passe pas gardé" "$(grep -rl "$pw1" "$PI_STATE" || true)" ""
check "comptes listés" "$(run status | jq -c .accounts)" '["marie","pia-guillaume"]'

# marie n'est plus administratrice : son compte système part.
check "administrateur retiré" "$(printf 'guillaume %s\n' "$pw2" | run sync | jq -c .accounts)" '{"guillaume":"pia-guillaume"}'
check "compte retiré" "$(test -e "$fake/users/marie" && echo oui || echo non)" non
check "nom refusé" "$(printf 'x;rm %s\n' "$pw1" | run sync | jq -r .error)" invalid
check "mot de passe trop court" "$(printf 'marie court\n' | run sync | jq -r .error)" invalid

check "retrait" "$(run remove | jq -c .started)" true
check "paquets retirés" "$(grep -c '^apt-get .* purge cockpit-ws' "$fake/calls")" 1
check "comptes supprimés" "$(grep -c '^userdel --remove pia-guillaume$' "$fake/calls")" 1
check "compte du Pi gardé" "$(test -e "$fake/users/guillaume" && echo oui || echo non)" oui
check "groupe supprimé" "$(test -e "$fake/group" && echo oui || echo non)" non
check "SSH remis" "$(test -e "$SSHD_CONF" && echo oui || echo non)" non
check "retiré" "$(run status | jq -c '[.installed, .accounts, .job.action, .job.state]')" '[false,[],"remove","done"]'

# Échec d'apt : l'état le dit en langage courant.
touch "$fake/apt-fails"
run install >/dev/null
check "échec" "$(run status | jq -c '[.installed, .job.state, .job.error]')" '[false,"failed","failed"]'

# Opération en cours alors que son service n'existe plus (Pi éteint).
jq -n '{action: "install", state: "running"}' >"$PI_STATE/cockpit-job.json"
check "interrompue" "$(run status | jq -r .job.error)" interrupted

if [ "$failures" -ne 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "cockpit.sh : tout est bon"
