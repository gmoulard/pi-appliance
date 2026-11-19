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

# Démarre ou arrête Traefik, point d'entrée unique du Pi sur les ports 80 et 443
# (spécification, sections 8.4 et 10). Lancé par pi-appliance-traefik.service.
#   traefik.sh start | stop

set -eu

COMPOSE=${COMPOSE:-/etc/pi-appliance/traefik/compose.yaml}
ARCHIVE=${ARCHIVE:-/usr/local/share/pi-appliance/traefik-image.tar}

case "${1:-}" in
start)
	image=$(sed -n 's/^[[:space:]]*image:[[:space:]]*//p' "$COMPOSE" | head -n 1)
	# L'image de Traefik est livrée dans l'image du Pi : le portail marche sans internet.
	if ! docker image inspect "$image" >/dev/null 2>&1 && [ -f "$ARCHIVE" ]; then
		docker load -i "$ARCHIVE"
	fi
	exec docker compose -f "$COMPOSE" up -d --remove-orphans
	;;
stop)
	exec docker compose -f "$COMPOSE" down
	;;
*)
	echo "usage : traefik.sh start | stop" >&2
	exit 2
	;;
esac
