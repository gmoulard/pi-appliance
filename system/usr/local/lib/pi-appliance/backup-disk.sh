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

# Disque USB des sauvegardes, pour la console (spécification 9.1) : un disque
# USB autre que celui des données, monté sur BACKUP_MOUNT. Un disque déjà en
# ext4 est utilisé tel quel ; sinon la console propose de le formater, avec
# accord. Toujours un objet JSON sur la sortie standard ; code de retour 0 si
# la demande a réussi, 1 sinon (champs « error » et « message »).
#
#   backup-disk.sh status              disques USB proposés, disque de sauvegarde
#   backup-disk.sh use /dev/sdb1       sauvegarder sur cette partition ext4
#   backup-disk.sh format /dev/sdb     effacer le disque, une partition ext4, puis « use »

# shellcheck source-path=SCRIPTDIR
set -eu

BACKUP_MOUNT=${BACKUP_MOUNT:-/mnt/pi-appliance-backup}

# Fonctions du disque des données : usb_disks, data_device, fail.
STORAGE_LIB_ONLY=1
# shellcheck source=storage.sh
. "${PI_LIB:-$(dirname "$0")}/storage.sh"

# Disque (chemin complet) qui porte les données des services ; vide sur la carte SD.
data_disk() {
	data=$(data_device)
	if [ -n "$data" ]; then
		lsblk -n -p -o PKNAME "$data" 2>/dev/null | head -n 1
	fi
}

# Partition montée comme disque de sauvegarde ; vide sinon.
backup_device() {
	findmnt -n -o SOURCE --mountpoint "$BACKUP_MOUNT" 2>/dev/null || true
}

# Liste des disques USB (usb_disks_json) sans le disque des données.
without_data_disk() {
	jq --arg data "$1" '[.[] | select(.device != $data)]'
}

status() {
	target=$(backup_device)
	usb_disks | without_data_disk "$(data_disk)" | jq --arg target "$target" --arg mount "$BACKUP_MOUNT" \
		'{disks: ., target: (if $target == "" then null else {device: $target, mountpoint: $mount} end)}'
}

# check_device <périphérique> <champ> : un disque USB de la liste, pas celui des données.
check_device() {
	case "$1" in
	/dev/[a-z]*) ;;
	*) fail invalid "Périphérique inconnu." ;;
	esac
	if ! usb_disks | without_data_disk "$(data_disk)" | jq -e --arg dev "$1" --arg field "$2" 'any(.[]; .[$field] == $dev)' >/dev/null; then
		fail not-usb "Ce disque n'est pas un disque USB branché au Pi, ou il porte déjà les données des services."
	fi
	if [ -n "$(backup_device)" ]; then
		fail already "Un disque de sauvegarde est déjà utilisé."
	fi
}

use_partition() {
	part=$1
	if [ "$(lsblk -n -o FSTYPE "$part" 2>/dev/null | head -n 1)" != ext4 ]; then
		fail not-ext4 "Ce disque doit d'abord être formaté."
	fi
	uuid=$(blkid -s UUID -o value "$part")
	mkdir -p "$BACKUP_MOUNT"
	if ! mount -o noatime "$part" "$BACKUP_MOUNT"; then
		fail mount-failed "Le disque n'a pas pu être ouvert."
	fi
	mkdir -p "$BACKUP_MOUNT/pi-appliance-backups"
	chmod 0700 "$BACKUP_MOUNT/pi-appliance-backups"
	# nofail : le Pi démarre sans le disque ; les sauvegardes attendent qu'on le rebranche.
	sed -i "\\# $BACKUP_MOUNT #d" /etc/fstab
	echo "UUID=$uuid $BACKUP_MOUNT ext4 defaults,noatime,nofail 0 2" >>/etc/fstab
	systemctl daemon-reload || true
	jq -n '{ok: true}'
}

format_disk() {
	disk=$1
	if usb_disks | jq -e --arg dev "$disk" 'any(.[]; .device == $dev and .mounted)' >/dev/null; then
		fail mounted "Ce disque est utilisé : débranchez-le et rebranchez-le, puis réessayez."
	fi
	wipefs --all --quiet "$disk"
	printf 'label: gpt\n,,L\n' | sfdisk --quiet "$disk"
	udevadm settle
	part=$(lsblk -n -p -o NAME,TYPE "$disk" | awk '$2 == "part" { print $1; exit }')
	if [ -z "$part" ]; then
		fail format-failed "Le formatage du disque a échoué."
	fi
	mkfs.ext4 -q -F -L pi-backup "$part"
	udevadm settle
	use_partition "$part"
}

# Lu par « . » depuis les tests : les fonctions seulement.
# shellcheck disable=SC2317
if [ "${BACKUP_LIB_ONLY:-}" = 1 ]; then return 0 2>/dev/null || exit 0; fi

case "${1:-}" in
status)
	status
	;;
use)
	check_device "${2:-}" partition
	use_partition "$2"
	;;
format)
	check_device "${2:-}" device
	format_disk "$2"
	;;
*)
	fail invalid "usage : backup-disk.sh status | use <partition> | format <disque>"
	;;
esac
