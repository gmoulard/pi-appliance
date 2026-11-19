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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

const adminPassword = "une phrase de passe"

func init() {
	auth.Iterations = 1000
}

type okRunner struct{}

func (okRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

type fixture struct {
	handler  http.Handler
	accounts *auth.Store
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	return fixture{handler: Handler("1.2.3", catalog, manager, accounts), accounts: accounts}
}

// admin crée le compte administrateur et renvoie le cookie de sa session.
func (f fixture) admin(t *testing.T) *http.Cookie {
	t.Helper()
	response := f.do(http.MethodPost, "/pi-appliance/api/setup", "application/json", `{"name": "marie", "email": "marie@example.org", "password": "`+adminPassword+`"}`, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("création de l'administrateur : code %d : %s", response.Code, response.Body.String())
	}
	return sessionCookie(t, response)
}

func sessionCookie(t *testing.T, response *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == cookieName && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("pas de cookie de session : %v", response.Header())
	return nil
}

func (f fixture) do(method, path, contentType, body string, cookie *http.Cookie, headers ...string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.RemoteAddr = "192.168.1.20:5000"
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return newFixture(t).do(http.MethodGet, path, "", "", nil)
}

func (f fixture) install(body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, "/pi-appliance/api/services", "application/json", body, cookie)
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatalf("%v : %s", err, response.Body.String())
	}
	return value
}

func TestHealth(t *testing.T) {
	response := get(t, "/pi-appliance/api/health")
	if response.Code != http.StatusOK {
		t.Fatalf("code %d", response.Code)
	}
	if health := decode[Health](t, response); health != (Health{Status: "ok", Version: "1.2.3"}) {
		t.Fatalf("réponse %+v", health)
	}
}

func TestConsolePage(t *testing.T) {
	response := get(t, "/pi-appliance/")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<img src="logo.png" alt="" width="64" height="64">pi-appliance</h1>`) {
		t.Fatalf("code %d, page %q", response.Code, response.Body.String())
	}
}

func TestLogo(t *testing.T) {
	for _, path := range []string{"/pi-appliance/logo.png", "/pi-appliance/favicon.png"} {
		response := get(t, path)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s : code %d, type %q", path, response.Code, response.Header().Get("Content-Type"))
		}
	}
}

func TestRedirectWithoutSlash(t *testing.T) {
	response := get(t, "/pi-appliance")
	if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != "/pi-appliance/" {
		t.Fatalf("code %d, Location %q", response.Code, response.Header().Get("Location"))
	}
}

func TestOutsideConsole(t *testing.T) {
	if response := get(t, "/"); response.Code != http.StatusNotFound {
		t.Fatalf("code %d", response.Code)
	}
}

func TestSetup(t *testing.T) {
	f := newFixture(t)
	state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", nil))
	if !state.Setup || state.Account != nil {
		t.Fatalf("avant la création : %+v", state)
	}
	short := f.do(http.MethodPost, "/pi-appliance/api/setup", "application/json", `{"name": "marie", "email": "marie@example.org", "password": "8 signes"}`, nil)
	if short.Code != http.StatusBadRequest || !strings.Contains(short.Body.String(), "12 caractères") {
		t.Fatalf("mot de passe court : code %d : %s", short.Code, short.Body.String())
	}
	cookie := f.admin(t)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/pi-appliance" {
		t.Fatalf("cookie %+v", cookie)
	}
	state = decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", cookie))
	if state.Setup || state.Account == nil || state.Account.Name != "marie" || state.Account.Role != auth.RoleAdmin || state.Account.EmailVerified {
		t.Fatalf("après la création : %+v", state)
	}
	again := f.do(http.MethodPost, "/pi-appliance/api/setup", "application/json", `{"name": "pirate", "email": "pirate@example.org", "password": "`+adminPassword+`"}`, nil)
	if again.Code != http.StatusConflict {
		t.Fatalf("second administrateur : code %d", again.Code)
	}
}

func TestLoginLogout(t *testing.T) {
	f := newFixture(t)
	f.admin(t)
	login := func(password string) *httptest.ResponseRecorder {
		return f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+password+`"}`, nil)
	}
	if response := login("mauvais"); response.Code != http.StatusUnauthorized {
		t.Fatalf("mauvais mot de passe : code %d", response.Code)
	}
	response := login(adminPassword)
	if response.Code != http.StatusOK {
		t.Fatalf("connexion : code %d : %s", response.Code, response.Body.String())
	}
	cookie := sessionCookie(t, response)
	if cookie.Secure {
		t.Fatal("cookie Secure sans HTTPS : la console serait inutilisable en HTTP direct")
	}
	if code := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie).Code; code != http.StatusOK {
		t.Fatalf("services avec session : code %d", code)
	}
	if code := f.do(http.MethodPost, "/pi-appliance/api/logout", "", "", cookie).Code; code != http.StatusNoContent {
		t.Fatalf("déconnexion : code %d", code)
	}
	if code := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie).Code; code != http.StatusUnauthorized {
		t.Fatalf("services après déconnexion : code %d", code)
	}
}

