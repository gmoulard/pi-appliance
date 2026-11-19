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

# Teste les règles console/services avec Traefik réel et des clients sur des
# plages privées et hors liste autorisée. Nécessite Docker et les images Traefik/Python.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
IMAGE=traefik:v3.7.19
PYTHON_IMAGE=python:3.12-alpine
PREFIX=pi-appliance-console-test-$$
NET10=$PREFIX-10
NET172=$PREFIX-172
NET192=$PREFIX-192
NET_PUBLIC=$PREFIX-public
PROXY=$PREFIX-traefik
BACKEND=$PREFIX-backend
TMP=$(mktemp -d)
if command -v cygpath >/dev/null 2>&1; then
	VOLUME_TMP=$(cygpath -m "$TMP")
	export MSYS_NO_PATHCONV=1
else
	VOLUME_TMP=$TMP
fi

cleanup() {
	docker rm -f "$PROXY" "$BACKEND" >/dev/null 2>&1 || true
	docker network rm "$NET10" "$NET172" "$NET192" "$NET_PUBLIC" >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup 0 HUP INT TERM

docker pull "$IMAGE"
docker pull "$PYTHON_IMAGE"
docker network create --subnet 10.111.31.0/24 "$NET10" >/dev/null
docker network create --subnet 172.30.111.0/24 "$NET172" >/dev/null
docker network create --subnet 192.168.244.0/24 "$NET192" >/dev/null
docker network create --subnet 203.0.113.0/24 "$NET_PUBLIC" >/dev/null

mkdir -p "$TMP/dynamic"
awk '/^tls:$/ { exit } { print }' \
	"$ROOT/system/etc/pi-appliance/traefik/dynamic/console.yml" >"$TMP/dynamic/console.yml"

cat >"$TMP/traefik.yml" <<'EOF'
entryPoints:
  websecure:
    address: ":443"
providers:
  file:
    directory: /etc/traefik/dynamic
    watch: true
log:
  level: ERROR
EOF

cat >"$TMP/dynamic/service.yml" <<EOF
http:
  routers:
    test-service:
      entryPoints: [websecure]
      rule: "Host(\`service.test\`)"
      service: test-service
      tls: {}
  services:
    test-service:
      loadBalancer:
        servers:
          - url: "http://$BACKEND:80"
EOF

cat >"$TMP/request.py" <<'EOF'
import ssl
import sys
import urllib.error
import urllib.request

request = urllib.request.Request(sys.argv[1], headers={"Host": sys.argv[2]})
try:
    response = urllib.request.urlopen(request, context=ssl._create_unverified_context())
    status, body = response.status, response.read()
except urllib.error.HTTPError as error:
    status, body = error.code, error.read()

expected = int(sys.argv[3])
if status != expected:
    raise SystemExit(f"HTTP {status}, attendu {expected}")
if sys.argv[4] and sys.argv[4].encode() not in body:
    raise SystemExit(f"réponse ne contenant pas {sys.argv[4]!r}")
print(status)
EOF

docker run -d --name "$BACKEND" --network "$NET10" --network-alias "$BACKEND" "$PYTHON_IMAGE" \
	python -c 'from http.server import BaseHTTPRequestHandler,HTTPServer; H=type("H",(BaseHTTPRequestHandler,),{"do_GET":lambda s:(s.send_response(200),s.end_headers(),s.wfile.write(b"service backend"))}); HTTPServer(("0.0.0.0",80),H).serve_forever()' >/dev/null
docker run -d --name "$PROXY" --network "$NET10" --network-alias "$PROXY" \
	-v "$VOLUME_TMP/traefik.yml:/etc/traefik/traefik.yml:ro" \
	-v "$VOLUME_TMP/dynamic:/etc/traefik/dynamic:ro" \
	"$IMAGE" --configFile=/etc/traefik/traefik.yml >/dev/null
docker network connect --alias "$PROXY" "$NET172" "$PROXY"
docker network connect --alias "$PROXY" "$NET192" "$PROXY"
docker network connect --alias "$PROXY" "$NET_PUBLIC" "$PROXY"

request() {
	docker run --rm --network "$1" -v "$VOLUME_TMP/request.py:/request.py:ro" "$PYTHON_IMAGE" \
		python /request.py "https://$PROXY/pi-appliance/" "$2" "$3" "${4:-}"
}

ready=0
attempt=0
while [ "$attempt" -lt 30 ]; do
	if request "$NET10" console.test 502 >/dev/null 2>&1; then
		ready=1
		break
	fi
	attempt=$((attempt + 1))
	sleep 1
done
if [ "$ready" -ne 1 ]; then
	docker inspect --format '{{.State.Status}} {{.State.Error}}' "$PROXY" >&2 || true
	docker logs "$PROXY" >&2 || true
	request "$NET10" console.test 502 >&2 || true
	echo "Traefik n'a pas chargé le routeur de console" >&2
	exit 1
fi

for network in "$NET10" "$NET172" "$NET192"; do
	request "$network" console.test 502 >/dev/null
done
request "$NET_PUBLIC" console.test 404 >/dev/null
request "$NET10" service.test 200 "service backend" >/dev/null
request "$NET_PUBLIC" service.test 200 "service backend" >/dev/null
echo "ok"
