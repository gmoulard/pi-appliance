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

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "backup-disk : tous les tests passent"
