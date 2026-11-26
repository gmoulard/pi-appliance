// This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
// Copyright (c) 2026 Guillaume Moulard.
//
// Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
// 4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
// of the LICENSE file at the root of the project: this file may be shared and
// integrated into another project unmodified, keeping this notice and a link to
// the project; modified versions may only be published to propose them to the
// official repository. Commercial use requires a separate licence from the
// author: guillaume@moulard.org.
//
// See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.

package console

import (
	"context"
	"errors"
	"github.com/gmoulard/pi-appliance/internal/mail"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/auth"
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
	smtp, err := mail.OpenSMTP(filepath.Join(dir, "smtp.json"))
	if err != nil {
		t.Fatal(err)
	}
	handler := Handler(Config{
		Version:             "1.2.3",
		Catalog:             catalog,
		Services:            manager,
		Accounts:            accounts,
		SMTP:                smtp,
		InternetFile:        filepath.Join(dir, "internet-console.json"),
		TraefikInternetFile: traefik,
	})
	return fixture{handler: handler, accounts: accounts, manager: manager}, traefik
}

// fromOutside : demande d'un visiteur d'internet, relayée par Traefik (boucle locale).
func (f fixture) fromOutside(method, path, body string, cookie *http.Cookie, headers ...string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "127.0.0.1:40000"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.Header.Set("X-Forwarded-Proto", "https")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
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
	// Ouvrir sans double authentification : permis (décision du 2026-11-22).
	if response := f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", "", cookie); response.Code != http.StatusOK {
		t.Fatalf("ouverture sans double authentification : code %d %s", response.Code, response.Body.String())
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

	// Depuis internet : un compte sans double authentification se connecte
	// avec son seul mot de passe (décision du 2026-11-22) ; un mauvais mot de passe reste refusé.
	wrong := f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "paul", "password": "mauvais mot de passe"}`, nil)
	if wrong.Code != http.StatusUnauthorized {
		t.Fatalf("mauvais mot de passe : code %d", wrong.Code)
	}
	paul := f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "paul", "password": "un autre mot de passe"}`, nil)
	if paul.Code != http.StatusOK {
		t.Fatalf("compte sans double authentification : code %d %s", paul.Code, paul.Body.String())
	}
	paulCookie := sessionCookie(t, paul)
	if response := f.fromOutside(http.MethodGet, "/pi-appliance/api/services", "", paulCookie); response.Code != http.StatusOK {
		t.Fatalf("session sans double authentification depuis internet : code %d", response.Code)
	}
	if session := decode[SessionState](t, f.fromOutside(http.MethodGet, "/pi-appliance/api/session", "", paulCookie)); session.Account == nil || !session.Internet {
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
	// Réservé à la maison : les actions faites sans compte.
	if response := f.fromOutside(http.MethodPost, "/pi-appliance/api/setup", `{}`, nil); response.Code != http.StatusForbidden {
		t.Fatalf("mise en route depuis internet : code %d", response.Code)
	}
	if response := f.fromOutside(http.MethodGet, "/pi-appliance/api/setup/restore", "", nil); response.Code != http.StatusForbidden {
		t.Fatalf("restauration de l'appliance depuis internet : code %d", response.Code)
	}
	// Le reste se fait aussi depuis internet, confirmé : remise à zéro avec un
	// mauvais mot de passe refusée, ports de la box sans confirmation refusés.
	if response := f.fromOutside(http.MethodPost, "/pi-appliance/api/factory-reset", `{"confirm": true, "password": "mauvais"}`, outside); response.Code != http.StatusUnauthorized {
		t.Fatalf("remise à zéro depuis internet, mauvais mot de passe : code %d %s", response.Code, response.Body.String())
	}
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/internet-access", `{"enabled": true}`, outside); response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"confirm_needed":true`) {
		t.Fatalf("ports de la box sans confirmation : %d %s", response.Code, response.Body.String())
	}
	// Paul, sans double authentification : refusé même avec son mot de passe.
	if response := f.fromOutside(http.MethodPost, "/pi-appliance/api/factory-reset", `{"confirm": true, "password": "un autre mot de passe"}`, paulCookie); response.Code != http.StatusForbidden {
		t.Fatalf("remise à zéro par un utilisateur : code %d", response.Code)
	}
	// Envoi des e-mails depuis internet : mot de passe et code de double
	// authentification demandés (le serveur d'envoi reçoit les codes).
	settings := `"host": "smtp.gmail.com", "port": 587, "security": "starttls", "username": "pi@gmail.com", "password": "abcd efgh ijkl mnop", "from": "pi@gmail.com"`
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/mail", `{`+settings+`}`, outside); response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"confirm_needed":true`) {
		t.Fatalf("réglage sans confirmation : %d %s", response.Code, response.Body.String())
	}
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/mail", `{`+settings+`, "account_password": "mauvais"}`, outside); response.Code != http.StatusUnauthorized {
		t.Fatalf("réglage avec un mauvais mot de passe : %d %s", response.Code, response.Body.String())
	}
	// Le code de la connexion a déjà servi : on avance l'horloge des comptes.
	later := time.Now().Add(2 * time.Minute)
	clock := f.accounts.Now
	f.accounts.Now = func() time.Time { return later }
	mailCode, err := totp.GenerateCode(secret, later)
	if err != nil {
		t.Fatal(err)
	}
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/mail", `{`+settings+`, "account_password": "`+adminPassword+`", "code": "`+mailCode+`"}`, outside); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"configured":true`) {
		t.Fatalf("réglage confirmé depuis internet : %d %s", response.Code, response.Body.String())
	}
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/mail", `{`+settings+`, "account_password": "`+adminPassword+`", "code": "`+mailCode+`"}`, outside); response.Code != http.StatusUnauthorized {
		t.Fatalf("code déjà servi : %d %s", response.Code, response.Body.String())
	}
	if response := f.fromOutside(http.MethodDelete, "/pi-appliance/api/mail", "", outside); response.Code != http.StatusUnauthorized {
		t.Fatalf("effacement sans confirmation : %d %s", response.Code, response.Body.String())
	}
	// Confirmation par les en-têtes (fenêtre commune de la console).
	later = later.Add(time.Minute)
	headerCode, err := totp.GenerateCode(secret, later)
	if err != nil {
		t.Fatal(err)
	}
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/internet-console/domain", `{"domain": ""}`, outside, "X-Pi-Confirm-Password", adminPassword, "X-Pi-Confirm-Code", headerCode); response.Code == http.StatusUnauthorized || response.Code == http.StatusForbidden {
		t.Fatalf("adresse personnelle confirmée par les en-têtes : %d %s", response.Code, response.Body.String())
	}
	f.accounts.Now = clock
	// Administrateur sans double authentification : l'envoi des e-mails se règle
	// depuis internet avec son mot de passe (choix du 2026-11-25) ; les autres
	// actions sensibles exigent toujours la double authentification.
	jeanInvite := f.invite(t, cookie, `{"name": "jean", "email": "jean@example.org", "role": "admin"}`)
	if response := f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+jeanInvite+`", "password": "le mot de passe de jean"}`, nil); response.Code != http.StatusOK {
		t.Fatalf("invitation de jean : %d %s", response.Code, response.Body.String())
	}
	jeanLogin := f.fromOutside(http.MethodPost, "/pi-appliance/api/login", `{"name": "jean", "password": "le mot de passe de jean"}`, nil)
	if jeanLogin.Code != http.StatusOK {
		t.Fatalf("connexion de jean : %d %s", jeanLogin.Code, jeanLogin.Body.String())
	}
	jean := sessionCookie(t, jeanLogin)
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/mail", `{`+settings+`}`, jean); response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"confirm_needed":true`) {
		t.Fatalf("e-mail sans mot de passe : %d %s", response.Code, response.Body.String())
	}
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/mail", `{`+settings+`, "account_password": "le mot de passe de jean"}`, jean); response.Code != http.StatusOK {
		t.Fatalf("e-mail par un administrateur sans double authentification : %d %s", response.Code, response.Body.String())
	}
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/internet-access", `{"enabled": true}`, jean, "X-Pi-Confirm-Password", "le mot de passe de jean"); response.Code != http.StatusForbidden {
		t.Fatalf("ports de la box sans double authentification : %d %s", response.Code, response.Body.String())
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
	if got := traefikInternet(false, []string{"maison.example.org"}, true, false); got != "" {
		t.Fatalf("console fermée : %q", got)
	}
	if got := traefikInternet(true, nil, true, true); got != "" {
		t.Fatalf("sans adresse : %q", got)
	}
	got := traefikInternet(true, []string{"controltower.pi-appliance.com", "maison.example.net"}, false, true)
	if !strings.Contains(got, "(Host(`controltower.pi-appliance.com`) || Host(`maison.example.net`)) && PathPrefix(`/pi-appliance`)") {
		t.Fatalf("règle :\n%s", got)
	}
	if strings.Contains(got, "cockpit") {
		t.Fatalf("Cockpit publié sans être ouvert :\n%s", got)
	}
	// Cockpit ouvert sur internet (TODO-3.4) : même authentification déléguée qu'à la maison.
	got = traefikInternet(true, []string{"maison.example.net"}, true, false)
	if !strings.Contains(got, "rule: \"(Host(`maison.example.net`)) && PathPrefix(`/cockpit`)\"") ||
		!strings.Contains(got, "middlewares: [pi-appliance-errors@file, pi-appliance-cockpit@file]") ||
		!strings.Contains(got, "service: pi-appliance-cockpit@file") {
		t.Fatalf("Cockpit :\n%s", got)
	}
}

