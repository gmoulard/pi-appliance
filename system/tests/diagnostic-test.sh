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

# Tests du diagnostic du Pi (diagnostic.sh), sans Pi : de faux systemctl,
# journalctl, docker et curl rendent un Pi où Traefik redémarre en boucle.
#   sh system/tests/diagnostic-test.sh

# Corps des faux programmes entre apostrophes : développés quand ils tournent.
# shellcheck disable=SC2016
set -eu

SCRIPT=$(cd "$(dirname "$0")/../usr/local/lib/pi-appliance" && pwd)/diagnostic.sh
failures=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
bin=$work/bin
fake=$work/fake
mkdir -p "$bin" "$fake"

fake() {
	printf '#!/bin/sh\nFAKE=%s\n%s\n' "$fake" "$2" >"$bin/$1"
	chmod +x "$bin/$1"
}
fake systemctl 'echo "systemctl $*" >>"$FAKE/calls"
case "$1 $2" in
"show pi-appliance-traefik.service") printf "LoadState=loaded\nActiveState=active\nSubState=exited\nResult=success\nNRestarts=0\nStateChangeTimestamp=Fri 2026-11-20 10:00:00 CET\n" ;;
"show docker.service") printf "LoadState=loaded\nActiveState=activating\nSubState=auto-restart\nResult=exit-code\nNRestarts=4\n" ;;
"show avahi-daemon.service") printf "LoadState=loaded\nActiveState=failed\nSubState=failed\nResult=exit-code\nNRestarts=0\n" ;;
"show pi-appliance-mdns.service") printf "LoadState=not-found\nActiveState=inactive\n" ;;
"show pi-appliance-ap.service") printf "LoadState=loaded\nActiveState=inactive\nSubState=dead\nResult=success\n" ;;
show*) printf "LoadState=loaded\nActiveState=active\nSubState=running\nResult=success\nNRestarts=0\n" ;;
"--failed --plain") printf "avahi-daemon.service loaded failed failed Avahi\n" ;;
*) echo "état des services" ;;
esac'
fake journalctl 'echo "journalctl $*" >>"$FAKE/calls"
case "$*" in
*"-b -1"*) [ -e "$FAKE/previous" ] || { echo "Specified boot ID not found" >&2; exit 1; }
	echo "hwmon hwmon1: Undervoltage detected!" ;;
*"-k -b 0"*) printf "hwmon hwmon1: Undervoltage detected!\nhwmon hwmon1: Voltage normalised\nhwmon hwmon1: Undervoltage detected!\n" ;;
*"-p err"*) printf "2026-11-20T10:01:00+0100 pi NetworkManager[1]: secrets: psk=motdepassewifi refusé\n2026-11-20T10:02:00+0100 pi pi-appliance[2]: invitation pour marie@example.com\n" ;;
*) printf "2026-11-20T10:03:00+0100 pi traefik: token=abcdef Authorization: Bearer xyz\n-----BEGIN PRIVATE KEY-----\nMIIsecret\n-----END PRIVATE KEY-----\nMain process exited, code=exited, status=2/INVALIDARGUMENT\n" ;;
esac'
fake docker 'case "$1" in
inspect) echo "restarting 7" ;;
logs) echo "level=error msg=\"SIGILL: illegal instruction\" password=traefiksecret" ;;
*) echo "pi-appliance-traefik" ;;
esac'
fake curl 'printf 000; exit 7'
fake free 'echo mémoire'
printf 'Raspberry Pi 3 Model B Plus Rev 1.3\0' >"$work/model"
printf 'PRETTY_NAME="Debian GNU/Linux 13 (trixie)"\n' >"$work/os-release"
printf '0x50005\n' >"$work/throttled"
fake pi-appliance 'echo 0.1.3'

export PATH="$bin:$PATH" CONSOLE="$bin/pi-appliance" MODEL_FILE="$work/model" OS_RELEASE="$work/os-release" THROTTLED_FILE="$work/throttled"

check() {
	if [ "$2" != "$3" ]; then
		echo "ÉCHEC $1 : $2, attendu $3"
		failures=$((failures + 1))
	fi
}

status=$(sh "$SCRIPT" status)
state() {
	printf '%s' "$status" | jq -r --arg id "$1" '.units[] | select(.id == $id) | "\(.state) \(.restarts)"'
}
check "console" "$(state console)" "ok 0"
check "Traefik (unité)" "$(state traefik)" "ok 0"
check "Docker qui redémarre" "$(state docker)" "restarting 4"
check "avahi en échec" "$(state avahi)" "failed 0"
check "unité absente" "$(state mdns)" "missing 0"
check "point d'accès fermé" "$(state ap)" "stopped 0"
check "dix unités" "$(printf '%s' "$status" | jq '.units | length')" 10
check "conteneur de Traefik" "$(printf '%s' "$status" | jq -c .traefik)" '{"container":"restarting","restarts":7,"https":false}'
check "unités en échec" "$(printf '%s' "$status" | jq -c .failed)" '["avahi-daemon.service"]'
check "sous-tensions" "$(printf '%s' "$status" | jq -c .undervoltage)" '{"thisBoot":2,"previousBoot":null}'
check "erreurs" "$(printf '%s' "$status" | jq '.errors | length')" 2
check "erreurs sans secret" "$(printf '%s' "$status" | grep -c 'motdepassewifi\|marie@' || true)" 0
check "secret masqué" "$(printf '%s' "$status" | jq -r '.errors[0]' | grep -c 'psk=\[masqué\]')" 1

touch "$fake/previous"
status=$(sh "$SCRIPT" status)
check "sous-tensions du démarrage précédent" "$(printf '%s' "$status" | jq -c .undervoltage.previousBoot)" 1

: >"$fake/calls"
logs=$(sh "$SCRIPT" logs)
for secret in abcdef xyz MIIsecret traefiksecret; do
	check "journaux sans « $secret »" "$(printf '%s\n' "$logs" | grep -c "$secret" || true)" 0
done
check "clé privée signalée" "$(printf '%s\n' "$logs" | grep -c 'clé privée masquée')" 1
check "code de sortie gardé" "$(printf '%s\n' "$logs" | grep -c 'code=exited, status=2')" 1
check "modèle" "$(printf '%s\n' "$logs" | grep -c '^Modèle : Raspberry Pi 3 Model B Plus Rev 1.3$')" 1
check "version" "$(printf '%s\n' "$logs" | grep -c '^Console : 0.1.3$')" 1
check "erreur de Traefik" "$(printf '%s\n' "$logs" | grep -c 'SIGILL')" 1
check "journal de toutes les unités" "$(grep -c -- '-b 0 -u pi-appliance.service -u pi-appliance-traefik.service .*-u pi-appliance-secrets.service' "$fake/calls")" 1
check "démarrage précédent" "$(grep -c -- '-b -1 -u pi-appliance.service' "$fake/calls")" 1

rm "$fake/previous"
check "démarrage précédent absent" "$(sh "$SCRIPT" logs | grep -c '(indisponible)')" 2

if sh "$SCRIPT" autre 2>/dev/null; then
	check "commande inconnue" accepté refusé
fi

if [ "$failures" -ne 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "diagnostic.sh : tout est bon"
