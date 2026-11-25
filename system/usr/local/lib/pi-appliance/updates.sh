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

# Correctifs de Raspberry Pi OS, proposés dans la console (spécification 9.2) :
# rien n'est installé sans l'accord de l'administrateur. Toujours un objet JSON
# sur la sortie standard ; code de retour 0 si la demande a réussi, 1 sinon
# (champs « error » et « message »).
#
#   updates.sh status    paquets à mettre à jour (simulation d'apt, rien n'est installé)
#   updates.sh refresh   relire les listes de paquets des dépôts (apt-get update)
#   updates.sh apply     installer les mises à jour
#   updates.sh appliance <archive> <version>
#                        installer une version de pi-appliance (archive de
#                        mise à jour d'une version publiée, déjà téléchargée et
#                        vérifiée par la console)
#
# « apply » dure plusieurs minutes et peut redémarrer Docker, donc Traefik et
# l'accès à la console : il se relance hors du service de la console
# (systemd-run) et répond tout de suite ; l'avancement se lit dans « status »
# (champ « apply »). De même pour « appliance » (champ « upgrade ») : la
# console redémarre avec la nouvelle version ; si elle ne répond pas avec ce
# numéro de version dans les 2 minutes, l'ancienne version est remise.

# shellcheck source-path=SCRIPTDIR
set -eu

PI_STATE=${PI_STATE:-/var/lib/pi-appliance/system}
APPLY_FILE=$PI_STATE/updates-apply.json
CHECKED_FILE=$PI_STATE/updates-checked
APPLY_LOG=$PI_STATE/updates-apply.log
REBOOT_FILE=${REBOOT_FILE:-/run/reboot-required}
UPGRADE_DIR=$PI_STATE/upgrade
UPGRADE_FILE=$PI_STATE/upgrade.json
UPGRADE_LOG=$PI_STATE/upgrade.log
# ROOT, CONSOLE_DATA, HEALTH_URL, HEALTH_WAIT : changés par les tests.
ROOT=${ROOT:-}
CONSOLE_DATA=${CONSOLE_DATA:-/var/lib/pi-appliance}
HEALTH_URL=${HEALTH_URL:-http://127.0.0.1:8080/pi-appliance/api/health}
HEALTH_WAIT=${HEALTH_WAIT:-120}

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

# set_upgrade <état> <version> [code] [message] [remise] : installation d'une
# version de pi-appliance, lue par « status ». remise : true si l'ancienne
# version a été remise après un échec.
set_upgrade() {
	mkdir -p "$PI_STATE"
	jq -n --arg state "$1" --arg version "$2" --arg error "${3:-}" --arg message "${4:-}" \
		--argjson rolledBack "${5:-false}" --arg at "$(now)" \
		'{state: $state, version: $version, error: $error, message: $message, rolled_back: $rolledBack, at: $at}' >"$UPGRADE_FILE.tmp"
	mv "$UPGRADE_FILE.tmp" "$UPGRADE_FILE"
}

upgrade_busy() {
	case "$(jq -r '.state // ""' "$UPGRADE_FILE" 2>/dev/null || true)" in
	starting | running) return 0 ;;
	esac
	return 1
}

status() {
	upgrade='null'
	if [ -s "$UPGRADE_FILE" ]; then
		if upgrade_busy && ! systemctl is-active --quiet pi-appliance-upgrade.service; then
			set_upgrade failed "$(jq -r '.version' "$UPGRADE_FILE")" interrupted "L'installation de la nouvelle version a été interrompue. Relancez-la."
		fi
		upgrade=$(cat "$UPGRADE_FILE")
	fi
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
	upgrades | jq --argjson apply "$apply" --argjson upgrade "$upgrade" --arg checked "$checked" --argjson reboot "$reboot" \
		'{packages: ., checked_at: (if $checked == "" then null else $checked end), apply: $apply, upgrade: $upgrade, reboot_required: $reboot}'
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

# Version de pi-appliance : v1.2.3, v1.2.3-beta.1…
valid_version() {
	case "$1" in
	v[0-9]*) ;;
	*) return 1 ;;
	esac
	case "$1" in
	*[!0-9A-Za-z.-]*) return 1 ;;
	esac
	[ "${#1}" -le 64 ]
}