func TestTraefikInternetPeerOnly(t *testing.T) {
	got := traefikInternet(false, []string{"maison.example.net"}, true, true)
	if !strings.Contains(got, "rule: \"(Host(`maison.example.net`)) && PathPrefix(`/pi-appliance/api/peer/`)\"") ||
		!strings.Contains(got, "service: pi-appliance-console@file") || !strings.Contains(got, "certResolver: letsencrypt") {
		t.Fatalf("API des proches :\n%s", got)
	}
	if strings.Contains(got, "pi-appliance-console-internet") || strings.Contains(got, "cockpit") {
		t.Fatalf("console ou Cockpit publiés, console fermée :\n%s", got)
	}
	if got := traefikInternet(false, nil, false, true); got != "" {
		t.Fatalf("sans adresse : %q", got)
	}
}

func TestInternetHostsFiltered(t *testing.T) {
	for host, ok := range map[string]bool{
		"maison.example.org": true, "a.example.net": true, "pi.local": false, "localhost": false,
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
		request.Header.Set("X-Pi-Appliance", "1")
		request.RemoteAddr = addr
		if got := (&console{}).fromInternet(request); got != want {
			t.Errorf("%s : %v", addr, got)
		}
	}
	// Un visiteur direct (sans Traefik) ne choisit pas son adresse par X-Forwarded-For.
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "203.0.113.9:1"
	request.Header.Set("X-Forwarded-For", "192.168.1.20")
	if !(&console{}).fromInternet(request) {
		t.Fatal("X-Forwarded-For d'un visiteur direct pris en compte")
	}
}

