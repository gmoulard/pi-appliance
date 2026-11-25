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

# Tests de traefik.sh check (Traefik redémarré en boucle, image rechargée depuis
# l'archive), sans Docker : docker remplacé par un faux.
#   sh system/tests/traefik-repair-test.sh

set -eu

SCRIPT=$(cd "$(dirname "$0")/../usr/local/lib/pi-appliance" && pwd)/traefik.sh
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
failures=0

fail() {
	echo "ÉCHEC $1"
	failures=$((failures + 1))
}

cat >"$TMP/compose.yaml" <<'EOF'
services:
  traefik:
    image: traefik:v3.7.19
EOF
: >"$TMP/archive.tar"
cat >"$TMP/docker" <<'FAKE'
#!/bin/sh
echo "$*" >>"$CALLS"
case "$1" in
inspect)
	[ -f "$COUNT" ] || exit 1
	cat "$COUNT"
	;;
esac
FAKE
chmod +x "$TMP/docker"

export CALLS="$TMP/calls" COUNT="$TMP/count"
export DOCKER="$TMP/docker" COMPOSE="$TMP/compose.yaml" ARCHIVE="$TMP/archive.tar"
export RUN_DIR="$TMP/run" REPAIR_FILE="$TMP/state/repaired" ACME_EMAIL_FILE="$TMP/acme-email"

check() {
	: >"$CALLS"
	sh "$SCRIPT" check >"$TMP/out" 2>&1
}

check || fail "conteneur absent : $(cat "$TMP/out")"
grep -q "load" "$CALLS" && fail "conteneur absent : image rechargée"

echo 4 >"$COUNT"
check || fail "première vérification : $(cat "$TMP/out")"
grep -q "load" "$CALLS" && fail "première vérification : image rechargée sans référence"

echo 6 >"$COUNT"
check || fail "deux redémarrages : $(cat "$TMP/out")"
grep -q "load" "$CALLS" && fail "deux redémarrages : image rechargée"

echo 9 >"$COUNT"
check || fail "boucle : $(cat "$TMP/out")"
for call in "rm -f pi-appliance-traefik" "rmi -f traefik:v3.7.19" "load -i $ARCHIVE" "compose -f $COMPOSE up -d --remove-orphans"; do
	grep -qx -- "$call" "$CALLS" || fail "boucle : appel manquant « $call » dans $(cat "$CALLS")"
done
[ -s "$REPAIR_FILE" ] || fail "boucle : date de la réparation non gardée"

echo 2 >"$COUNT"
check || fail "après réparation : $(cat "$TMP/out")"
echo 8 >"$COUNT"
if check; then
	fail "boucle de nouveau dans l'heure : pas d'erreur"
fi
grep -q "load" "$CALLS" && fail "boucle de nouveau dans l'heure : image rechargée encore"

touch -d "2 hours ago" "$REPAIR_FILE"
echo 2 >"$COUNT"
check || true
echo 8 >"$COUNT"
check || fail "boucle deux heures plus tard : $(cat "$TMP/out")"
grep -q "load" "$CALLS" || fail "boucle deux heures plus tard : image pas rechargée"

rm -f "$ARCHIVE"
echo 2 >"$COUNT"
check || true
echo 8 >"$COUNT"
touch -d "2 hours ago" "$REPAIR_FILE"
if check; then
	fail "archive absente : pas d'erreur"
fi
grep -q "rmi" "$CALLS" && fail "archive absente : image supprimée sans pouvoir la recharger"

if [ "$failures" -gt 0 ]; then
	echo "$failures échec(s)"
	exit 1
fi
echo "traefik.sh check : tous les tests passent"
