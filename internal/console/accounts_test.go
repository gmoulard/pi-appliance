// This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
// Copyright (c) 2026 Guillaume Moulard.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package console

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/pquerna/otp/totp"
)

// invite crée un compte par l'administrateur et renvoie le jeton de son lien.
func (f fixture) invite(t *testing.T, admin *http.Cookie, body string) string {
	t.Helper()
	response := f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/accounts", "application/json", body, admin)
	if response.Code != http.StatusCreated {
		t.Fatalf("création du compte : code %d : %s", response.Code, response.Body.String())
	}
	reply := decode[InvitationReply](t, response)
	prefix := "http://maison.example.org/pi-appliance/#invite="
	if reply.Sent || !strings.HasPrefix(reply.Link, prefix) {
		t.Fatalf("lien d'invitation : %+v", reply)
	}
	return strings.TrimPrefix(reply.Link, prefix)
}

func TestAccountsAdminOnly(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	check := f.do(http.MethodPost, "/pi-appliance/api/invitation/check", "application/json", `{"token": "`+token+`"}`, nil)
	if check.Code != http.StatusOK || decode[auth.Public](t, check).Name != "paul" {
		t.Fatalf("lecture de l'invitation : %d %s", check.Code, check.Body.String())
	}
	accept := f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil)
	if accept.Code != http.StatusOK {
		t.Fatalf("invitation acceptée : %d %s", accept.Code, accept.Body.String())
	}
	paul := sessionCookie(t, accept)
	for _, request := range [][2]string{
		{http.MethodGet, "/pi-appliance/api/accounts"},
		{http.MethodPost, "/pi-appliance/api/accounts"},
		{http.MethodDelete, "/pi-appliance/api/accounts/marie"},
		{http.MethodDelete, "/pi-appliance/api/accounts/marie/totp"},
	} {
		if response := f.do(request[0], request[1], "application/json", `{}`, paul); response.Code != http.StatusForbidden {
			t.Errorf("%s %s par un utilisateur : code %d", request[0], request[1], response.Code)
		}
	}
	list := decode[struct{ Accounts []auth.Public }](t, f.do(http.MethodGet, "/pi-appliance/api/accounts", "", "", admin))
	if len(list.Accounts) != 2 || strings.Contains(f.do(http.MethodGet, "/pi-appliance/api/accounts", "", "", admin).Body.String(), "hash") {
		t.Fatalf("comptes : %+v", list)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/accounts/marie", "", "", admin); response.Code != http.StatusConflict {
		t.Fatalf("supprimer son propre compte : %d", response.Code)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/accounts/paul", "", "", admin); response.Code != http.StatusNoContent {
		t.Fatalf("supprimer paul : %d %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", paul); response.Code != http.StatusUnauthorized {
		t.Fatalf("compte supprimé : %d", response.Code)
	}
}

func TestGuestNeedsEndDate(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/accounts", "application/json", `{"name": "invite", "email": "i@example.org", "role": "guest"}`, admin); response.Code != http.StatusBadRequest {
		t.Fatalf("invité sans date de fin : %d %s", response.Code, response.Body.String())
	}
	end := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	f.invite(t, admin, `{"name": "invite", "email": "i@example.org", "role": "guest", "expires": "`+end+`"}`)
}

