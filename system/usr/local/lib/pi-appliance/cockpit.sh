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

# Cockpit, administration du système du Pi depuis le navigateur (spécification
# 6.4), installé à la demande de l'administrateur : paquets Debian, pas un
# conteneur. Toujours un objet JSON sur la sortie standard ; code de retour 0
# si la demande a réussi, 1 sinon (champs « error » et « message »).
#
#   cockpit.sh status     installé ou non, installation ou retrait en cours
#   cockpit.sh install    mot de passe du compte pi-admin sur l'entrée standard ;
#                         crée le compte puis installe les paquets
#   cockpit.sh password   nouveau mot de passe du compte pi-admin sur l'entrée standard
#   cockpit.sh remove     retire les paquets et le compte pi-admin
#
# Le mot de passe n'est jamais écrit ailleurs que dans /etc/shadow (chpasswd).
# L'installation et le retrait durent quelques minutes : ils se relancent hors
# du service de la console (systemd-run) et répondent tout de suite ;
# l'avancement se lit dans « status » (champ « job »).
#
# Paquets : sans les paquets recommandés, et sans cockpit-packagekit : les
# mises à jour du système passent par la console et l'accord de
# l'administrateur (updates.sh, spécification 9.2). cockpit-networkmanager
# passe par NetworkManager, qui garde la main sur le réseau (network.sh) ;
# cockpit-storaged (udisks2) ne monte rien de lui-même : les disques USB
# restent montés par storage.sh et backup-disk.sh.
#
# Écoute et accès : cockpit.socket.d/pi-appliance.conf (127.0.0.1:9090),
# /etc/cockpit/cockpit.conf (sous /cockpit/), Traefik dynamic/cockpit.yml.

# shellcheck source-path=SCRIPTDIR
set -eu

PI_STATE=${PI_STATE:-/var/lib/pi-appliance/system}
JOB_FILE=$PI_STATE/cockpit-job.json
JOB_LOG=$PI_STATE/cockpit.log
COCKPIT_USER=${COCKPIT_USER:-pi-admin}
SSHD_CONF=${SSHD_CONF:-/etc/ssh/sshd_config.d/pi-appliance-cockpit.conf}
PACKAGES="cockpit-ws cockpit-system cockpit-bridge cockpit-networkmanager cockpit-storaged"
PASSWORD_MIN=12
PASSWORD_MAX=128

export LC_ALL=C DEBIAN_FRONTEND=noninteractive

# fail <code> <message>
fail() {
	jq -n --arg error "$1" --arg message "$2" '{ok: false, error: $error, message: $message}'
	exit 1
}

now() {
	date -u +%Y-%m-%dT%H:%M:%SZ
}

# set_job <action> <état> [code] [message] : installation ou retrait, lu par « status ».
set_job() {
	mkdir -p "$PI_STATE"
	jq -n --arg action "$1" --arg state "$2" --arg error "${3:-}" --arg message "${4:-}" --arg at "$(now)" \
		'{action: $action, state: $state, error: $error, message: $message, at: $at}' >"$JOB_FILE.tmp"
	mv "$JOB_FILE.tmp" "$JOB_FILE"
}

busy() {
	case "$(jq -r '.state // ""' "$JOB_FILE" 2>/dev/null || true)" in
	starting | running) return 0 ;;
	esac
	return 1
}

installed() {
	# shellcheck disable=SC2016 # format de dpkg-query, pas une variable
	dpkg-query -W -f='${Status}' cockpit-ws 2>/dev/null | grep -q 'install ok installed'
}

user_exists() {
	id -u "$COCKPIT_USER" >/dev/null 2>&1
}

status() {
	job='null'
	if [ -s "$JOB_FILE" ]; then
		# Opération interrompue (Pi éteint) : le service n'existe plus.
		if busy && ! systemctl is-active --quiet pi-appliance-cockpit.service; then
			set_job "$(jq -r '.action' "$JOB_FILE")" failed interrupted "L'opération a été interrompue. Relancez-la."
		fi
		job=$(cat "$JOB_FILE")
	fi
	is_installed=false
	if installed; then is_installed=true; fi
	active=false
	if [ "$is_installed" = true ] && systemctl is-active --quiet cockpit.socket; then active=true; fi
	has_account=false
	if user_exists; then has_account=true; fi
	jq -n --argjson installed "$is_installed" --argjson active "$active" --argjson account "$has_account" \
		--arg user "$COCKPIT_USER" --argjson job "$job" \
		'{installed: $installed, active: $active, account: $account, user: $user, path: "/cockpit/", job: $job}'
}

# read_password : une ligne sur l'entrée standard, dans $password.
read_password() {
	password=''
	IFS= read -r password || true
	if [ "${#password}" -lt "$PASSWORD_MIN" ]; then
		fail invalid "Le mot de passe doit faire au moins $PASSWORD_MIN caractères."
	fi
	if [ "${#password}" -gt "$PASSWORD_MAX" ]; then
		fail invalid "Le mot de passe est trop long."
	fi
}

set_password() {
	if ! printf '%s:%s\n' "$COCKPIT_USER" "$password" | chpasswd >/dev/null 2>&1; then
		fail failed "Le mot de passe n'a pas pu être changé."
	fi
}

