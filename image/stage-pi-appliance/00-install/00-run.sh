#!/bin/bash
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

# Installe la couche pi-appliance dans l'image, par system/install.sh, le même
# script que pour une installation à la main. files/ est préparé par
# image/build.sh : system/, pi-appliance (binaire arm64), traefik-image.tar.
# /var/tmp et non /tmp : pi-gen monte un /tmp vide dans le chroot.

set -eu

dest="${ROOTFS_DIR}/var/tmp/pi-appliance-install"
rm -rf "${dest}"
mkdir -p "${dest}"
cp -a files/. "${dest}/"

on_chroot <<-CHROOT
	set -eu
	sh /var/tmp/pi-appliance-install/system/install.sh \
		/var/tmp/pi-appliance-install/pi-appliance \
		/var/tmp/pi-appliance-install/traefik-image.tar
	apt-get clean
CHROOT

rm -rf "${dest}"
