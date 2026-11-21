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

# Installe la couche système de pi-appliance sur Raspberry Pi OS Lite 64 bits :
# Docker et Docker Compose, la console, Traefik, le point d'accès Wi-Fi et les
# secrets du premier démarrage. Utilisé par la construction de l'image (pi-gen,
# dans un chroot) ; marche aussi sur un Pi déjà installé, en root :
#
#   sudo sh system/install.sh <binaire pi-appliance arm64> [archive de l'image Traefik]
#
# Mise à jour de pi-appliance sur un Pi installé (updates.sh appliance) :
#
#   sh system/install.sh --update <binaire pi-appliance arm64>
#
# remplace la console et les fichiers de system/ sans réinstaller ni
# redémarrer Docker (les services continuent de tourner) ; seuls Traefik,
# avahi et la console redémarrent.
#
# Sans archive, Traefik est téléchargé au premier démarrage (internet nécessaire).
# N'écrit aucun secret : ils sont créés sur le Pi au démarrage (secrets.sh).

set -eu

update=0
if [ "${1:-}" = --update ]; then
	update=1
	shift
fi
if [ "$(id -u)" -ne 0 ]; then
	echo "à lancer en root (sudo)" >&2
	exit 1
fi
if [ $# -lt 1 ] || [ ! -f "$1" ]; then
	echo "usage : install.sh <binaire pi-appliance arm64> [archive de l'image Traefik]" >&2
	exit 2
fi
binary=$1
archive=${2:-}
here=$(cd "$(dirname "$0")" && pwd)

# Clé du dépôt Docker (https://docs.docker.com/engine/install/debian/), vérifiée.
DOCKER_KEY_FINGERPRINT=9DC858229FC7DD38854AE2D88D81803C0EBFCD88

PACKAGES="ca-certificates curl gnupg jq openssl iw network-manager dnsmasq-base avahi-daemon avahi-utils fdisk e2fsprogs nftables"

export DEBIAN_FRONTEND=noninteractive
# NE PAS sauter les paquets à la mise à jour : un paquet ajouté par une nouvelle
# version (avahi-utils en 0.1.3) manquerait sur les Pi mis à jour depuis la console.
if [ "$update" = 1 ]; then
	missing=
	for package in $PACKAGES; do
		if ! dpkg-query -W -f '${Status}' "$package" 2>/dev/null | grep -q 'install ok installed'; then
			missing="$missing $package"
		fi
	done
	if [ -n "$missing" ]; then
		# shellcheck disable=SC2086
		if ! { apt-get update && apt-get install -y --no-install-recommends $missing; }; then
			echo "paquets non installés :$missing" >&2
		fi
	fi
fi
if [ "$update" = 0 ]; then
	apt-get update
	# shellcheck disable=SC2086
	apt-get install -y --no-install-recommends $PACKAGES

	install -m 0755 -d /etc/apt/keyrings
	curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc.tmp
	fingerprints=$(gpg --show-keys --with-colons /etc/apt/keyrings/docker.asc.tmp | awk -F: '$1 == "fpr" { print $10 }')
	if ! printf '%s\n' "$fingerprints" | grep -qx "$DOCKER_KEY_FINGERPRINT"; then
		echo "clé du dépôt Docker inattendue : $fingerprints" >&2
		rm -f /etc/apt/keyrings/docker.asc.tmp
		exit 1
	fi
	mv /etc/apt/keyrings/docker.asc.tmp /etc/apt/keyrings/docker.asc
	chmod 0644 /etc/apt/keyrings/docker.asc
	# shellcheck source=/dev/null
	codename=$(. /etc/os-release && echo "$VERSION_CODENAME")
	echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $codename stable" \
		>/etc/apt/sources.list.d/docker.list
	apt-get update
	apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
fi

# Données des services sur un disque USB (storage.sh) : le réglage de Docker
# survit à une réinstallation, sinon Docker repartirait à vide sur la carte SD.
data_root=$(jq -r '."data-root" // empty' /etc/docker/daemon.json 2>/dev/null || true)

# Fichiers de system/, aux mêmes chemins sur le Pi.
for dir in etc usr; do
	(cd "$here/$dir" && find . -type d) | while read -r d; do
		install -d -m 0755 "/$dir/$d"
	done
	(cd "$here/$dir" && find . -type f) | while read -r f; do
		case "$f" in
		*.sh) mode=0755 ;;
		*.nmconnection) mode=0600 ;; # NetworkManager refuse les connexions lisibles par tous
		*) mode=0644 ;;
		esac
		install -m "$mode" "$here/$dir/$f" "/$dir/$f"
	done
done
if [ -n "$data_root" ]; then
	jq --arg root "$data_root" '. + {"data-root": $root}' /etc/docker/daemon.json >/etc/docker/daemon.json.tmp
	mv /etc/docker/daemon.json.tmp /etc/docker/daemon.json
fi
install -m 0755 "$binary" /usr/local/bin/pi-appliance
if [ -n "$archive" ]; then
	install -D -m 0644 "$archive" /usr/local/share/pi-appliance/traefik-image.tar
fi

# Annonce mDNS (pi-appliance.local) sur les seules interfaces du Pi : sans
# cela, avahi annonce aussi les adresses internes de Docker, injoignables.
if [ -f /etc/avahi/avahi-daemon.conf ]; then
	sed -i 's/^#\?allow-interfaces=.*/allow-interfaces=eth0,eth1,wlan0/' /etc/avahi/avahi-daemon.conf
fi

systemctl enable docker.service NetworkManager.service avahi-daemon.service \
	pi-appliance-secrets.service pi-appliance-traefik.service \
	pi-appliance-firewall.service pi-appliance-ap.service pi-appliance.service \
	pi-appliance-mdns.service pi-appliance-traefik-check.timer
# Rien n'est mis à jour sans l'accord de l'administrateur (spécification 9.2) :
# pas d'installation automatique par apt, les correctifs passent par la console
# (updates.sh).
systemctl mask apt-daily-upgrade.timer apt-daily-upgrade.service

# SSH, s'il a été choisi dans Imager : clés seulement depuis internet
# (sshd_config.d/10-pi-appliance.conf). Relu seulement si la configuration est
# valable, pour ne jamais couper un SSH qui marche.
reload_ssh() {
	if [ -x /usr/sbin/sshd ] && /usr/sbin/sshd -t 2>/dev/null; then
		systemctl try-reload-or-restart ssh.service || true
	fi
}

if [ "$update" = 1 ]; then
	reload_ssh
	systemctl daemon-reload
	systemctl restart avahi-daemon.service || true
	systemctl restart pi-appliance-traefik.service pi-appliance.service pi-appliance-mdns.service
	echo "pi-appliance mis à jour"
	exit 0
fi

# Sur un Pi en marche (pas dans le chroot de pi-gen) : démarrer tout de suite.
if [ -d /run/systemd/system ] && ! systemd-detect-virt --chroot >/dev/null 2>&1; then
	systemctl daemon-reload
	reload_ssh
	systemctl reload NetworkManager.service || true
	systemctl restart avahi-daemon.service || true
	systemctl restart docker.service pi-appliance-secrets.service
	systemctl restart pi-appliance-traefik.service pi-appliance.service pi-appliance-ap.service pi-appliance-mdns.service
fi
echo "pi-appliance installé"
