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

# Tests de la lecture des disques USB (storage.sh), sans Pi ni disque :
# sh system/tests/storage-test.sh

# shellcheck source-path=SCRIPTDIR
set -eu

LIB=$(dirname "$0")/../usr/local/lib/pi-appliance
failures=0

STORAGE_LIB_ONLY=1
# shellcheck source=../usr/local/lib/pi-appliance/storage.sh
. "$LIB/storage.sh"

# expect <nom> <filtre jq> <attendu>
expect() {
	got=$(printf '%s' "$lsblk" | usb_disks_json | jq -c "$2")
	if [ "$got" != "$3" ]; then
		echo "ÉCHEC $1 : $got, attendu $3"
		failures=$((failures + 1))
	fi
}

# Carte SD (mmcblk0, pas USB), disque USB en ext4, clé USB en FAT sans table de
# partitions, disque USB neuf sans partition.
lsblk='{"blockdevices": [
 {"name": "/dev/mmcblk0", "type": "disk", "tran": null, "size": 31914983424, "model": null, "fstype": null, "label": null, "mountpoint": null,
  "children": [{"name": "/dev/mmcblk0p2", "type": "part", "tran": null, "size": 31376000000, "model": null, "fstype": "ext4", "label": "rootfs", "mountpoint": "/"}]},
 {"name": "/dev/sda", "type": "disk", "tran": "usb", "size": 500107862016, "model": "  Samsung SSD T7 ", "fstype": null, "label": null, "mountpoint": null,
  "children": [{"name": "/dev/sda1", "type": "part", "tran": null, "size": 500106813440, "model": null, "fstype": "ext4", "label": "data", "mountpoint": null}]},
 {"name": "/dev/sdb", "type": "disk", "tran": "usb", "size": "16008609792", "model": "Cruzer", "fstype": "vfat", "label": "CLE", "mountpoint": "/media/cle"},
 {"name": "/dev/sdc", "type": "disk", "tran": "usb", "size": 1000204886016, "model": "WD", "fstype": null, "label": null, "mountpoint": null}
]}'

expect "seulement les disques USB" '[.[].device]' '["/dev/sda","/dev/sdb","/dev/sdc"]'
expect "partition ext4" '.[0] | [.model, .sizeMB, .partition, .fstype, .label, .mounted]' '["Samsung SSD T7",476940,"/dev/sda1","ext4","data",false]'
expect "clé sans table de partitions" '.[1] | [.sizeMB, .partition, .fstype, .mounted]' '[15267,"/dev/sdb","vfat",true]'
expect "disque neuf" '.[2] | [.partitions, .partition, .fstype]' '[0,null,""]'

lsblk='{"blockdevices": []}'
expect "aucun disque" '.' '[]'

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "storage : tous les tests passent"
