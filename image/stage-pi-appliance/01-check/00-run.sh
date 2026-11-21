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

# Vérifie qu'aucun secret ne part dans l'image (spécification 4 : aucun secret
# n'est commun à deux images) : ils sont créés sur chaque Pi au démarrage.

set -eu

fail() {
	echo "image refusée : $1" >&2
	exit 1
}

for f in \
	var/lib/pi-appliance/system/device.key \
	var/lib/pi-appliance/system/tls/local.key \
	var/lib/pi-appliance/accounts.json; do
	if [ -e "${ROOTFS_DIR}/${f}" ]; then
		fail "/${f} existe déjà"
	fi
done
if compgen -G "${ROOTFS_DIR}/etc/ssh/ssh_host_*_key" >/dev/null; then
	fail "clés SSH du serveur présentes dans /etc/ssh"
fi
for f in "${ROOTFS_DIR}"/etc/NetworkManager/system-connections/*; do
	if [ -e "${f}" ] && [ "$(basename "${f}")" != pi-appliance-ap.nmconnection ]; then
		fail "connexion réseau inattendue : ${f#"${ROOTFS_DIR}"}"
	fi
done

# Le binaire de la console doit être pour arm64 (ELF, machine 0xb7).
if [ "$(od -An -t x1 -j 18 -N 2 "${ROOTFS_DIR}/usr/local/bin/pi-appliance" | tr -d ' ')" != b700 ]; then
	fail "/usr/local/bin/pi-appliance n'est pas un binaire arm64"
fi
# Ligne de commande du Pi : « pia », lien relatif vers pi-appliance.
if [ "$(readlink "${ROOTFS_DIR}/usr/local/bin/pia" || true)" != pi-appliance ]; then
	fail "/usr/local/bin/pia n'est pas le lien vers pi-appliance (ligne de commande du Pi)"
fi
# NE PAS tester avec -e : les liens de systemd sont absolus (/usr/lib/...) et -e
# les suivrait dans le conteneur de pi-gen, pas dans l'image ; -L teste le lien.
for unit in docker pi-appliance-secrets pi-appliance-traefik pi-appliance-firewall pi-appliance-ap pi-appliance pi-appliance-mdns; do
	if [ ! -L "${ROOTFS_DIR}/etc/systemd/system/multi-user.target.wants/${unit}.service" ]; then
		fail "${unit}.service n'est pas activé au démarrage"
	fi
done
if ! grep -qx 'preserve_hostname: true' "${ROOTFS_DIR}/etc/cloud/cloud.cfg.d/99-pi-appliance.cfg" 2>/dev/null; then
	fail "cloud-init remplacerait le nom du Pi (99-pi-appliance.cfg manquant)"
fi
if ! grep -qx '  config: disabled' "${ROOTFS_DIR}/etc/cloud/cloud.cfg.d/99-pi-appliance.cfg"; then
	fail "cloud-init appliquerait le Wi-Fi des réglages de Raspberry Pi Imager (network: config: disabled manquant)"
fi
if [ ! -L "${ROOTFS_DIR}/etc/systemd/system/timers.target.wants/pi-appliance-traefik-check.timer" ]; then
	fail "pi-appliance-traefik-check.timer n'est pas activé au démarrage"
fi
echo "image vérifiée : aucun secret, console arm64, services activés"
