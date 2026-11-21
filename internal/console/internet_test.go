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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
	"github.com/pquerna/otp/totp"
)

// internetFixture : console dont les services ont le domaine personnel
// maison.example.org (adresse internet du Pi), avec le réglage de la console
// sur internet et le fichier de Traefik dans un dossier de test.
func internetFixture(t *testing.T) (fixture, string) {
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
	traefik := filepath.Join(dir, "dynamic", "console-internet.yml")
	if err := os.MkdirAll(filepath.Dir(traefik), 0o755); err != nil {
		t.Fatal(err)
	}
	handler := Handler(Config{
		Version:             "1.2.3",
		Catalog:             catalog,
		Services:            manager,
		Accounts:            accounts,
		InternetFile:        filepath.Join(dir, "internet-console.json"),
		TraefikInternetFile: traefik,
	})
	return fixture{handler: handler, accounts: accounts, manager: manager}, traefik
}

// fromOutside : demande d'un visiteur d'internet, relayée par Traefik (boucle locale).
func (f fixture) fromOutside(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.RemoteAddr = "127.0.0.1:40000"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.Header.Set("X-Forwarded-Proto", "https")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// enableTOTP active la double authentification du compte connecté et renvoie son secret.
func enableTOTP(t *testing.T, f fixture, cookie *http.Cookie) string {
	t.Helper()
	setup := decode[auth.TOTPSetup](t, f.do(http.MethodPost, "/pi-appliance/api/account/totp", "", "", cookie))
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if confirm := f.do(http.MethodPost, "/pi-appliance/api/account/totp/confirm", "application/json", `{"code": "`+code+`"}`, cookie); confirm.Code != http.StatusOK {
		t.Fatalf("activation : %d %s", confirm.Code, confirm.Body.String())
	}
	return setup.Secret
}

func TestConsoleClosedOnInternetByDefault(t *testing.T) {
	f, traefik := internetFixture(t)
	cookie := f.admin(t)
	for _, path := range []string{"/pi-appliance/", "/pi-appliance/api/session", "/pi-appliance/api/services"} {
		if response := f.fromOutside(http.MethodGet, path, "", cookie); response.Code != http.StatusNotFound {
			t.Errorf("%s depuis internet, console fermée : code %d", path, response.Code)
		}
	}
	if response := f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "marie", "password": "`+adminPassword+`"}`, nil); response.Code != http.StatusNotFound {
		t.Fatalf("connexion depuis internet, console fermée : code %d", response.Code)
	}
	// api/hello reste lisible ; la maison n'est pas touchée.
	if response := f.fromOutside(http.MethodGet, "/pi-appliance/api/hello", "", nil); response.Code != http.StatusOK {
		t.Fatalf("api/hello : code %d", response.Code)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie); response.Code != http.StatusOK {
		t.Fatalf("maison : code %d", response.Code)
	}
	if _, err := os.Stat(traefik); !os.IsNotExist(err) {
		t.Fatalf("fichier de Traefik écrit console fermée : %v", err)
	}
	// Ouvrir sans double authentification : refusé.
	if response := f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", "", cookie); response.Code != http.StatusConflict {
		t.Fatalf("ouverture sans double authentification : code %d", response.Code)
	}
}

func TestConsoleOpenOnInternet(t *testing.T) {
	f, traefik := internetFixture(t)
	cookie := f.admin(t)
	secret := enableTOTP(t, f, cookie)
	// Un second compte, sans double authentification.
	invitation := f.invite(t, cookie, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	if response := f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+invitation+`", "password": "un autre mot de passe"}`, nil); response.Code != http.StatusOK {
		t.Fatalf("invitation : %d %s", response.Code, response.Body.String())
	}

	// Ouvrir depuis internet : impossible (la console n'y est pas encore).
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", cookie); response.Code != http.StatusNotFound {
		t.Fatalf("ouverture depuis internet : code %d", response.Code)
	}
	status := decode[InternetStatus](t, f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", "", cookie))
	if !status.Enabled || status.URL != "https://maison.example.org/pi-appliance/" || !status.TOTP {
		t.Fatalf("état %+v", status)
	}
	data, err := os.ReadFile(traefik)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Host(`maison.example.org`)", "PathPrefix(`/pi-appliance`)", "!PathPrefix(`/pi-appliance/api/auth/forward`)", "certResolver: letsencrypt", "entryPoints: [websecure]"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("configuration de Traefik sans %q :\n%s", want, data)
		}
	}

	// Depuis internet : compte sans double authentification refusé comme un mauvais mot de passe.
	paul := f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "paul", "password": "un autre mot de passe"}`, nil)
	wrong := f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "paul", "password": "mauvais mot de passe"}`, nil)
	if paul.Code != http.StatusUnauthorized || paul.Body.String() != wrong.Body.String() || len(paul.Result().Cookies()) != 0 {
		t.Fatalf("compte sans double authentification : code %d %s (mauvais mot de passe : %s)", paul.Code, paul.Body.String(), wrong.Body.String())
	}
	// Session de paul ouverte à la maison : sans effet depuis internet.
	home := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "paul", "password": "un autre mot de passe"}`, nil)
	paulCookie := sessionCookie(t, home)
	if response := f.fromOutside(http.MethodGet, "/pi-appliance/api/services", "", paulCookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("session sans double authentification depuis internet : code %d", response.Code)
	}
	if session := decode[SessionState](t, f.fromOutside(http.MethodGet, "/pi-appliance/api/session", "", paulCookie)); session.Account != nil || !session.Internet {
		t.Fatalf("session %+v", session)
	}

	// Administratrice avec double authentification : connexion depuis internet.
	login := f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "marie", "password": "`+adminPassword+`"}`, nil)
	if login.Code != http.StatusUnauthorized || !strings.Contains(login.Body.String(), `"code_needed":true`) {
		t.Fatalf("sans code : %d %s", login.Code, login.Body.String())
	}
	code, err := totp.GenerateCode(secret, time.Now().Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	login = f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "marie", "password": "`+adminPassword+`", "code": "`+code+`"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("connexion avec code : %d %s", login.Code, login.Body.String())
	}
	outside := sessionCookie(t, login)
	if response := f.fromOutside(http.MethodGet, "/pi-appliance/api/services", "", outside); response.Code != http.StatusOK {
		t.Fatalf("console depuis internet : code %d", response.Code)
	}
	// Réservé à la maison, même console ouverte.
	if response := f.fromOutside(http.MethodPost, "/pi-appliance/api/factory-reset", `{}`, outside); response.Code != http.StatusForbidden {
		t.Fatalf("remise à zéro depuis internet : code %d", response.Code)
	}
	// Fermer depuis internet : permis ; ensuite tout est refusé, fichier retiré.
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/internet-console?enabled=false", "", outside); response.Code != http.StatusOK {
		t.Fatalf("fermeture depuis internet : code %d %s", response.Code, response.Body.String())
	}
	if response := f.fromOutside(http.MethodGet, "/pi-appliance/api/services", "", outside); response.Code != http.StatusNotFound {
		t.Fatalf("après fermeture : code %d", response.Code)
	}
	if _, err := os.Stat(traefik); !os.IsNotExist(err) {
		t.Fatalf("fichier de Traefik gardé après fermeture : %v", err)
	}
}