# account : compte système de Cockpit. Groupe sudo : l'« accès administrateur »
# de Cockpit redemande ce mot de passe. Jamais ouvert par SSH.
account() {
	mkdir -p "$PI_STATE"
	if ! user_exists; then
		if ! useradd --create-home --shell /bin/bash --groups sudo \
			--comment "Administration du systeme (pi-appliance)" "$COCKPIT_USER" >>"$JOB_LOG" 2>&1; then
			fail failed "Le compte d'administration du système n'a pas pu être créé."
		fi
	fi
	mkdir -p "$(dirname "$SSHD_CONF")"
	printf '# pi-appliance : compte de Cockpit, jamais ouvert par SSH (cockpit.sh).\nDenyUsers %s\n' "$COCKPIT_USER" >"$SSHD_CONF"
	set_password
}

# detach <action> : relance l'action hors du service de la console.
detach() {
	set_job "$1" starting
	if ! systemd-run --quiet --collect --unit=pi-appliance-cockpit \
		--setenv=PI_COCKPIT_DETACHED=1 "$0" "$1" >/dev/null 2>&1; then
		set_job "$1" failed failed "L'opération n'a pas pu démarrer."
		fail failed "L'opération n'a pas pu démarrer."
	fi
	jq -n '{ok: true, started: true}'
}

install_start() {
	if busy; then fail busy "Une installation est déjà en cours."; fi
	read_password
	account
	detach install
}

# shellcheck disable=SC2086 # liste des paquets
install_packages() {
	set_job install running
	if ! apt-get -qq -o DPkg::Lock::Timeout=300 update >>"$JOB_LOG" 2>&1; then
		set_job install failed refresh-failed "Les dépôts de Raspberry Pi OS ne répondent pas. Le Pi est-il relié à internet ?"
		fail refresh-failed "Dépôts injoignables."
	fi
	if ! apt-get -y -q --no-install-recommends -o DPkg::Lock::Timeout=300 \
		-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold \
		install $PACKAGES >>"$JOB_LOG" 2>&1; then
		set_job install failed failed "L'installation a échoué. Réessayez plus tard."
		fail failed "Installation en échec."
	fi
	systemctl daemon-reload || true
	if ! systemctl enable --now cockpit.socket >>"$JOB_LOG" 2>&1; then
		set_job install failed failed "L'administration du système est installée mais ne démarre pas."
		fail failed "cockpit.socket ne démarre pas."
	fi
	# Réglages relus (cockpit.conf, écoute) si Cockpit tournait déjà.
	systemctl restart cockpit.socket >>"$JOB_LOG" 2>&1 || true
	set_job install "done"
	jq -n '{ok: true}'
}

password() {
	if ! user_exists; then fail invalid "L'administration du système n'est pas installée."; fi
	read_password
	set_password
	jq -n '{ok: true}'
}

remove_start() {
	if busy; then fail busy "Une installation est déjà en cours."; fi
	detach remove
}

# shellcheck disable=SC2086 # liste des paquets
remove_packages() {
	set_job remove running
	systemctl disable --now cockpit.socket cockpit.service >>"$JOB_LOG" 2>&1 || true
	if installed && ! apt-get -y -q -o DPkg::Lock::Timeout=300 purge $PACKAGES >>"$JOB_LOG" 2>&1; then
		set_job remove failed failed "Le retrait a échoué. Réessayez plus tard."
		fail failed "Retrait en échec."
	fi
	apt-get -y -q -o DPkg::Lock::Timeout=300 autoremove >>"$JOB_LOG" 2>&1 || true
	if user_exists; then
		loginctl terminate-user "$COCKPIT_USER" >>"$JOB_LOG" 2>&1 || true
		if ! userdel --remove "$COCKPIT_USER" >>"$JOB_LOG" 2>&1; then
			set_job remove failed failed "Le compte d'administration du système n'a pas pu être supprimé. Redémarrez le Pi et réessayez."
			fail failed "Compte non supprimé."
		fi
	fi
	rm -f "$SSHD_CONF"
	set_job remove "done"
	jq -n '{ok: true}'
}

# Sortie imprévue (set -e) pendant l'opération : pas d'état « en cours » sans fin.
on_exit() {
	code=$?
	if [ "$code" -ne 0 ] && busy; then
		set_job "$(jq -r '.action' "$JOB_FILE")" failed failed "L'opération a échoué. Réessayez plus tard."
	fi
}

# Lu par « . » depuis les tests : les fonctions seulement.
# shellcheck disable=SC2317
if [ "${COCKPIT_LIB_ONLY:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

if [ "${PI_COCKPIT_DETACHED:-}" = 1 ]; then trap on_exit EXIT; fi

case "${1:-}" in
status) status ;;
install)
	if [ "${PI_COCKPIT_DETACHED:-}" = 1 ]; then install_packages; else install_start; fi
	;;
password) password ;;
remove)
	if [ "${PI_COCKPIT_DETACHED:-}" = 1 ]; then remove_packages; else remove_start; fi
	;;
*) fail invalid "usage : cockpit.sh status | install | password | remove" ;;
esac
