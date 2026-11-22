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

# Construit l'image pi-appliance avec pi-gen (Docker nécessaire, une heure
# environ, 15 Go de disque libre) :
#
#   image/build.sh <binaire pi-appliance arm64> [dossier de sortie]
#
# Sortie : pi-appliance-<version>.img.xz et sa somme de contrôle .sha256.
# Variables : IMG_VERSION (version dans le nom du fichier, date du jour par
# défaut), PI_GEN_REF (version de pi-gen), TRAEFIK_ARCHIVE (image Traefik déjà
# exportée, sinon téléchargée), DRY_RUN=1 (prépare et vérifie pi-gen sans
# construire, sans Docker).

set -eu

if [ $# -lt 1 ] || [ ! -f "$1" ]; then
	echo "usage : image/build.sh <binaire pi-appliance arm64> [dossier de sortie]" >&2
	exit 2
fi
binary=$(realpath "$1")
here=$(cd "$(dirname "$0")" && pwd)
repo=$(dirname "$here")
out=$(realpath -m "${2:-$here/deploy}")
work=${WORK_DIR:-$here/work}
# Raspberry Pi OS Trixie, 64 bits. Changer de version : une étiquette
# « *-raspios-trixie-arm64 » de https://github.com/RPi-Distro/pi-gen/tags.
PI_GEN_REF=${PI_GEN_REF:-2026-10-06-raspios-trixie-arm64}
IMG_VERSION=${IMG_VERSION:-$(date +%Y-%m-%d)}
DRY_RUN=${DRY_RUN:-0}

if [ "$(od -An -t x1 -j 18 -N 2 "$binary" | tr -d ' ')" != b700 ]; then
	echo "$binary n'est pas un binaire arm64 : GOOS=linux GOARCH=arm64 go build ./cmd/pi-appliance" >&2
	exit 1
fi

pigen=$work/pi-gen
rm -rf "$pigen"
mkdir -p "$work"
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$PI_GEN_REF" https://github.com/RPi-Distro/pi-gen "$pigen"

stage=$pigen/stage-pi-appliance
cp -a "$here/stage-pi-appliance" "$stage"
files=$stage/00-install/files
mkdir -p "$files"
cp -a "$repo/system" "$files/system"
rm -rf "$files/system/tests"
install -m 0755 "$binary" "$files/pi-appliance"

# Image Traefik, version fixée dans compose.yaml, embarquée pour que le portail
# marche sans internet au premier démarrage.
traefik=$(sed -n 's/^[[:space:]]*image:[[:space:]]*//p' "$repo/system/etc/pi-appliance/traefik/compose.yaml" | head -n 1)
if [ -n "${TRAEFIK_ARCHIVE:-}" ]; then
	cp "$TRAEFIK_ARCHIVE" "$files/traefik-image.tar"
elif [ "$DRY_RUN" = 1 ]; then
	: >"$files/traefik-image.tar"
else
	docker pull --quiet --platform linux/arm64 "$traefik"
	docker save --platform linux/arm64 -o "$files/traefik-image.tar" "$traefik"
fi

# Seule l'image de la dernière étape est exportée, pas celle de Raspberry Pi OS Lite.
touch "$pigen/stage2/SKIP_IMAGES"
{
	cat "$here/config"
	echo "IMG_FILENAME=pi-appliance-$IMG_VERSION"
	echo "ARCHIVE_FILENAME=pi-appliance-$IMG_VERSION"
} >"$pigen/config"

# Vérifications de l'arborescence de pi-gen, aussi faites par DRY_RUN=1.
for script in "$stage/prerun.sh" "$stage"/*/00-run.sh; do
	bash -n "$script"
	[ -x "$script" ] || { echo "$script n'est pas exécutable : pi-gen l'ignorerait" >&2; exit 1; }
done
stages=$(sed -n 's/^STAGE_LIST="\(.*\)"/\1/p' "$here/config")
for s in $stages; do
	[ -d "$pigen/$s" ] || { echo "étape $s introuvable dans pi-gen $PI_GEN_REF" >&2; exit 1; }
done
[ -f "$stage/EXPORT_IMAGE" ] || { echo "EXPORT_IMAGE manquant" >&2; exit 1; }
sh -n "$files/system/install.sh"

if [ "$DRY_RUN" = 1 ]; then
	echo "pi-gen $PI_GEN_REF prêt dans $pigen (DRY_RUN : pas de construction)"
	exit 0
fi

(cd "$pigen" && ./build-docker.sh)

mkdir -p "$out"
for img in "$pigen"/deploy/*.img.xz; do
	name=$(basename "$img")
	mv "$img" "$out/$name"
	(cd "$out" && sha256sum "$name" >"$name.sha256")
	echo "image : $out/$name"
done