func TestLoginLimit(t *testing.T) {
	f := newFixture(t)
	f.admin(t)
	var response *httptest.ResponseRecorder
	for i := 0; i < 6; i++ {
		response = f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "faux"}`, nil)
	}
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" || !strings.Contains(response.Body.String(), "une minute") {
		t.Fatalf("code %d, Retry-After %q : %s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
}

func TestSecureCookieBehindTraefik(t *testing.T) {
	f := newFixture(t)
	f.admin(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+adminPassword+`"}`, nil, "X-Forwarded-Proto", "https")
	if sessionCookie(t, response).Secure {
		t.Fatal("en-tête X-Forwarded-Proto d'un client du réseau pris en compte")
	}
	request := httptest.NewRequest(http.MethodPost, "/pi-appliance/api/login", strings.NewReader(`{"name": "marie", "password": "`+adminPassword+`"}`))
	request.RemoteAddr = "172.18.0.2:4000"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	if !sessionCookie(t, recorder).Secure {
		t.Fatal("cookie non Secure derrière Traefik en HTTPS")
	}
	if ip := clientIP(request); ip != "203.0.113.9" {
		t.Fatalf("adresse du client %q", ip)
	}
}

func TestReset(t *testing.T) {
	f := newFixture(t)
	old := f.admin(t)
	reset := func(password string) *httptest.ResponseRecorder {
		return f.do(http.MethodPost, "/pi-appliance/api/reset", "application/json", `{"password": "`+password+`"}`, nil)
	}
	if code := reset("nouvelle phrase de passe").Code; code != http.StatusConflict {
		t.Fatalf("sans fichier reset : code %d", code)
	}
	file := filepath.Join(t.TempDir(), "reset")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.ConsumeResetFile(file); err != nil {
		t.Fatal(err)
	}
	if state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", nil)); !state.Reset {
		t.Fatalf("réinitialisation non signalée : %+v", state)
	}
	response := reset("nouvelle phrase de passe")
	if response.Code != http.StatusOK {
		t.Fatalf("réinitialisation : code %d : %s", response.Code, response.Body.String())
	}
	sessionCookie(t, response)
	if code := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", old).Code; code != http.StatusUnauthorized {
		t.Fatalf("ancienne session : code %d", code)
	}
}

func TestSignInRequired(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/pi-appliance/api/catalog", "/pi-appliance/api/services"} {
		if code := f.do(http.MethodGet, path, "", "", nil).Code; code != http.StatusUnauthorized {
			t.Fatalf("%s sans session : code %d", path, code)
		}
	}
	if code := f.install(`{"app": "nextcloud"}`, nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("installation sans session : code %d", code)
	}
	if code := f.do(http.MethodDelete, "/pi-appliance/api/services/cloud?data=delete", "", "", nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("désinstallation sans session : code %d", code)
	}
}

func TestCatalog(t *testing.T) {
	f := newFixture(t)
	body := decode[struct {
		Apps []map[string]any `json:"apps"`
	}](t, f.do(http.MethodGet, "/pi-appliance/api/catalog", "", "", f.admin(t)))
	if len(body.Apps) == 0 || body.Apps[0]["id"] != "nextcloud" {
		t.Fatalf("catalogue %v", body.Apps)
	}
	if _, leaked := body.Apps[0]["Compose"]; leaked {
		t.Fatal("fichier Compose exposé dans le catalogue")
	}
}

func TestInstallListRemove(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	response := f.install(`{"app": "nextcloud"}`, cookie)
	if response.Code != http.StatusCreated {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	result := decode[services.Result](t, response)
	if result.Instance.Name != "cloud" || result.Instance.Address != "cloud.maison.example.org" || len(result.Secrets) != 1 {
		t.Fatalf("résultat %+v", result)
	}
	if response := f.install(`{"app": "nextcloud"}`, cookie); response.Code != http.StatusConflict {
		t.Fatalf("doublon : code %d", response.Code)
	}
	list := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie)
	if !strings.Contains(list.Body.String(), `"name":"cloud"`) {
		t.Fatalf("liste %s", list.Body.String())
	}
	remove := func(query string) int {
		return f.do(http.MethodDelete, "/pi-appliance/api/services/cloud"+query, "", "", cookie).Code
	}
	if code := remove(""); code != http.StatusBadRequest {
		t.Fatalf("sans choix sur les données : code %d", code)
	}
	if code := remove("?data=keep"); code != http.StatusNoContent {
		t.Fatalf("désinstallation : code %d", code)
	}
	if code := remove("?data=keep"); code != http.StatusNotFound {
		t.Fatalf("seconde désinstallation : code %d", code)
	}
}

func TestInstallErrors(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	cases := map[string]struct {
		contentType, body string
		code              int
	}{
		"pas du JSON":       {"text/plain", `{"app": "nextcloud"}`, http.StatusUnsupportedMediaType},
		"illisible":         {"application/json", `{"app":`, http.StatusBadRequest},
		"champ inconnu":     {"application/json", `{"app": "nextcloud", "privileged": true}`, http.StatusBadRequest},
		"application":       {"application/json", `{"app": "inconnue"}`, http.StatusNotFound},
		"nom":               {"application/json", `{"app": "nextcloud", "name": "Mon Cloud"}`, http.StatusBadRequest},
		"paramètre inconnu": {"application/json", `{"app": "nextcloud", "parameters": {"X": "1"}}`, http.StatusBadRequest},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			response := f.do(http.MethodPost, "/pi-appliance/api/services", c.contentType, c.body, cookie)
			if response.Code != c.code || !strings.Contains(response.Body.String(), `"error"`) {
				t.Fatalf("code %d, attendu %d : %s", response.Code, c.code, response.Body.String())
			}
		})
	}
}
