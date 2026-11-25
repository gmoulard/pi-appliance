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

# Ouvre la page d'un service comme un navigateur (services-e2e-test.sh) :
# redirections suivies sur la même adresse, puis les feuilles de style,
# scripts et images de la page. Toutes les demandes partent vers l'adresse IP
# de Traefik donnée, avec le nom demandé (Host, SNI) de l'adresse.
#
# services-check.py <ip de Traefik> <adresse https://…/> <attendu> [session]
#   attendu : page (page du service, ses fichiers compris), login (renvoi vers
#   la connexion de la console), refused (page « introuvable » ou refus) ;
#   session : cookie pi_session d'un administrateur connecté à la console.
#   Avec elle, le renvoi vers la connexion est suivi comme dans le navigateur
#   (api/auth/return, code pi_login échangé contre le cookie du service).
# Sortie : une ligne « ok … » ou « ÉCHEC … », code 0 ou 1.

import html.parser
import http.client
import json
import socket
import ssl
import sys
import urllib.parse

IP, START, EXPECTED = sys.argv[1], sys.argv[2], sys.argv[3]
SESSION = sys.argv[4] if len(sys.argv) > 4 else ""
# Cookies reçus, par adresse (le chemin des cookies est ignoré).
JAR = {}
START_URL = urllib.parse.urlsplit(START)
HOST = START_URL.hostname
# Sous l'adresse internet du Pi, le service n'a que son chemin /<nom>/ :
# un renvoi ou un fichier hors de ce chemin ne mène pas au service.
PREFIX = START_URL.path if START_URL.path not in ("", "/") else "/"
CONTEXT = ssl.create_default_context()
CONTEXT.check_hostname = False
CONTEXT.verify_mode = ssl.CERT_NONE
MAX_ASSETS = 25


def fetch(url, method="GET", data=None, cookies=None):
    parts = urllib.parse.urlsplit(url)
    path = parts.path or "/"
    if parts.query:
        path += "?" + parts.query
    conn = http.client.HTTPSConnection(IP, 443, timeout=60, context=CONTEXT)
    # Nom du service dans la poignée de main TLS, comme un navigateur.
    conn.sock = CONTEXT.wrap_socket(socket.create_connection((IP, 443), timeout=60), server_hostname=parts.hostname)
    headers = {"Host": parts.netloc, "User-Agent": "Mozilla/5.0 pi-appliance-test", "Accept": "text/html,*/*"}
    jar = dict(JAR.get(parts.hostname, {}))
    jar.update(cookies or {})
    if jar:
        headers["Cookie"] = "; ".join(f"{k}={v}" for k, v in jar.items())
    if data is not None:
        headers["Content-Type"] = "application/json"
        headers["X-Pi-Appliance"] = "1"
        data = json.dumps(data).encode()
    conn.request(method, path, body=data, headers=headers)
    response = conn.getresponse()
    body = response.read(4 << 20)
    for cookie in response.msg.get_all("Set-Cookie") or []:
        pair = cookie.split(";", 1)[0]
        if "=" in pair:
            key, value = pair.split("=", 1)
            JAR.setdefault(parts.hostname, {})[key.strip()] = value.strip()
    conn.close()
    return response.status, response.getheader("Location") or "", body


def console_return(login_url):
    """Comme la page de connexion de la console, déjà connectée : l'adresse
    du service, avec son code pi_login (POST api/auth/return)."""
    parts = urllib.parse.urlsplit(login_url)
    query = urllib.parse.parse_qs(parts.query)
    api = f"https://{parts.netloc}/pi-appliance/api/auth/return"
    status, _, body = fetch(api, "POST", {"return": query.get("return", [""])[0], "state": query.get("state", [""])[0]}, {"pi_session": SESSION})
    if status != 200:
        fail(f"api/auth/return : HTTP {status} {body[:200]!r}")
    return json.loads(body)["url"]


