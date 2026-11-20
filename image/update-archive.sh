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