func TestTraefikInternet(t *testing.T) {
	if got := traefikInternet(false, []string{"maison.example.org"}); got != "" {
		t.Fatalf("console fermée : %q", got)
	}
	if got := traefikInternet(true, nil); got != "" {
		t.Fatalf("sans adresse : %q", got)
	}
	got := traefikInternet(true, []string{"controltower.pi-appliance.com", "maison.duckdns.org"})
	if !strings.Contains(got, "(Host(`controltower.pi-appliance.com`) || Host(`maison.duckdns.org`)) && PathPrefix(`/pi-appliance`)") {
		t.Fatalf("règle :\n%s", got)
	}
}

func TestInternetHostsFiltered(t *testing.T) {
	for host, ok := range map[string]bool{
		"maison.example.org": true, "a.duckdns.org": true, "pi.local": false, "localhost": false,
		"x`) || PathPrefix(`/": false, "bad host.org": false, "-a.org": false,
	} {
		if got := hostPattern.MatchString(host) && !strings.HasSuffix(host, ".local"); got != ok {
			t.Errorf("%q : %v", host, got)
		}
	}
}

func TestFromInternet(t *testing.T) {
	for addr, want := range map[string]bool{"192.168.1.20:1": false, "10.13.13.2:1": false, "[fd00::1]:1": false, "[fe80::1]:1": false, "127.0.0.1:1": false, "203.0.113.9:1": true, "[2001:db8::1]:1": true} {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = addr
		if got := fromInternet(request); got != want {
			t.Errorf("%s : %v", addr, got)
		}
	}
	// Un visiteur direct (sans Traefik) ne choisit pas son adresse par X-Forwarded-For.
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.9:1"
	request.Header.Set("X-Forwarded-For", "192.168.1.20")
	if !fromInternet(request) {
		t.Fatal("X-Forwarded-For d'un visiteur direct pris en compte")
	}
}

