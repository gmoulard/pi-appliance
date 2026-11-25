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

# Archive de mise à jour de pi-appliance, publiée avec chaque version à côté
# de l'image et installée par la console (updates.sh appliance) :
#
#   image/update-archive.sh <binaire pi-appliance arm64> <version> [dossier de sortie]
#
# Sortie : pi-appliance-<version>-arm64.tar.gz (le binaire « pi-appliance » et
# la couche système « system/ », sans ses tests) et sa somme de contrôle
# .sha256. Le binaire doit annoncer cette version (pi-appliance -version).

set -eu

if [ $# -lt 2 ] || [ ! -f "$1" ]; then
	echo "usage : image/update-archive.sh <binaire pi-appliance arm64> <version> [dossier de sortie]" >&2
	exit 2
fi
binary=$(realpath "$1")
version=$2
repo=$(cd "$(dirname "$0")/.." && pwd)
out=$(realpath -m "${3:-$repo/image/deploy}")

if [ "$(od -An -t x1 -j 18 -N 2 "$binary" | tr -d ' ')" != b700 ]; then
	echo "$binary n'est pas un binaire arm64 : GOOS=linux GOARCH=arm64 go build ./cmd/pi-appliance" >&2
	exit 1
fi

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
install -m 0755 "$binary" "$stage/pi-appliance"
cp -a "$repo/system" "$stage/system"
rm -rf "$stage/system/tests"
mkdir -p "$out"
name=pi-appliance-$version-arm64.tar.gz
tar -C "$stage" --owner=0 --group=0 -czf "$out/$name" pi-appliance system
(cd "$out" && sha256sum "$name" >"$name.sha256")
echo "$out/$name"
