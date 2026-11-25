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

# Disque USB des données des services, pour la console (spécification 4) :
# disques USB branchés, déplacement des données de Docker (volumes des
# services et images) sur le disque, formatage en ext4 avec accord. Toujours un
# objet JSON sur la sortie standard ; code de retour 0 si la demande a réussi,
# 1 sinon (champs « error » et « message »).
#
#   storage.sh status              disques USB, disque des données, déplacement en cours
#   storage.sh use /dev/sda1       placer les données sur cette partition ext4
#   storage.sh format /dev/sda     effacer le disque, une partition ext4, puis « use »
#
# « use » et « format » durent plusieurs minutes et arrêtent Docker, donc
# Traefik et l'accès à la console : ils se relancent hors du service de la
# console (systemd-run) et répondent tout de suite ; l'avancement se lit dans
# « status » (champ « move »).

# shellcheck source-path=SCRIPTDIR
set -eu

PI_STATE=${PI_STATE:-/var/lib/pi-appliance/system}
DATA_MOUNT=${DATA_MOUNT:-/mnt/pi-appliance-data}
DOCKER_ROOT=${DOCKER_ROOT:-/var/lib/docker}
DAEMON_JSON=${DAEMON_JSON:-/etc/docker/daemon.json}
DOCKER_DROPIN=${DOCKER_DROPIN:-/etc/systemd/system/docker.service.d/pi-appliance-data.conf}
MOVE_FILE=$PI_STATE/storage-move.json

# fail <code> <message>
fail() {
	jq -n --arg error "$1" --arg message "$2" '{ok: false, error: $error, message: $message}'
	exit 1
}

# set_move <état> [code] [message] : état du déplacement, lu par « status ».
set_move() {
	mkdir -p "$PI_STATE"
	jq -n --arg state "$1" --arg error "${2:-}" --arg message "${3:-}" \
		--arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		'{state: $state, error: $error, message: $message, at: $at}' >"$MOVE_FILE.tmp"
	mv "$MOVE_FILE.tmp" "$MOVE_FILE"
}