class Assets(html.parser.HTMLParser):
    def __init__(self):
        super().__init__()
        self.found = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "script" and attrs.get("src"):
            self.found.append(attrs["src"])
        elif tag == "link" and attrs.get("href") and "stylesheet" in (attrs.get("rel") or ""):
            self.found.append(attrs["href"])
        elif tag == "img" and attrs.get("src") and not attrs["src"].startswith("data:"):
            self.found.append(attrs["src"])


def fail(message):
    print(f"ÉCHEC {START} : {message}")
    sys.exit(1)


def console_page(location, body):
    return "/pi-appliance/" in location or b"/pi-appliance/erreur/" in body


url, status, location, body = START, 0, "", b""
seen = []
for _ in range(10):
    try:
        status, location, body = fetch(url)
    except OSError as error:
        fail(f"pas de réponse ({error})")
    seen.append(f"{status} {urllib.parse.urlsplit(url).path}")
    if status not in (301, 302, 303, 307, 308):
        break
    target = urllib.parse.urljoin(url, location)
    target_parts = urllib.parse.urlsplit(target)
    to_console = target_parts.path.startswith("/pi-appliance/") and not PREFIX.startswith("/pi-appliance/")
    if to_console and "return=" in target_parts.query:
        if EXPECTED == "login":
            print(f"ok   {START} : renvoi vers la connexion de la console")
            sys.exit(0)
        if SESSION and EXPECTED == "page":
            seen.append("connexion")
            url = console_return(target)
            continue
    if to_console:
        fail(f"renvoi vers la console au lieu du service : {target} ({' → '.join(seen)})")
    if target_parts.hostname != HOST:
        fail(f"renvoi vers une autre adresse : {target} ({' → '.join(seen)})")
    if not target_parts.path.startswith(PREFIX):
        fail(f"renvoi hors de {PREFIX} : {target_parts.path} ({' → '.join(seen)})")
    url = target
else:
    fail(f"trop de renvois ({' → '.join(seen)})")

trail = " → ".join(seen)
if EXPECTED == "refused":
    if status in (401, 403, 404) or console_page(location, body):
        print(f"ok   {START} : refusé ({trail})")
        sys.exit(0)
    fail(f"ouvert alors qu'il devrait être refusé ({trail})")
if EXPECTED == "login":
    fail(f"pas de renvoi vers la connexion de la console ({trail})")
if b"/pi-appliance/erreur/" in body:
    fail(f"page d'erreur de la console au lieu du service ({trail}) : {body[:200]!r}")
# 401 : la connexion de la console n'a pas ouvert le service (Etherpad sans
# l'en-tête Authorization de son compte interne, par exemple).
if not 200 <= status < 300:
    fail(f"HTTP {status} ({trail}) : {body[:300]!r}")

parser = Assets()
try:
    parser.feed(body.decode("utf-8", "replace"))
except Exception as error:
    fail(f"page illisible ({error})")
broken = []
checked = 0
for ref in dict.fromkeys(parser.found):
    if checked >= MAX_ASSETS:
        break
    asset = urllib.parse.urljoin(url, ref)
    parts = urllib.parse.urlsplit(asset)
    if parts.scheme != "https" or parts.hostname != HOST:
        continue
    checked += 1
    if not parts.path.startswith(PREFIX):
        broken.append(f"{parts.path} (hors de {PREFIX})")
        continue
    try:
        asset_status, _, asset_body = fetch(asset)
    except OSError as error:
        broken.append(f"{parts.path} (pas de réponse : {error})")
        continue
    if asset_status >= 400 or b"/pi-appliance/erreur/" in asset_body:
        broken.append(f"{parts.path} (HTTP {asset_status})")
if broken:
    fail(f"{len(broken)} fichier(s) de la page introuvable(s) sur {checked} : " + ", ".join(broken[:8]))
print(f"ok   {START} : HTTP {status}, {checked} fichier(s) de la page ({trail})")
