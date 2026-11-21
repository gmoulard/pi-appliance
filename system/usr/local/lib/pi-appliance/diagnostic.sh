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

# Diagnostic du Pi sans écran (spécification 8.5, TODO-2.55), lu par la console
# pour la page /pi-appliance/diagnostic :
#
#   diagnostic.sh status   état des services du système, erreurs récentes,
#                          sous-tensions (objet JSON)
#   diagnostic.sh logs     journaux des services de pi-appliance, secrets et
#                          adresses e-mail masqués (texte, fichier à envoyer
#                          à qui aide à réparer)

set -eu
export LC_ALL=C

HTTPS_URL=${HTTPS_URL:-https://127.0.0.1/pi-appliance/api/health}
CONSOLE=${CONSOLE:-/usr/local/bin/pi-appliance}
MODEL_FILE=${MODEL_FILE:-/proc/device-tree/model}
OS_RELEASE=${OS_RELEASE:-/etc/os-release}
THROTTLED_FILE=${THROTTLED_FILE:-/sys/devices/platform/soc/soc:firmware/get_throttled}
TRAEFIK_CONTAINER=pi-appliance-traefik
ERROR_LINES=${ERROR_LINES:-30}
LOG_LINES=${LOG_LINES:-3000}

# <identifiant pour la console>:<unité systemd>
UNITS="console:pi-appliance.service
traefik:pi-appliance-traefik.service
traefik-check:pi-appliance-traefik-check.timer
docker:docker.service
network:NetworkManager.service
ap:pi-appliance-ap.service
firewall:pi-appliance-firewall.service
avahi:avahi-daemon.service
mdns:pi-appliance-mdns.service
secrets:pi-appliance-secrets.service"

unit_names() {
	printf '%s\n' "$UNITS" | cut -d: -f2
}

# NE PAS retirer un motif sans test : le fichier des journaux est fait pour être
# envoyé à quelqu'un d'autre (forum, proche) ; aucun secret ne doit y rester.
redact() {
	sed -E \
		-e '/-----BEGIN [A-Z ]*PRIVATE KEY-----/,/-----END [A-Z ]*PRIVATE KEY-----/c\[clé privée masquée]' \
		-e 's#(://)[^/@[:space:]]+@#\1[masqué]@#g' \
		-e "s/(authorization[\"']?[[:space:]]*[:=][[:space:]]*).*/\\1[masqué]/I" \
		-e 's/(bearer)[[:space:]]+[A-Za-z0-9._~+\/=-]+/\1 [masqué]/Ig' \
		-e "s/((pass(word|wd|phrase)?|psk|secret|token|api[_-]?key|private[_-]?key|cookie|session|invite|otp|totp)[\"']?[[:space:]]*[:=][[:space:]]*[\"']?)[^[:space:]\"',;&]+/\\1[masqué]/Ig" \
		-e 's/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/[adresse e-mail]/g'
}

# unit_json <identifiant> <unité> : état d'une unité, en langage de la console
# (ok, starting, restarting, failed, stopped, missing).
unit_json() {
	{ systemctl show "$2" -p LoadState -p ActiveState -p SubState -p Result -p NRestarts -p StateChangeTimestamp 2>/dev/null || true; } |
		jq -R -s -c --arg id "$1" --arg unit "$2" '
			split("\n") | map(select(contains("=")) | {key: .[:index("=")], value: .[index("=") + 1:]}) | from_entries
			| {id: $id, unit: $unit,
				state: (if (.LoadState // "") != "loaded" then "missing"
					elif .ActiveState == "active" or .ActiveState == "reloading" then "ok"
					elif .ActiveState == "activating" and .SubState == "auto-restart" then "restarting"
					elif .ActiveState == "activating" then "starting"
					elif .ActiveState == "failed" then "failed"
					else "stopped" end),
				restarts: ((.NRestarts // "0") | tonumber? // 0),
				since: (.StateChangeTimestamp // "")}'
}

units_json() {
	printf '%s\n' "$UNITS" | while IFS=: read -r id unit; do
		unit_json "$id" "$unit"
	done | jq -s -c .
}

traefik_json() {
	if ! state=$(docker inspect -f '{{.State.Status}} {{.RestartCount}}' "$TRAEFIK_CONTAINER" 2>/dev/null) || [ -z "$state" ]; then
		state="absent 0"
	fi
	code=$(curl -sk --max-time 5 -o /dev/null -w '%{http_code}' "$HTTPS_URL" 2>/dev/null || true)
	printf '%s\n' "$state" | jq -R -c --arg code "$code" '
		split(" ") | {container: .[0], restarts: ((.[1] // "0") | tonumber? // 0), https: ($code == "200")}'
}

# undervoltage <démarrage> : nombre de sous-tensions écrites par le noyau, ou
# null si le journal de ce démarrage n'est pas gardé.
undervoltage() {
	if lines=$(journalctl -k -b "$1" --no-pager -q -o cat 2>/dev/null); then
		printf '%s\n' "$lines" | grep -c 'Undervoltage detected' || true
	else
		echo null
	fi
}

errors_json() {
	{ journalctl -b 0 -p err --no-pager -q -o short-iso -n "$ERROR_LINES" 2>/dev/null || true; } |
		redact | jq -R -s -c 'split("\n") | map(select(length > 0))'
}

failed_json() {
	{ systemctl --failed --plain --no-legend --no-pager 2>/dev/null || true; } |
		awk '{ print $1 }' | jq -R -s -c 'split("\n") | map(select(length > 0))'
}

status() {
	jq -n -c \
		--arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		--argjson uptime "$(cut -d. -f1 /proc/uptime)" \
		--argjson units "$(units_json)" \
		--argjson traefik "$(traefik_json)" \
		--argjson failed "$(failed_json)" \
		--argjson errors "$(errors_json)" \
		--argjson now "$(undervoltage 0)" \
		--argjson before "$(undervoltage -1)" \
		'{at: $at, uptimeSeconds: $uptime, units: $units, traefik: $traefik, failed: $failed, errors: $errors,
			undervoltage: {thisBoot: $now, previousBoot: $before}}'
}

# section <titre> <commande…> : la commande, ou « indisponible » si elle échoue.
section() {
	printf '\n===== %s =====\n' "$1"
	shift
	"$@" 2>&1 || echo "(indisponible)"
}

journal_units() {
	set --
	for unit in $(unit_names); do
		set -- "$@" -u "$unit"
	done
	journalctl -b "$boot" "$@" --no-pager -q -o short-iso -n "$LOG_LINES"
}

logs() {
	{
		printf 'pi-appliance : diagnostic du %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		printf 'Console : %s\n' "$("$CONSOLE" -version 2>/dev/null || echo inconnue)"
		printf 'Modèle : %s\n' "$(tr -d '\0' 2>/dev/null <"$MODEL_FILE" || echo inconnu)"
		printf 'Système : %s\n' "$(sed -n 's/^PRETTY_NAME=//p' "$OS_RELEASE" 2>/dev/null | tr -d '"')"
		printf 'Allumé depuis : %s secondes\n' "$(cut -d. -f1 /proc/uptime)"
		printf 'Alimentation (get_throttled) : %s\n' "$(cat "$THROTTLED_FILE" 2>/dev/null || echo inconnue)"
		# shellcheck disable=SC2046
		section "État des services" systemctl status --no-pager -n 0 $(unit_names)
		section "Services en échec" systemctl --failed --no-pager
		section "Traefik (conteneur)" docker ps -a --filter "name=$TRAEFIK_CONTAINER"
		section "Traefik (journal du conteneur)" docker logs --tail 300 "$TRAEFIK_CONTAINER"
		section "Disques" df -h
		section "Mémoire" free -m
		boot=0
		section "Journal des services de pi-appliance, ce démarrage" journal_units
		section "Noyau, ce démarrage" journalctl -k -b 0 --no-pager -q -o short-iso -n 500
		boot=-1
		section "Journal des services de pi-appliance, démarrage précédent" journal_units
		section "Noyau, démarrage précédent" journalctl -k -b -1 --no-pager -q -o short-iso -n 300
	} | redact
}

case "${1:-}" in
status) status ;;
logs) logs ;;
*)
	echo "usage : diagnostic.sh status | logs" >&2
	exit 2
	;;
esac
