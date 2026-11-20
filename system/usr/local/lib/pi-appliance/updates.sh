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

# Correctifs de Raspberry Pi OS, proposés dans la console (spécification 9.2) :
# rien n'est installé sans l'accord de l'administrateur. Toujours un objet JSON
# sur la sortie standard ; code de retour 0 si la demande a réussi, 1 sinon
# (champs « error » et « message »).
#
#   updates.sh status    paquets à mettre à jour (simulation d'apt, rien n'est installé)
#   updates.sh refresh   relire les listes de paquets des dépôts (apt-get update)
#   updates.sh apply     installer les mises à jour
#
# « apply » dure plusieurs minutes et peut redémarrer Docker, donc Traefik et
# l'accès à la console : il se relance hors du service de la console
# (systemd-run) et répond tout de suite ; l'avancement se lit dans « status »
# (champ « apply »).

# shellcheck source-path=SCRIPTDIR
set -eu

PI_STATE=${PI_STATE:-/var/lib/pi-appliance/system}
APPLY_FILE=$PI_STATE/updates-apply.json
CHECKED_FILE=$PI_STATE/updates-checked
APPLY_LOG=$PI_STATE/updates-apply.log
REBOOT_FILE=${REBOOT_FILE:-/run/reboot-required}

export LC_ALL=C DEBIAN_FRONTEND=noninteractive

# fail <code> <message>
fail() {
	jq -n --arg error "$1" --arg message "$2" '{ok: false, error: $error, message: $message}'
	exit 1
}

now() {
	date -u +%Y-%m-%dT%H:%M:%SZ
}

# set_apply <état> [pourcentage] [code] [message] : état de l'installation, lu par « status ».
set_apply() {
	mkdir -p "$PI_STATE"
	jq -n --arg state "$1" --argjson percent "${2:-0}" --arg error "${3:-}" --arg message "${4:-}" \
		--arg at "$(now)" \
		'{state: $state, percent: $percent, error: $error, message: $message, at: $at}' >"$APPLY_FILE.tmp"
	mv "$APPLY_FILE.tmp" "$APPLY_FILE"
}

# Lignes « Inst » de la simulation d'apt-get vers la liste des paquets :
#   Inst libssl3t64 [3.5.1-1] (3.5.1-1+deb13u1 Debian-Security:13/stable-security [arm64])
# Un paquet vient d'un dépôt de sécurité si l'une de ses origines le dit.
upgrades_json() {
	sed -n 's/^Inst \([^ ]*\) \(\[\([^]]*\)\] \)\{0,1\}(\([^ ]*\) \(.*\))$/\1\t\3\t\4\t\5/p' |
		jq -R -s '[split("\n")[] | select(. != "") | split("\t")
			| {name: .[0], from: .[1], to: .[2],
				security: (.[3] | test("security"; "i"))}]
			| sort_by((if .security then 0 else 1 end), .name)'
}

upgrades() {
	apt-get -s -o Debug::NoLocking=1 full-upgrade 2>/dev/null | upgrades_json
}

busy() {
	case "$(jq -r '.state // ""' "$APPLY_FILE" 2>/dev/null || true)" in
	starting | running) return 0 ;;
	esac
	return 1
}

status() {
	apply='null'
	if [ -s "$APPLY_FILE" ]; then
		# Installation interrompue (Pi éteint) : le service n'existe plus.
		if busy && ! systemctl is-active --quiet pi-appliance-updates.service; then
			set_apply failed 0 interrupted "L'installation des mises à jour a été interrompue. Relancez-la."
		fi
		apply=$(cat "$APPLY_FILE")
	fi
	checked=''
	if [ -s "$CHECKED_FILE" ]; then checked=$(cat "$CHECKED_FILE"); fi
	reboot=false
	if [ -e "$REBOOT_FILE" ]; then reboot=true; fi
	upgrades | jq --argjson apply "$apply" --arg checked "$checked" --argjson reboot "$reboot" \
		'{packages: ., checked_at: (if $checked == "" then null else $checked end), apply: $apply, reboot_required: $reboot}'
}

