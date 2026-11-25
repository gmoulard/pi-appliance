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

# Cartes de la console à l'écran (TODO-5.27 à 5.29) : la console de ce dépôt,
# sans Pi ni Docker, avec un faux script des correctifs du système et un
# service « camera » déjà installé, ouverte
# dans Chromium sans tête par console-cards.cjs (Playwright).
#
# Usage : sh system/tests/console-cards-test.sh
# Nécessite Go, curl et Node.js avec Playwright (require('playwright'),
# NODE_PATH réglé sur les modules globaux s'il le faut).
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
TMP=$(mktemp -d)
PORT=${PORT:-18080}
CONSOLE_PID=

# shellcheck disable=SC2329 # appelée par trap
cleanup() {
	[ -z "$CONSOLE_PID" ] || kill "$CONSOLE_PID" 2>/dev/null || true
	rm -rf "$TMP"
}
trap cleanup 0 HUP INT TERM

for tool in go curl node; do
	command -v "$tool" >/dev/null || { echo "$tool manquant" >&2; exit 1; }
done
[ -n "${NODE_PATH:-}" ] || NODE_PATH=$(npm root -g 2>/dev/null || true)
export NODE_PATH
node -e "require('playwright')" 2>/dev/null || { echo "Playwright manquant pour Node.js" >&2; exit 1; }

(cd "$ROOT" && go build -o "$TMP/pi-appliance" ./cmd/pi-appliance)

cat >"$TMP/updates.sh" <<'EOF'
#!/bin/sh
case "$1" in
status) echo '{"packages":[{"name":"openssl","security":true},{"name":"curl","security":false}],"checked_at":"2026-11-25T06:00:00Z","apply":null,"upgrade":null,"reboot_required":false}' ;;
*) echo '{}' ;;
esac
EOF
chmod +x "$TMP/updates.sh"
mkdir -p "$TMP/data/services/camera" "$TMP/tls"
printf '{"name":"camera","app":"camera","app_name":"Caméra et timelapse","address":"camera-pi-essai.local","installed_at":"2026-11-25T00:00:00Z"}\n' \
	>"$TMP/data/services/camera/instance.json"

"$TMP/pi-appliance" -addr "127.0.0.1:$PORT" -data "$TMP/data" -domain pi-essai.local \
	-reset-file "" -network-script "" -stats-url "" -storage-script "" -backup-script "" \
	-updates-script "$TMP/updates.sh" -diagnostic-script "" -diagnostic-file "" -rescue-addr "" -tls-dir "$TMP/tls" \
	-cockpit-script "" -release-repo "" -catalog-url "" -cli-token-file "$TMP/token" \
	-addresses-url "" -upnp=false -mail-url "" -mail-relay=false >"$TMP/console.log" 2>&1 &
CONSOLE_PID=$!

API="http://127.0.0.1:$PORT/pi-appliance/api"
attempt=0
until curl -fsS --noproxy '*' -o /dev/null "$API/hello" 2>/dev/null; do
	attempt=$((attempt + 1))
	[ "$attempt" -lt 60 ] || { echo "la console ne répond pas" >&2; cat "$TMP/console.log" >&2; exit 1; }
	sleep 1
done

curl -fsS --noproxy '*' -c "$TMP/cookies" -H 'X-Pi-Appliance: 1' -H 'Content-Type: application/json' \
	--data '{"name":"essai","email":"essai@example.org","password":"Essai-des-cartes-2026","lang":"fr"}' \
	-o /dev/null "$API/setup"
curl -fsS --noproxy '*' -b "$TMP/cookies" -H 'X-Pi-Appliance: 1' -X POST -o /dev/null "$API/wizard"

node "$ROOT/system/tests/console-cards.cjs" "http://127.0.0.1:$PORT/pi-appliance/" essai Essai-des-cartes-2026
