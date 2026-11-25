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

# Démarre ou arrête Traefik, point d'entrée unique du Pi sur les ports 80 et 443
# (spécification, sections 8.4 et 10). Lancé par pi-appliance-traefik.service ;
# check, toutes les 2 minutes par pi-appliance-traefik-check.timer, recharge
# l'image depuis l'archive quand Traefik redémarre en boucle (fichier de l'image
# abîmé sur la carte SD, par exemple après une sous-tension).
#   traefik.sh start | stop | check

set -eu

COMPOSE=${COMPOSE:-/etc/pi-appliance/traefik/compose.yaml}
ARCHIVE=${ARCHIVE:-/usr/local/share/pi-appliance/traefik-image.tar}
ACME_EMAIL_FILE=${ACME_EMAIL_FILE:-/var/lib/pi-appliance/system/traefik/acme-email}
DOCKER=${DOCKER:-docker}
CONTAINER=${CONTAINER:-pi-appliance-traefik}
RUN_DIR=${RUN_DIR:-/run/pi-appliance}
REPAIR_FILE=${REPAIR_FILE:-/var/lib/pi-appliance/system/traefik/repaired}
LOOP_RESTARTS=${LOOP_RESTARTS:-3}
REPAIR_MINUTES=${REPAIR_MINUTES:-60}

image=$(sed -n 's/^[[:space:]]*image:[[:space:]]*//p' "$COMPOSE" | head -n 1)

load_env() {
	if [ -s "$ACME_EMAIL_FILE" ]; then
		PI_ACME_EMAIL=$(sed -n '1p' "$ACME_EMAIL_FILE")
		export PI_ACME_EMAIL
	fi
}

repair() {
	if [ ! -f "$ARCHIVE" ]; then
		echo "Traefik redémarre en boucle, mais l'archive $ARCHIVE manque : rien à recharger" >&2
		return 1
	fi
	echo "Traefik redémarre en boucle : image $image rechargée depuis $ARCHIVE"
	"$DOCKER" rm -f "$CONTAINER" >/dev/null 2>&1 || true
	"$DOCKER" rmi -f "$image" >/dev/null 2>&1 || true
	"$DOCKER" load -i "$ARCHIVE"
	load_env
	"$DOCKER" compose -f "$COMPOSE" up -d --remove-orphans
	mkdir -p "$(dirname "$REPAIR_FILE")"
	date -u +%Y-%m-%dT%H:%M:%SZ >"$REPAIR_FILE"
}

case "${1:-}" in
start)
	load_env
	# L'image de Traefik est livrée dans l'image du Pi : le portail marche sans internet.
	if ! "$DOCKER" image inspect "$image" >/dev/null 2>&1 && [ -f "$ARCHIVE" ]; then
		"$DOCKER" load -i "$ARCHIVE"
	fi
	exec "$DOCKER" compose -f "$COMPOSE" up -d --remove-orphans
	;;
stop)
	exec "$DOCKER" compose -f "$COMPOSE" down
	;;
check)
	count=$("$DOCKER" inspect -f '{{.RestartCount}}' "$CONTAINER" 2>/dev/null) || exit 0
	mkdir -p "$RUN_DIR"
	last=$(cat "$RUN_DIR/traefik-restarts" 2>/dev/null || echo "$count")
	echo "$count" >"$RUN_DIR/traefik-restarts"
	# NE PAS se fier à l'identifiant de l'image : un fichier abîmé sur la carte SD
	# le laisse intact ; seul le redémarrage en boucle le révèle.
	if [ "$count" -lt "$last" ] || [ $((count - last)) -lt "$LOOP_RESTARTS" ]; then
		exit 0
	fi
	if [ -n "$(find "$REPAIR_FILE" -mmin -"$REPAIR_MINUTES" 2>/dev/null)" ]; then
		echo "Traefik redémarre encore en boucle ($count redémarrages), image déjà rechargée il y a moins de $REPAIR_MINUTES minutes" >&2
		exit 1
	fi
	repair
	echo 0 >"$RUN_DIR/traefik-restarts"
	;;
*)
	echo "usage : traefik.sh start | stop | check" >&2
	exit 2
	;;
esac