func TestCLIToken(t *testing.T) {
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Accounts: accounts, CLIToken: "jeton",
		Services: &services.Manager{Dir: dir, Domain: "maison.local", Runner: okRunner{}}}), accounts: accounts}
	f.admin(t)
	try := func(remote, token, forwarded string) int {
		request := httptest.NewRequest(http.MethodGet, "/pi-appliance/api/services", nil)
		request.RemoteAddr = remote
		request.Header.Set("Authorization", "Bearer "+token)
		if forwarded != "" {
			request.Header.Set("X-Forwarded-For", forwarded)
		}
		recorder := httptest.NewRecorder()
		f.handler.ServeHTTP(recorder, request)
		return recorder.Code
	}
	if code := try("127.0.0.1:5000", "jeton", ""); code != http.StatusOK {
		t.Fatalf("jeton en direct : code %d", code)
	}
	for name, code := range map[string]int{
		"mauvais jeton":    try("127.0.0.1:5000", "autre", ""),
		"jeton vide":       try("127.0.0.1:5000", "", ""),
		"par Traefik":      try("127.0.0.1:5000", "jeton", "192.168.1.20"),
		"depuis le réseau": try("192.168.1.20:5000", "jeton", ""),
	} {
		if code != http.StatusUnauthorized {
			t.Errorf("%s : code %d", name, code)
		}
	}
}

func TestConsolePersonalDomain(t *testing.T) {
	f, traefik := internetFixture(t)
	known := map[string]bool{"vdl.moulard.org": true}
	saved := lookupHost
	lookupHost = func(_ context.Context, host string) ([]string, error) {
		if known[host] {
			return []string{"82.66.246.212"}, nil
		}
		return nil, errors.New("pas de réponse")
	}
	t.Cleanup(func() { lookupHost = saved })
	cookie := f.admin(t)
	enableTOTP(t, f, cookie)

	put := func(body string) *httptest.ResponseRecorder {
		return f.do(http.MethodPut, "/pi-appliance/api/internet-console/domain", "application/json", body, cookie)
	}
	for body, code := range map[string]int{
		`{"domain": "maison.local"}`:           http.StatusBadRequest,
		`{"domain": "82.66.246.212"}`:          http.StatusBadRequest,
		`{"domain": "x.pi-appliance.com"}`:     http.StatusBadRequest,
		"{\"domain\": \"a`) || Host(`b.org\"}": http.StatusBadRequest,
		`{"domain": "inconnu.moulard.org"}`:    http.StatusConflict,
		`pas du json`:                          http.StatusBadRequest,
	} {
		if response := put(body); response.Code != code {
			t.Errorf("%s : code %d, attendu %d", body, response.Code, code)
		}
	}
	// Adresse tapée avec https:// et le chemin : ramenée au nom.
	status := decode[InternetStatus](t, put(`{"domain": " HTTPS://VDL.moulard.org/pi-appliance/ "}`))
	if status.Domain != "vdl.moulard.org" || status.Enabled {
		t.Fatalf("état %+v", status)
	}
	if _, err := os.Stat(traefik); !os.IsNotExist(err) {
		t.Fatalf("fichier de Traefik écrit console fermée : %v", err)
	}
	// Ouvrir garde l'adresse, qui passe en premier.
	status = decode[InternetStatus](t, f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", "", cookie))
	if !status.Enabled || status.URL != "https://vdl.moulard.org/pi-appliance/" || status.Domain != "vdl.moulard.org" {
		t.Fatalf("état %+v", status)
	}
	data, err := os.ReadFile(traefik)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "(Host(`vdl.moulard.org`) || Host(`maison.example.org`))") {
		t.Fatalf("configuration de Traefik :\n%s", data)
	}
	// Depuis internet : changer l'adresse est refusé.
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/internet-console/domain", `{"domain": ""}`, cookie); response.Code != http.StatusForbidden && response.Code != http.StatusUnauthorized {
		t.Fatalf("changement depuis internet : code %d", response.Code)
	}
	// Retirer l'adresse : la console reste ouverte à ses autres adresses.
	status = decode[InternetStatus](t, put(`{"domain": ""}`))
	if status.Domain != "" || !status.Enabled || status.URL != "https://maison.example.org/pi-appliance/" {
		t.Fatalf("après retrait : %+v", status)
	}
}

func TestDomainTarget(t *testing.T) {
	c := &console{}
	if got := c.domainTarget(); got != "" {
		t.Fatalf("sans DuckDNS : %q", got)
	}
	updater, err := ddns.Open(filepath.Join(t.TempDir(), "duckdns.json"))
	if err != nil {
		t.Fatal(err)
	}
	c.DuckDNS = updater
	if got := c.domainTarget(); got != "" {
		t.Fatalf("DuckDNS pas réglé : %q", got)
	}
	if err := updater.Save(ddns.Settings{Domain: "monpi", Token: "12345678-1234-1234-1234-123456789abc"}); err != nil {
		t.Fatal(err)
	}
	if got := c.domainTarget(); got != "monpi.duckdns.org" {
		t.Fatalf("cible : %q", got)
	}
}
