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

# Tests du choix du disque de sauvegarde (backup-disk.sh), sans Pi ni disque :
# sh system/tests/backup-disk-test.sh

# shellcheck source-path=SCRIPTDIR
set -eu

PI_LIB=$(dirname "$0")/../usr/local/lib/pi-appliance
failures=0

BACKUP_LIB_ONLY=1
# shellcheck source=../usr/local/lib/pi-appliance/backup-disk.sh
. "$PI_LIB/backup-disk.sh"

# expect <nom> <disque des données> <attendu>
expect() {
	got=$(printf '%s' "$lsblk" | usb_disks_json | without_data_disk "$2" | jq -c '[.[].device]')
	if [ "$got" != "$3" ]; then
		echo "ÉCHEC $1 : $got, attendu $3"
		failures=$((failures + 1))
	fi
}

lsblk='{"blockdevices": [
 {"name": "/dev/sda", "type": "disk", "tran": "usb", "size": 500107862016, "model": "SSD", "fstype": null, "label": null, "mountpoint": null,
  "children": [{"name": "/dev/sda1", "type": "part", "size": 500106813440, "fstype": "ext4", "label": "data", "mountpoint": "/mnt/pi-appliance-data"}]},
 {"name": "/dev/sdb", "type": "disk", "tran": "usb", "size": 1000204886016, "model": "WD", "fstype": null, "label": null, "mountpoint": null}
]}'

expect "données sur la carte SD : tous les disques" "" '["/dev/sda","/dev/sdb"]'
expect "sans le disque des données" "/dev/sda" '["/dev/sdb"]'

# Remise à zéro : le disque est oublié (fstab, démontage), jamais effacé.
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
FSTAB=$TMP/fstab
printf 'PARTUUID=1 / ext4 defaults 0 1\nUUID=abc %s ext4 defaults,noatime,nofail 0 2\n' "$BACKUP_MOUNT" >"$FSTAB"
backup_device() { echo /dev/sdb1; }
umount() { echo "$*" >>"$TMP/umount"; }
sync() { :; }
systemctl() { :; }
wipefs() { echo wipefs >>"$TMP/forbidden"; }
got=$(forget_disk)
if [ "$got" != '{"ok":true}' ] && [ "$(echo "$got" | jq -c .)" != '{"ok":true}' ]; then
	echo "ÉCHEC oubli : $got"
	failures=$((failures + 1))
fi
if grep -q "$BACKUP_MOUNT" "$FSTAB" || ! grep -q '^PARTUUID=1 ' "$FSTAB"; then
	echo "ÉCHEC oubli : fstab $(cat "$FSTAB")"
	failures=$((failures + 1))
fi
if [ "$(cat "$TMP/umount")" != "$BACKUP_MOUNT" ] || [ -e "$TMP/forbidden" ]; then
	echo "ÉCHEC oubli : démontage $(cat "$TMP/umount" 2>/dev/null)"
	failures=$((failures + 1))
fi

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "backup-disk : tous les tests passent"