refresh() {
	if busy; then fail busy "Des mises à jour sont en cours d'installation."; fi
	if ! apt-get -qq -o DPkg::Lock::Timeout=60 update >/dev/null 2>&1; then
		fail refresh-failed "Les dépôts de Raspberry Pi OS ne répondent pas. Le Pi est-il relié à internet ?"
	fi
	mkdir -p "$PI_STATE"
	now >"$CHECKED_FILE"
	jq -n '{ok: true}'
}

# detach : relance « apply » hors du service de la console.
detach() {
	if [ "${PI_UPDATES_DETACHED:-}" = 1 ]; then return 0; fi
	if busy; then fail busy "Des mises à jour sont déjà en cours d'installation."; fi
	if [ "$(upgrades | jq length)" -eq 0 ]; then fail nothing "Le système est à jour."; fi
	set_apply starting
	if ! systemd-run --quiet --collect --unit=pi-appliance-updates \
		--setenv=PI_UPDATES_DETACHED=1 "$0" apply >/dev/null 2>&1; then
		set_apply failed 0 failed "L'installation n'a pas pu démarrer."
		fail failed "L'installation n'a pas pu démarrer."
	fi
	jq -n '{ok: true, started: true}'
	exit 0
}

# Lignes d'avancement d'apt (APT::Status-Fd) vers le fichier d'état :
#   pmstatus:libssl3t64:42.8571:Installing libssl3t64 (arm64)
progress() {
	last=-1
	while IFS=: read -r kind _ percent _; do
		case "$kind" in
		dlstatus | pmstatus) ;;
		*) continue ;;
		esac
		# Téléchargement : 0 à 30 %, installation : 30 à 100 %.
		p=${percent%%.*}
		case "$p" in '' | *[!0-9]*) continue ;; esac
		if [ "$kind" = dlstatus ]; then p=$((p * 30 / 100)); else p=$((30 + p * 70 / 100)); fi
		if [ "$p" -gt "$last" ]; then
			set_apply running "$p"
			last=$p
		fi
	done
}

apply() {
	set_apply running 0
	rc_file=$PI_STATE/updates-apply.rc
	# Les fichiers de configuration modifiés sont gardés (confold) : aucune question.
	{
		apt-get -y -q -o DPkg::Lock::Timeout=120 -o APT::Status-Fd=3 \
			-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold \
			full-upgrade 3>&1 1>>"$APPLY_LOG" 2>&1
		echo "$?" >"$rc_file"
	} | progress
	rc=$(cat "$rc_file" 2>/dev/null || echo 1)
	rm -f "$rc_file"
	if [ "$rc" -ne 0 ]; then
		set_apply failed 0 failed "L'installation des mises à jour a échoué. Réessayez plus tard."
		fail failed "L'installation des mises à jour a échoué."
	fi
	apt-get -y -q autoremove >>"$APPLY_LOG" 2>&1 || true
	set_apply "done" 100
	jq -n '{ok: true}'
}

# Sortie imprévue (set -e) pendant l'installation : pas d'état « en cours » sans fin.
on_exit() {
	code=$?
	if [ "$code" -ne 0 ] && busy; then
		set_apply failed 0 failed "L'installation des mises à jour a échoué. Réessayez plus tard."
	fi
}

# Lu par « . » depuis les tests : les fonctions seulement.
# shellcheck disable=SC2317
if [ "${UPDATES_LIB_ONLY:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

if [ "${PI_UPDATES_DETACHED:-}" = 1 ]; then trap on_exit EXIT; fi

case "${1:-}" in
status) status ;;
refresh) refresh ;;
apply)
	detach
	apply
	;;
*) fail invalid "usage : updates.sh status | refresh | apply" ;;
esac
