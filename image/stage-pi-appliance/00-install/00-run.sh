#!/bin/bash
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
