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

# Secrets propres à ce Pi, créés au premier démarrage (spécification, sections 4,
# 7.1 et 8.4) et jamais présents dans l'image : chaque Pi a les siens.
#   - clé du Pi (Ed25519) : identifie l'appliance auprès du service d'adresses ;
#   - certificat local auto-signé, utilisé par Traefik tant qu'il n'y a pas de
#     certificat Let's Encrypt ; renouvelé 30 jours avant son expiration.
# Lancé à chaque démarrage par pi-appliance-secrets.service : ne fait rien si tout existe.

set -eu

PI_STATE=${PI_STATE:-/var/lib/pi-appliance/system}
TLS=$PI_STATE/tls
DAYS=825      # durée maximale acceptée par les navigateurs d'Apple
RENEW=2592000 # 30 jours, en secondes

umask 077
mkdir -p "$PI_STATE" "$TLS" "$PI_STATE/traefik"
chmod 700 "$PI_STATE" "$TLS"

if [ ! -s "$PI_STATE/device.key" ]; then
	openssl genpkey -algorithm ed25519 -out "$PI_STATE/device.key.tmp"
	openssl pkey -in "$PI_STATE/device.key.tmp" -pubout -out "$PI_STATE/device.pub"
	mv "$PI_STATE/device.key.tmp" "$PI_STATE/device.key"
	chmod 644 "$PI_STATE/device.pub"
	echo "clé du Pi créée : $PI_STATE/device.key"
fi

if [ -s "$TLS/local.crt" ] && [ -s "$TLS/local.key" ] &&
	openssl x509 -checkend "$RENEW" -noout -in "$TLS/local.crt" >/dev/null 2>&1; then
	exit 0
fi

host=$(hostname 2>/dev/null || echo pi-appliance)
names="DNS:pi-appliance.local,DNS:*.pi-appliance.local,DNS:pi-appliance,DNS:localhost"
if [ "$host" != pi-appliance ]; then
	names="$names,DNS:$host.local,DNS:*.$host.local,DNS:$host"
fi
# 10.42.0.1 : adresse du Pi sur son point d'accès Wi-Fi (portail de configuration).
names="$names,IP:127.0.0.1,IP:10.42.0.1"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
	-days "$DAYS" -subj "/O=pi-appliance/CN=$host.local" \
	-addext "subjectAltName=$names" \
	-addext "basicConstraints=critical,CA:FALSE" \
	-addext "keyUsage=critical,digitalSignature" \
	-addext "extendedKeyUsage=serverAuth" \
	-keyout "$TLS/local.key.tmp" -out "$TLS/local.crt.tmp" 2>/dev/null
mv "$TLS/local.key.tmp" "$TLS/local.key"
chmod 644 "$TLS/local.crt.tmp"
mv "$TLS/local.crt.tmp" "$TLS/local.crt"
echo "certificat local créé : $TLS/local.crt"
