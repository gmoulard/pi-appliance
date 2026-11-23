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
#   cockpit.sh install    installe les paquets
#   cockpit.sh sync       comptes système des administrateurs de la console,
#                         une ligne « <nom> <mot de passe> » par administrateur
#                         sur l'entrée standard ; répond le compte système de
#                         chacun (« accounts ») ; les comptes qui ne sont plus
#                         listés sont supprimés
#   cockpit.sh remove     retire les paquets et les comptes système de Cockpit
#
# Connexion unique (spécification 6.4) : chaque administrateur de la console a
# son compte système, membre du groupe pia-cockpit et de sudo, avec un mot de
# passe tiré au hasard par la console, qui le donne elle-même à Cockpit par
# Traefik (api/auth/forward-cockpit) : on ouvre Cockpit sans se reconnecter.
# Le mot de passe n'est écrit ici que dans /etc/shadow (chpasswd).
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
# Groupe des comptes système créés pour Cockpit : seuls ceux-là sont modifiés
# ou supprimés, jamais un compte du Pi qui porterait le même nom.
COCKPIT_GROUP=${COCKPIT_GROUP:-pia-cockpit}
# Compte unique des versions d'avant la connexion unique (0.1.25 et avant).
LEGACY_USER=${LEGACY_USER:-pi-admin}
SSHD_CONF=${SSHD_CONF:-/etc/ssh/sshd_config.d/pi-appliance-cockpit.conf}
PACKAGES="cockpit-ws cockpit-system cockpit-bridge cockpit-networkmanager cockpit-storaged"
PASSWORD_MIN=16
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
	id -u "$1" >/dev/null 2>&1
}

# ours <compte> : compte créé par ce script (membre du groupe de Cockpit).
ours() {
	id -nG "$1" 2>/dev/null | tr ' ' '\n' | grep -qx "$COCKPIT_GROUP"
}

# members : comptes système de Cockpit, un par ligne.
members() {
	getent group "$COCKPIT_GROUP" | cut -d: -f4 | tr ',' '\n' | grep -v '^$' || true
}

# drop_user <compte> : ferme ses sessions et supprime le compte et son dossier.
drop_user() {
	loginctl terminate-user "$1" >>"$JOB_LOG" 2>&1 || true
	userdel --remove "$1" >>"$JOB_LOG" 2>&1
}

# drop_legacy : le compte pi-admin des anciennes versions, s'il vient d'elles.
drop_legacy() {
	if user_exists "$LEGACY_USER" && getent passwd "$LEGACY_USER" | cut -d: -f5 | grep -q 'pi-appliance'; then
		drop_user "$LEGACY_USER" || true
	fi
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
	accounts=$(members | jq -R . | jq -s .)
	jq -n --argjson installed "$is_installed" --argjson active "$active" --argjson accounts "$accounts" \
		--argjson job "$job" \
		'{installed: $installed, active: $active, accounts: $accounts, path: "/cockpit/", job: $job}'
}

# setup : groupe des comptes de Cockpit, jamais ouverts par SSH.
setup() {
	mkdir -p "$PI_STATE"
	if ! getent group "$COCKPIT_GROUP" >/dev/null 2>&1; then
		if ! groupadd --system "$COCKPIT_GROUP" >>"$JOB_LOG" 2>&1; then
			fail failed "Le groupe des comptes de Cockpit n'a pas pu être créé."
		fi
	fi
	mkdir -p "$(dirname "$SSHD_CONF")"
	printf '# pi-appliance : comptes de Cockpit, jamais ouverts par SSH (cockpit.sh).\nDenyGroups %s\n' "$COCKPIT_GROUP" >"$SSHD_CONF"
}

# unix_name <nom> : compte système d'un administrateur de la console. Son nom
# s'il est libre ou déjà à Cockpit ; sinon « pia-<nom> » (un compte du Pi du
# même nom n'est jamais touché) ; vide si aucun ne convient.
unix_name() {
	for candidate in "$1" "pia-$1"; do
		case "$candidate" in
		[a-z_]*) ;;
		*) continue ;;
		esac
		if [ "${#candidate}" -gt 32 ]; then continue; fi
		if ! user_exists "$candidate" || ours "$candidate"; then
			printf '%s' "$candidate"
			return 0
		fi
	done
}

# sync_accounts : un compte système par administrateur de la console (entrée
# standard : « <nom> <mot de passe> » par ligne), membre de sudo : l'accès
# administrateur de Cockpit reprend le mot de passe de la connexion
# (X-Superuser posé par Traefik).
sync_accounts() {
	setup
	drop_legacy
	keep=$(mktemp)
	result='{}'
	conflicts='[]'
	while IFS=' ' read -r name password; do
		[ -n "$name" ] || continue
		case "$name" in
		*[!a-z0-9._-]* | [!a-z0-9]*) rm -f "$keep"; fail invalid "Nom de compte refusé." ;;
		esac
		if [ "${#password}" -lt "$PASSWORD_MIN" ] || [ "${#password}" -gt "$PASSWORD_MAX" ]; then
			rm -f "$keep"
			fail invalid "Mot de passe refusé."
		fi
		unix=$(unix_name "$name")
		if [ -z "$unix" ]; then
			conflicts=$(printf '%s' "$conflicts" | jq -c --arg name "$name" '. + [$name]')
			continue
		fi
		if ! user_exists "$unix"; then
			if ! useradd --create-home --shell /bin/bash --groups "sudo,$COCKPIT_GROUP" \
				--comment "$name (Cockpit, pi-appliance)" "$unix" >>"$JOB_LOG" 2>&1; then
				rm -f "$keep"
				fail failed "Le compte système de $name n'a pas pu être créé."
			fi
		else
			usermod --append --groups "sudo,$COCKPIT_GROUP" "$unix" >>"$JOB_LOG" 2>&1 || true
		fi
		if ! printf '%s:%s\n' "$unix" "$password" | chpasswd >/dev/null 2>&1; then
			rm -f "$keep"
			fail failed "Le mot de passe du compte système de $name n'a pas pu être changé."
		fi
		printf '%s\n' "$unix" >>"$keep"
		result=$(printf '%s' "$result" | jq -c --arg name "$name" --arg unix "$unix" '. + {($name): $unix}')
	done
	# Administrateur retiré ou devenu simple utilisateur : plus de compte système.
	for unix in $(members); do
		if ! grep -qx "$unix" "$keep"; then
			drop_user "$unix" || true
		fi
	done
	rm -f "$keep"
	jq -n --argjson accounts "$result" --argjson conflicts "$conflicts" '{ok: true, accounts: $accounts, conflicts: $conflicts}'
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
	setup
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
		set_job install failed failed "Cockpit est installé mais ne démarre pas."
		fail failed "cockpit.socket ne démarre pas."
	fi
	# Réglages relus (cockpit.conf, écoute) si Cockpit tournait déjà.
	systemctl restart cockpit.socket >>"$JOB_LOG" 2>&1 || true
	set_job install "done"
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
	for unix in $(members); do
		if ! drop_user "$unix"; then
			set_job remove failed failed "Un compte système de Cockpit n'a pas pu être supprimé. Redémarrez le Pi et réessayez."
			fail failed "Compte non supprimé."
		fi
	done
	drop_legacy
	groupdel "$COCKPIT_GROUP" >>"$JOB_LOG" 2>&1 || true
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
sync) sync_accounts ;;
remove)
	if [ "${PI_COCKPIT_DETACHED:-}" = 1 ]; then remove_packages; else remove_start; fi
	;;
*) fail invalid "usage : cockpit.sh status | install | sync | remove" ;;
esac