# appliance_detach <archive> <version> : relance « appliance » hors du service
# de la console, qui va redémarrer.
appliance_detach() {
	if [ "${PI_UPDATES_DETACHED:-}" = 1 ]; then return 0; fi
	if ! valid_version "$2"; then fail invalid "Numéro de version invalide."; fi
	case "$1" in
	"$UPGRADE_DIR"/*.tar.gz) ;;
	*) fail invalid "Archive de mise à jour inattendue." ;;
	esac
	if [ ! -f "$1" ]; then fail invalid "Archive de mise à jour introuvable."; fi
	if upgrade_busy || busy; then fail busy "Une installation est déjà en cours."; fi
	set_upgrade starting "$2"
	if ! systemd-run --quiet --collect --unit=pi-appliance-upgrade \
		--setenv=PI_UPDATES_DETACHED=1 "$0" appliance "$1" "$2" >/dev/null 2>&1; then
		set_upgrade failed "$2" failed "L'installation n'a pas pu démarrer."
		fail failed "L'installation n'a pas pu démarrer."
	fi
	jq -n '{ok: true, started: true}'
	exit 0
}

# healthy <version> : la console répond, avec ce numéro de version.
healthy() {
	waited=0
	while [ "$waited" -lt "$HEALTH_WAIT" ]; do
		if [ "$(curl -fsS --max-time 5 "$HEALTH_URL" 2>/dev/null | jq -r '.version // ""' 2>/dev/null || true)" = "$1" ]; then
			return 0
		fi
		sleep 2
		waited=$((waited + 2))
	done
	return 1
}

restart_console() {
	systemctl daemon-reload || true
	systemctl restart pi-appliance-traefik.service pi-appliance.service || true
}

# rollback : remet les fichiers d'avant l'installation et enlève les nouveaux.
rollback() {
	while IFS= read -r f; do
		rm -f "$ROOT/$f"
	done <"$UPGRADE_DIR/added"
	tar -C "$ROOT/" -xpf "$UPGRADE_DIR/rollback.tar"
	restart_console
}

# appliance <archive> <version> : archive faite de « pi-appliance » (binaire
# arm64) et de « system/ » (couche système, installée par system/install.sh
# --update, sans toucher à Docker).
appliance() {
	archive=$1
	version=$2
	set_upgrade running "$version"
	new=$UPGRADE_DIR/new
	rm -rf "$new"
	mkdir -p "$new"
	if ! tar -C "$new" -xzf "$archive" >>"$UPGRADE_LOG" 2>&1 ||
		[ ! -f "$new/pi-appliance" ] || [ ! -f "$new/system/install.sh" ]; then
		set_upgrade failed "$version" invalid "L'archive de la nouvelle version est abîmée. Rien n'a été changé."
		fail invalid "Archive abîmée."
	fi
	chmod 0755 "$new/pi-appliance"
	if [ "$("$new/pi-appliance" -version 2>/dev/null || true)" != "$version" ]; then
		set_upgrade failed "$version" invalid "Cette archive ne contient pas la version $version pour ce Pi. Rien n'a été changé."
		fail invalid "Mauvaise version."
	fi
	# Copie de ce que l'installation remplace : console, fichiers du système
	# livrés par la nouvelle version, réglages de la console (pas les services).
	list=$UPGRADE_DIR/files
	: >"$list"
	: >"$UPGRADE_DIR/added"
	echo usr/local/bin/pi-appliance >>"$list"
	for dir in etc usr; do
		if [ -d "$new/system/$dir" ]; then
			(cd "$new/system" && find "$dir" -type f) | while IFS= read -r f; do
				if [ -e "$ROOT/$f" ]; then echo "$f" >>"$list"; else echo "$f" >>"$UPGRADE_DIR/added"; fi
			done
		fi
	done
	find "$ROOT$CONSOLE_DATA" -maxdepth 1 -type f | sed "s#^$ROOT/##" >>"$list"
	if ! tar -C "$ROOT/" -cpf "$UPGRADE_DIR/rollback.tar" -T "$list" >>"$UPGRADE_LOG" 2>&1; then
		set_upgrade failed "$version" failed "La copie de la version actuelle a échoué. Rien n'a été changé."
		fail failed "Copie impossible."
	fi
	if sh "$new/system/install.sh" --update "$new/pi-appliance" >>"$UPGRADE_LOG" 2>&1 && healthy "$version"; then
		rm -rf "$new" "$UPGRADE_DIR/rollback.tar" "$archive"
		set_upgrade "done" "$version"
		jq -n '{ok: true}'
		return 0
	fi
	rollback >>"$UPGRADE_LOG" 2>&1 || true
	set_upgrade failed "$version" failed "La version $version n'a pas démarré : l'ancienne version a été remise." true
	fail failed "Nouvelle version en échec, ancienne version remise."
}

# Sortie imprévue (set -e) pendant l'installation : pas d'état « en cours » sans fin.
on_exit() {
	code=$?
	if [ "$code" -ne 0 ] && busy; then
		set_apply failed 0 failed "L'installation des mises à jour a échoué. Réessayez plus tard."
	fi
	if [ "$code" -ne 0 ] && upgrade_busy; then
		set_upgrade failed "$(jq -r '.version' "$UPGRADE_FILE")" failed "L'installation de la nouvelle version a échoué. Réessayez plus tard."
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
appliance)
	appliance_detach "${2:-}" "${3:-}"
	appliance "$2" "$3"
	;;
*) fail invalid "usage : updates.sh status | refresh | apply | appliance <archive> <version>" ;;
esac