# Sortie JSON de lsblk vers la liste des disques USB, une entrée par disque :
# sa première partition (ou le disque lui-même s'il porte un système de
# fichiers sans partition).
usb_disks_json() {
	jq '[.blockdevices[]? | select(.type == "disk" and .tran == "usb")
		| . as $disk
		| ((.children // []) | map(select(.type == "part"))) as $parts
		| ($parts[0] // (if $disk.fstype then $disk else null end)) as $part
		| {
			device: $disk.name,
			model: (($disk.model // "") | gsub("^\\s+|\\s+$"; "")),
			sizeMB: (($disk.size | tonumber) / 1048576 | floor),
			partitions: ($parts | length),
			partition: ($part.name // null),
			fstype: ($part.fstype // ""),
			label: ($part.label // ""),
			mounted: ([$disk, $parts[]] | any(.mountpoint != null and .mountpoint != ""))
		}]'
}

usb_disks() {
	lsblk --json --bytes --paths --output NAME,TYPE,TRAN,SIZE,MODEL,FSTYPE,LABEL,MOUNTPOINT | usb_disks_json
}

# Partition qui porte aujourd'hui les données ; vide si elles sont sur la carte SD.
data_device() {
	findmnt -n -o SOURCE --mountpoint "$DATA_MOUNT" 2>/dev/null || true
}

status() {
	move='null'
	if [ -s "$MOVE_FILE" ]; then
		case "$(jq -r '.state // ""' "$MOVE_FILE")" in
		starting | formatting | copying)
			# Copie interrompue (Pi éteint pendant la copie) : le service n'existe plus.
			if ! systemctl is-active --quiet pi-appliance-storage.service; then
				set_move failed interrupted "Le déplacement a été interrompu : les données sont restées sur la carte SD."
			fi
			;;
		esac
		move=$(cat "$MOVE_FILE")
	fi
	usb_disks | jq --arg data "$(data_device)" --arg mount "$DATA_MOUNT" --argjson move "$move" \
		'{disks: ., data: (if $data == "" then null else {device: $data, mountpoint: $mount} end), move: $move}'
}

# detach <commande> <périphérique> : relance le script hors du service de la console.
detach() {
	if [ "${PI_STORAGE_DETACHED:-}" = 1 ]; then return 0; fi
	case "$(jq -r '.state // ""' "$MOVE_FILE" 2>/dev/null || true)" in
	starting | formatting | copying) fail busy "Une copie des données est déjà en cours." ;;
	esac
	set_move starting
	if ! systemd-run --quiet --collect --unit=pi-appliance-storage \
		--setenv=PI_STORAGE_DETACHED=1 "$0" "$1" "$2" >/dev/null 2>&1; then
		set_move failed failed "Le déplacement n'a pas pu démarrer."
		fail failed "Le déplacement n'a pas pu démarrer."
	fi
	jq -n '{ok: true, started: true}'
	exit 0
}

# check_device <périphérique> <champ> : le périphérique doit être un disque
# USB de la liste (champ « device ») ou sa partition (champ « partition »).
check_device() {
	case "$1" in
	/dev/[a-z]*) ;;
	*) fail invalid "Périphérique inconnu." ;;
	esac
	if ! usb_disks | jq -e --arg dev "$1" --arg field "$2" 'any(.[]; .[$field] == $dev)' >/dev/null; then
		fail not-usb "Ce disque n'est pas un disque USB branché au Pi."
	fi
	if [ -n "$(data_device)" ]; then
		fail already "Les données sont déjà sur un disque USB."
	fi
}

# Retour à la carte SD après un échec : ancienne configuration de Docker.
restore() {
	if [ -f "$DAEMON_JSON.before-usb" ]; then mv "$DAEMON_JSON.before-usb" "$DAEMON_JSON"; fi
	rm -f "$DOCKER_DROPIN"
	sed -i "\\# $DATA_MOUNT #d" /etc/fstab
	systemctl daemon-reload
	rm -rf "${DATA_MOUNT:?}/docker"
	umount "$DATA_MOUNT" 2>/dev/null || true
	systemctl start docker.service pi-appliance-traefik.service || true
}

use_partition() {
	part=$1
	if [ "$(lsblk -n -o FSTYPE "$part" 2>/dev/null | head -n 1)" != ext4 ]; then
		set_move failed not-ext4 "Ce disque doit d'abord être formaté."
		fail not-ext4 "Ce disque doit d'abord être formaté."
	fi
	uuid=$(blkid -s UUID -o value "$part")
	set_move copying
	mkdir -p "$DATA_MOUNT"
	if ! findmnt -n --mountpoint "$DATA_MOUNT" >/dev/null 2>&1; then
		mount -o noatime "$part" "$DATA_MOUNT"
	fi
	# Place : ce que Docker occupe sur la carte SD, plus 10 %.
	need=$(du -sxk "$DOCKER_ROOT" | cut -f1)
	free=$(df -Pk "$DATA_MOUNT" | awk 'NR == 2 { print $4 }')
	if [ $((need + need / 10)) -gt "$free" ]; then
		umount "$DATA_MOUNT" || true
		set_move failed not-enough-space "Le disque USB n'a pas assez de place libre pour les données des services."
		fail not-enough-space "Le disque USB n'a pas assez de place libre pour les données des services."
	fi
	# Un ancien dossier docker sur le disque (autre Pi, essai précédent) est mis de côté.
	if [ -e "$DATA_MOUNT/docker" ]; then
		mv "$DATA_MOUNT/docker" "$DATA_MOUNT/docker.old-$(date +%Y%m%d%H%M%S)"
	fi
	sed -i "\\# $DATA_MOUNT #d" /etc/fstab
	echo "UUID=$uuid $DATA_MOUNT ext4 defaults,noatime,nofail 0 2" >>/etc/fstab
	systemctl stop pi-appliance-traefik.service docker.service docker.socket
	if ! cp -a "$DOCKER_ROOT" "$DATA_MOUNT/docker"; then
		restore
		set_move failed copy-failed "La copie sur le disque USB a échoué : les données restent sur la carte SD."
		fail copy-failed "La copie sur le disque USB a échoué : les données restent sur la carte SD."
	fi
	cp "$DAEMON_JSON" "$DAEMON_JSON.before-usb"
	jq --arg root "$DATA_MOUNT/docker" '. + {"data-root": $root}' "$DAEMON_JSON.before-usb" >"$DAEMON_JSON.tmp"
	mv "$DAEMON_JSON.tmp" "$DAEMON_JSON"
	# Sans le disque, Docker ne démarre pas : sinon il repartirait à vide sur la carte SD.
	mkdir -p "$(dirname "$DOCKER_DROPIN")"
	printf '[Unit]\nRequiresMountsFor=%s\n' "$DATA_MOUNT" >"$DOCKER_DROPIN"
	systemctl daemon-reload
	if ! systemctl start docker.service pi-appliance-traefik.service; then
		systemctl stop pi-appliance-traefik.service docker.service docker.socket || true
		restore
		set_move failed start-failed "Docker n'a pas démarré sur le disque USB : les données restent sur la carte SD."
		fail start-failed "Docker n'a pas démarré sur le disque USB : les données restent sur la carte SD."
	fi
	rm -f "$DAEMON_JSON.before-usb"
	# Place rendue sur la carte SD.
	rm -rf "${DOCKER_ROOT:?}"
	set_move "done"
	jq -n '{ok: true}'
}

format_disk() {
	disk=$1
	if usb_disks | jq -e --arg dev "$disk" 'any(.[]; .device == $dev and .mounted)' >/dev/null; then
		fail mounted "Ce disque est utilisé : débranchez-le et rebranchez-le, puis réessayez."
	fi
	set_move formatting
	wipefs --all --quiet "$disk"
	printf 'label: gpt\n,,L\n' | sfdisk --quiet "$disk"
	udevadm settle
	part=$(lsblk -n -p -o NAME,TYPE "$disk" | awk '$2 == "part" { print $1; exit }')
	if [ -z "$part" ]; then
		set_move failed format-failed "Le formatage du disque a échoué."
		fail format-failed "Le formatage du disque a échoué."
	fi
	mkfs.ext4 -q -F -L pi-appliance "$part"
	udevadm settle
	use_partition "$part"
}

# Sortie imprévue (set -e) pendant le déplacement : retour à la carte SD,
# pour ne laisser ni Docker arrêté ni un état « en cours » sans fin.
on_exit() {
	code=$?
	state=$(jq -r '.state // ""' "$MOVE_FILE" 2>/dev/null || true)
	case "$state" in
	starting | formatting | copying)
		if [ "$code" -ne 0 ]; then
			if [ "$state" = copying ]; then restore || true; fi
			set_move failed failed "Le déplacement a échoué : les données restent sur la carte SD."
		fi
		;;
	esac
}

# Lu par « . » depuis les tests : les fonctions seulement.
# shellcheck disable=SC2317
if [ "${STORAGE_LIB_ONLY:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

if [ "${PI_STORAGE_DETACHED:-}" = 1 ]; then trap on_exit EXIT; fi

case "${1:-}" in
status)
	status
	;;
use)
	check_device "${2:-}" partition
	detach use "$2"
	use_partition "$2"
	;;
format)
	check_device "${2:-}" device
	detach format "$2"
	format_disk "$2"
	;;
*)
	fail invalid "usage : storage.sh status | use <partition> | format <disque>"
	;;
esac