// Préfixe IPv6 global de la maison (TODO-3.11) : accepté comme la maison ;
// une autre adresse IPv6 d'internet reste refusée.
func TestFromInternetHomeIPv6(t *testing.T) {
	m := &services.Manager{Dir: t.TempDir(), ReadHomeIPv6: func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("2a01:cb00:1234:5600::/64")} }}
	c := &console{Config: Config{Services: m}}
	check := func(addr string, want bool) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = addr
		if got := c.fromInternet(request); got != want {
			t.Errorf("%s : %v", addr, got)
		}
	}
	check("[2a01:cb00:1234:5600::42]:1", true)
	if _, err := m.RefreshHomeIPv6(context.Background()); err != nil {
		t.Fatal(err)
	}
	check("[2a01:cb00:1234:5600::42]:1", false)
	check("[2a01:cb00:1234:5601::42]:1", true)
	check("[2001:db8::1]:1", true)
	check("203.0.113.9:1", true)
	check("192.168.1.20:1", false)
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
		request.Header.Set("X-Pi-Appliance", "1")
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
		t.Fatalf("sans service d'adresses : %q", got)
	}
	dir := t.TempDir()
	key, err := addresses.LoadOrCreateKey(filepath.Join(dir, "address-key"))
	if err != nil {
		t.Fatal(err)
	}
	c.Address, err = addresses.OpenClient(filepath.Join(dir, "address.json"), key)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.domainTarget(); got != "" {
		t.Fatalf("pas d'adresse réservée : %q", got)
	}
	// Le domaine personnel devient un alias vers <nom>.pi-appliance.com.
	os.WriteFile(filepath.Join(dir, "address.json"), []byte(`{"name":"monpi","hostname":"monpi.pi-appliance.com","ip":"90.12.34.56"}`), 0o600)
	if c.Address, err = addresses.OpenClient(filepath.Join(dir, "address.json"), key); err != nil {
		t.Fatal(err)
	}
	if got := c.domainTarget(); got != "monpi.pi-appliance.com" {
		t.Fatalf("cible : %q", got)
	}
}