func TestLoginWithCode(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	setup := decode[auth.TOTPSetup](t, f.do(http.MethodPost, "/pi-appliance/api/account/totp", "", "", admin))
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	confirm := f.do(http.MethodPost, "/pi-appliance/api/account/totp/confirm", "application/json", `{"code": "`+code+`"}`, admin)
	if confirm.Code != http.StatusOK {
		t.Fatalf("activation : %d %s", confirm.Code, confirm.Body.String())
	}
	codes := decode[struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}](t, confirm).RecoveryCodes
	login := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+adminPassword+`"}`, nil)
	if login.Code != http.StatusUnauthorized || !strings.Contains(login.Body.String(), `"code_needed":true`) {
		t.Fatalf("connexion sans code : %d %s", login.Code, login.Body.String())
	}
	login = f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+adminPassword+`", "code": "`+codes[0]+`"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("connexion avec un code de secours : %d %s", login.Code, login.Body.String())
	}
}

func TestRequiredTOTPBlocksConsole(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user", "totp_required": true}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", paul); response.Code != http.StatusForbidden {
		t.Fatalf("double authentification exigée, pas activée : %d", response.Code)
	}
	if response := f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/auth/return", "application/json", `{"return": "http://cloud.maison.example.org/"}`, paul); response.Code != http.StatusForbidden {
		t.Fatalf("pas de service avant l'activation : %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/account/totp", "", "", paul); response.Code != http.StatusOK {
		t.Fatalf("activation permise : %d %s", response.Code, response.Body.String())
	}
}

func forwardRequest(f fixture, remote string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	return forwardTo(f, "/pi-appliance/api/auth/forward", remote, cookie, headers)
}

func forwardTo(f fixture, path, remote string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = remote
	if cookie != nil {
		request.AddCookie(cookie)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// installed déclare un service installé (instance.json seulement).
func installed(t *testing.T, f fixture, name, address, access string) {
	t.Helper()
	dir := filepath.Join(f.manager.Dir, "services", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(services.Instance{Name: name, App: "site-web", Address: address, Access: access})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// returnFrom : POST api/auth/return derrière Traefik en HTTPS, sur l'adresse host de la console.
func returnFrom(f fixture, host, body string, session *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://"+host+"/pi-appliance/api/auth/return", strings.NewReader(body))
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "127.0.0.1:5000"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(session)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// loginRedirect : la redirection d'un service sans session vers la console,
// et le cookie pi_login_state qu'elle pose.
func loginRedirect(t *testing.T, f fixture, path string, headers map[string]string) (*url.URL, *http.Cookie) {
	t.Helper()
	response := forwardTo(f, path, "127.0.0.1:5000", nil, headers)
	location, err := url.Parse(response.Header().Get("Location"))
	state := cookieNamed(response, stateCookie)
	if response.Code != http.StatusFound || err != nil || state == nil || state.Domain != "" || !state.HttpOnly || !state.Secure || location.Query().Get("state") != state.Value {
		t.Fatalf("sans session : %d %v %+v", response.Code, response.Header(), state)
	}
	return location, state
}

// forwardCookieFor : cookie délégué d'une personne connectée à la console, par
// tout le parcours : service sans session, console, retour au service avec le code.
func forwardCookieFor(t *testing.T, f fixture, session *http.Cookie, path string, headers map[string]string) *http.Cookie {
	t.Helper()
	location, state := loginRedirect(t, f, path, headers)
	body, _ := json.Marshal(ForwardRequest{Return: location.Query().Get("return"), State: state.Value})
	back := returnFrom(f, location.Host, string(body), session)
	target, err := url.Parse(decode[map[string]string](t, back)["url"])
	if back.Code != http.StatusOK || err != nil || target.Query().Get(loginParam) == "" {
		t.Fatalf("retour au service : %d %s", back.Code, back.Body.String())
	}
	callback := maps.Clone(headers)
	callback["X-Forwarded-Uri"] = target.RequestURI()
	response := forwardTo(f, path, "127.0.0.1:5000", state, callback)
	forward := cookieNamed(response, forwardCookie)
	if response.Code != http.StatusFound || response.Header().Get("Location") != location.Query().Get("return") || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("échange du code : %d %v", response.Code, response.Header())
	}
	if forward == nil || forward.Domain != "" || forward.Path != "/" || !forward.HttpOnly || !forward.Secure || forward.SameSite != http.SameSiteLaxMode || forward.Value == session.Value {
		t.Fatalf("cookie délégué : %+v", forward)
	}
	return forward
}

// Adresses de la maison à un seul niveau (TODO-2.61) : www-maison.local et la
// console maison.local sont voisins, chaque service a son propre cookie.
func TestForwardAuth(t *testing.T) {
	f := newFixture(t)
	f.manager.Domain = "maison.local"
	admin := f.admin(t)
	installed(t, f, "www", "www-maison.local", services.AccessUsers)
	installed(t, f, "wiki", "wiki-maison.local", services.AccessUsers)
	installed(t, f, "site", "site-maison.local", services.AccessPublic)
	service := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "www-maison.local", "X-Forwarded-Uri": "/apps/files?dir=/", "X-Forwarded-Method": "GET"}

	if response := forwardRequest(f, "192.168.1.20:5000", nil, service); response.Code != http.StatusForbidden {
		t.Fatalf("appel hors de Traefik : %d", response.Code)
	}
	location, state := loginRedirect(t, f, "/pi-appliance/api/auth/forward", service)
	if location.Host != "maison.local" || location.Path != "/pi-appliance/" || location.Query().Get("return") != "https://www-maison.local/apps/files?dir=/" {
		t.Fatalf("connexion : %s", location)
	}
	post := map[string]string{"X-Forwarded-Host": "www-maison.local", "X-Forwarded-Method": "POST"}
	if response := forwardRequest(f, "172.18.0.2:5000", nil, post); response.Code != http.StatusUnauthorized {
		t.Fatalf("POST sans session : %d", response.Code)
	}
	if response := forwardRequest(f, "127.0.0.1:5000", &http.Cookie{Name: forwardCookie, Value: admin.Value}, service); response.Code != http.StatusFound {
		t.Fatalf("jeton de la console comme cookie délégué : %d", response.Code)
	}

	request := func(target string) string {
		body, _ := json.Marshal(ForwardRequest{Return: target, State: state.Value})
		return string(body)
	}
	for _, refused := range []string{"https://evil.example/", "https://www-maison.local.evil.example/", "https://maison.evil/", "http://www-maison.local/", "https://autre-maison.local/"} {
		if back := returnFrom(f, "maison.local", request(refused), admin); back.Code != http.StatusBadRequest {
			t.Errorf("retour vers %s : %d %s", refused, back.Code, back.Body.String())
		}
	}
	if back := returnFrom(f, "maison.local", request("https://site-maison.local/"), admin); decode[map[string]string](t, back)["url"] != "https://site-maison.local/" {
		t.Fatalf("service public : pas de code : %s", back.Body.String())
	}

	back := returnFrom(f, "maison.local", request("https://www-maison.local/apps/files?dir=/"), admin)
	target, _ := url.Parse(decode[map[string]string](t, back)["url"])
	code := target.Query().Get(loginParam)
	if back.Code != http.StatusOK || target.Host != "www-maison.local" || target.Query().Get("dir") != "/" || code == "" {
		t.Fatalf("retour au service : %d %s", back.Code, back.Body.String())
	}
	callback := maps.Clone(service)
	callback["X-Forwarded-Uri"] = target.RequestURI()
	if response := forwardRequest(f, "127.0.0.1:5000", &http.Cookie{Name: stateCookie, Value: "autre-navigateur"}, callback); response.Code != http.StatusForbidden || cookieNamed(response, forwardCookie) != nil || !strings.Contains(response.Body.String(), "plus valable") {
		t.Fatalf("code venu d'un autre navigateur : %d %v", response.Code, response.Header())
	}
	if response := forwardRequest(f, "127.0.0.1:5000", state, callback); response.Code != http.StatusForbidden {
		t.Fatalf("code déjà essayé, accepté : %d", response.Code)
	}

	forward := forwardCookieFor(t, f, admin, "/pi-appliance/api/auth/forward", service)
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", &http.Cookie{Name: cookieName, Value: forward.Value}); response.Code != http.StatusUnauthorized {
		t.Fatalf("le cookie délégué ouvre la console : %d", response.Code)
	}
	response := forwardRequest(f, "127.0.0.1:5000", forward, service)
	if response.Code != http.StatusOK || response.Header().Get("Remote-User") != "marie" || response.Header().Get("Remote-Email") != "marie@example.org" || response.Header().Get("Remote-Groups") != "admin" {
		t.Fatalf("avec session déléguée : %d %v", response.Code, response.Header())
	}
	wiki := maps.Clone(service)
	wiki["X-Forwarded-Host"] = "wiki-maison.local"
	if response := forwardRequest(f, "127.0.0.1:5000", forward, wiki); response.Code != http.StatusFound {
		t.Fatalf("cookie d'un service sur un autre : %d", response.Code)
	}
	old := &http.Cookie{Name: forwardCookie, Value: "ancien-cookie-de-maison.local"}
	both := httptest.NewRequest(http.MethodGet, "/pi-appliance/api/auth/forward", nil)
	both.Header.Set("X-Pi-Appliance", "1")
	both.RemoteAddr = "127.0.0.1:5000"
	both.AddCookie(old)
	both.AddCookie(forward)
	for key, value := range service {
		both.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, both)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ancien cookie avant le bon : %d", recorder.Code)
	}

	logout := f.do(http.MethodPost, "http://maison.local/pi-appliance/api/logout", "", "", admin)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("déconnexion : %d", logout.Code)
	}
	if response := forwardRequest(f, "127.0.0.1:5000", forward, service); response.Code != http.StatusFound {
		t.Fatalf("après déconnexion de la console : %d", response.Code)
	}
}

// Un vrai nom de domaine garde les sous-domaines : cloud.maison.example.org.
func TestForwardAuthDomain(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	installed(t, f, "cloud", "cloud.maison.example.org", services.AccessUsers)
	service := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "cloud.maison.example.org", "X-Forwarded-Uri": "/", "X-Forwarded-Method": "GET"}
	location, _ := loginRedirect(t, f, "/pi-appliance/api/auth/forward", service)
	if location.Host != "maison.example.org" {
		t.Fatalf("connexion : %s", location)
	}
	forward := forwardCookieFor(t, f, admin, "/pi-appliance/api/auth/forward", service)
	if response := forwardRequest(f, "127.0.0.1:5000", forward, service); response.Code != http.StatusOK {
		t.Fatalf("avec session déléguée : %d", response.Code)
	}
}

// Administration du système (Cockpit) : réservée aux administrateurs, connexion
// sur l'adresse même de la console.
func TestForwardAdmin(t *testing.T) {
	f := newFixture(t)
	f.manager.Domain = "maison.local"
	admin := f.admin(t)
	cockpit := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "maison.local", "X-Forwarded-Uri": "/cockpit/system", "X-Forwarded-Method": "GET"}
	const path = "/pi-appliance/api/auth/forward-admin"

	if response := forwardTo(f, path, "192.168.1.20:5000", nil, cockpit); response.Code != http.StatusForbidden {
		t.Fatalf("appel hors de Traefik : %d", response.Code)
	}
	location, _ := loginRedirect(t, f, path, cockpit)
	if location.Host != "maison.local" || location.Query().Get("return") != "https://maison.local/cockpit/system" {
		t.Fatalf("sans session : %s", location)
	}
	if response := forwardTo(f, path, "127.0.0.1:5000", forwardCookieFor(t, f, admin, path, cockpit), cockpit); response.Code != http.StatusOK || response.Header().Get("Remote-User") != "marie" {
		t.Fatalf("administrateur : %d %v", response.Code, response.Header())
	}

	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	cookie := forwardCookieFor(t, f, paul, "/pi-appliance/api/auth/forward", cockpit)
	if response := forwardTo(f, path, "127.0.0.1:5000", cookie, cockpit); response.Code != http.StatusForbidden || response.Header().Get("Remote-User") != "" {
		t.Fatalf("utilisateur : %d %v", response.Code, response.Header())
	}
	if response := forwardRequest(f, "127.0.0.1:5000", cookie, cockpit); response.Code != http.StatusOK {
		t.Fatalf("utilisateur, service ordinaire : %d", response.Code)
	}
}

// Service réservé aux administrateurs (accès « admins », Portainer) : à sa
// propre adresse, connexion sur l'adresse de la console.
func TestForwardServiceAdmin(t *testing.T) {
	f := newFixture(t)
	f.manager.Domain = "maison.local"
	admin := f.admin(t)
	installed(t, f, "portainer", "portainer-maison.local", services.AccessAdmins)
	portainer := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "portainer-maison.local", "X-Forwarded-Uri": "/", "X-Forwarded-Method": "GET"}
	const path = "/pi-appliance/api/auth/forward-service-admin"

	if response := forwardTo(f, path, "192.168.1.20:5000", nil, portainer); response.Code != http.StatusForbidden {
		t.Fatalf("appel hors de Traefik : %d", response.Code)
	}
	if location, _ := loginRedirect(t, f, path, portainer); location.Host != "maison.local" || location.Query().Get("return") != "https://portainer-maison.local/" {
		t.Fatalf("sans session : %s", location)
	}
	if response := forwardTo(f, path, "127.0.0.1:5000", forwardCookieFor(t, f, admin, path, portainer), portainer); response.Code != http.StatusOK || response.Header().Get("Remote-User") != "marie" {
		t.Fatalf("administrateur : %d %v", response.Code, response.Header())
	}
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	if response := forwardTo(f, path, "127.0.0.1:5000", forwardCookieFor(t, f, paul, "/pi-appliance/api/auth/forward", portainer), portainer); response.Code != http.StatusForbidden || response.Header().Get("Remote-User") != "" {
		t.Fatalf("utilisateur : %d %v", response.Code, response.Header())
	}
}

func TestReturnURL(t *testing.T) {
	hosts := map[string]bool{"www-maison.local": true, "site-maison.local": false}
	for _, test := range []struct {
		raw, host string
		ok        bool
		protected bool
	}{
		{"https://cloud.maison.example.org/x", "maison.example.org", true, false},
		{"https://maison.example.org/pi-appliance/", "maison.example.org", true, true},
		{"https://CLOUD.Maison.example.org./", "maison.example.org", true, false},
		{"https://www-maison.local/x", "maison.local", true, true},
		{"https://WWW-maison.local./x", "maison.local", true, true},
		{"https://site-maison.local/", "maison.local", true, false},
		{"https://www-maison.local/x", "192.168.1.20", true, true},
		{"https://autre-maison.local/", "maison.local", false, false},
		{"https://evil.example/", "maison.example.org", false, false},
		{"https://maison.example.org.evil.example/", "maison.example.org", false, false},
		{"https://evilmaison.example.org/", "maison.example.org", false, false},
		{"https://marie@cloud.maison.example.org/", "maison.example.org", false, false},
		{"http://cloud.maison.example.org/", "maison.example.org", false, false},
		{"//cloud.maison.example.org/", "maison.example.org", false, false},
		{"javascript:alert(1)", "maison.example.org", false, false},
		{"https://1.168.192.0/", "192.168.1.20", false, false},
		{"https://192.168.1.20/x", "192.168.1.20", true, true},
	} {
		if _, protected, ok := returnURL(test.raw, test.host, true, hosts); ok != test.ok || protected != test.protected {
			t.Errorf("returnURL(%q, %q) = %v, %v", test.raw, test.host, ok, protected)
		}
	}
	if parentHost("cloud.maison.example.org") != "maison.example.org" || parentHost("example.org") != "example.org" || parentHost("192.168.1.20") != "192.168.1.20" {
		t.Error("adresse parente")
	}
	if value, rest, found := takeParam("a=1&pi_login=x&b=2", loginParam); value != "x" || rest != "a=1&b=2" || !found {
		t.Errorf("takeParam : %q %q %v", value, rest, found)
	}
}
