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
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/cockpit"
)

// cockpitScript tient lieu de cockpit.sh : chaque administrateur reçoit son
// nom comme compte système.
type cockpitScript struct{ fail bool }

func (s cockpitScript) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	if args[0] != "sync" {
		return []byte(`{"installed": true, "active": true, "accounts": []}`), nil
	}
	if s.fail {
		return []byte(`{"ok": false, "error": "failed", "message": "Échec."}`), context.Canceled
	}
	var pairs []string
	for _, line := range strings.Split(strings.TrimSpace(string(stdin)), "\n") {
		name, _, _ := strings.Cut(line, " ")
		pairs = append(pairs, `"`+name+`": "`+name+`"`)
	}
	return []byte(`{"ok": true, "accounts": {` + strings.Join(pairs, ",") + `}, "conflicts": []}`), nil
}

func cockpitFixture(t *testing.T, script cockpitScript) fixture {
	t.Helper()
	f := newFixture(t)
	client := &cockpit.Client{Runner: script}
	dir := t.TempDir()
	access := &cockpit.AccessFile{Path: filepath.Join(dir, "cockpit-access.json")}
	logins := &cockpit.Logins{
		Client: client,
		File:   filepath.Join(dir, "cockpit-accounts.json"),
		Admins: CockpitAccounts(f.accounts, access),
	}
	f.handler = Handler(Config{Version: "1.2.3", Services: f.manager, Accounts: f.accounts, ACMEEmailFile: f.acmeEmailFile,
		Cockpit: client, CockpitLogins: logins, CockpitAccess: access,
		InternetFile: filepath.Join(dir, "internet-console.json"), TraefikInternetFile: filepath.Join(dir, "console-internet.yml")})
	f.manager.Domain = "maison.local"
	return f
}

// Connexion unique à Cockpit : le compte système de l'administrateur et son
// mot de passe passent à Cockpit par l'en-tête Authorization ; jamais pour un
// simple utilisateur.
func TestForwardCockpit(t *testing.T) {
	f := cockpitFixture(t, cockpitScript{})
	admin := f.admin(t)
	headers := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "maison.local", "X-Forwarded-Uri": "/cockpit/", "X-Forwarded-Method": "GET"}
	const path = "/pi-appliance/api/auth/forward-cockpit"

	if response := forwardTo(f, path, "192.168.1.20:5000", nil, headers); response.Code != http.StatusForbidden || response.Header().Get("Authorization") != "" {
		t.Fatalf("appel hors de Traefik : %d", response.Code)
	}
	if location, _ := loginRedirect(t, f, path, headers); location.Host != "maison.local" {
		t.Fatalf("sans session : %s", location)
	}
	response := forwardTo(f, path, "127.0.0.1:5000", forwardCookieFor(t, f, admin, path, headers), headers)
	authorization := response.Header().Get("Authorization")
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(authorization, "Basic "))
	user, password, _ := strings.Cut(string(raw), ":")
	if response.Code != http.StatusOK || user != "marie" || len(password) < 32 || response.Header().Get("X-Superuser") != "any" {
		t.Fatalf("administrateur : %d %q %v", response.Code, raw, response.Header())
	}
	// Même mot de passe à la demande suivante (Cockpit garde sa session).
	again := forwardTo(f, path, "127.0.0.1:5000", forwardCookieFor(t, f, admin, path, headers), headers)
	if again.Header().Get("Authorization") != authorization {
		t.Fatal("mot de passe changé d'une demande à l'autre")
	}
	// L'écran de la console donne le compte système.
	if status := f.do(http.MethodGet, "/pi-appliance/api/cockpit", "", "", admin); !strings.Contains(status.Body.String(), `"user":"marie"`) {
		t.Fatalf("état : %s", status.Body)
	}

	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	cookie := forwardCookieFor(t, f, paul, "/pi-appliance/api/auth/forward", headers)
	if response := forwardTo(f, path, "127.0.0.1:5000", cookie, headers); response.Code != http.StatusForbidden || response.Header().Get("Authorization") != "" {
		t.Fatalf("utilisateur : %d %v", response.Code, response.Header())
	}
}

// Compte système impossible à créer : une page qui le dit, jamais d'en-tête
// Authorization (Traefik retire alors celui du visiteur).
func TestForwardCockpitNotReady(t *testing.T) {
	f := cockpitFixture(t, cockpitScript{fail: true})
	admin := f.admin(t)
	headers := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "maison.local", "X-Forwarded-Uri": "/cockpit/", "X-Forwarded-Method": "GET"}
	const path = "/pi-appliance/api/auth/forward-cockpit"
	response := forwardTo(f, path, "127.0.0.1:5000", forwardCookieFor(t, f, admin, path, headers), headers)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Authorization") != "" || !strings.Contains(response.Body.String(), "Cockpit") {
		t.Fatalf("pas prêt : %d %v", response.Code, response.Header())
	}
}

// Accès à Cockpit (TODO-3.4) : les utilisateurs passent quand un
// administrateur le permet, jamais les invités ; depuis internet, seulement
// si la console et Cockpit y sont ouverts, et l'ouvrir se fait depuis la maison.
func TestCockpitAccess(t *testing.T) {
	f := cockpitFixture(t, cockpitScript{})
	admin := f.admin(t)
	headers := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "maison.local", "X-Forwarded-Uri": "/cockpit/", "X-Forwarded-Method": "GET"}
	const path = "/pi-appliance/api/auth/forward-cockpit"
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	guestToken := f.invite(t, admin, `{"name": "ines", "email": "ines@example.org", "role": "guest", "expires": "2099-12-31T00:00:00Z"}`)
	ines := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+guestToken+`", "password": "huit car"}`, nil))
	paulForward := forwardCookieFor(t, f, paul, path, headers)
	inesForward := forwardCookieFor(t, f, ines, path, headers)

	if response := forwardTo(f, path, "127.0.0.1:5000", paulForward, headers); response.Code != http.StatusForbidden {
		t.Fatalf("utilisateur, par défaut : %d", response.Code)
	}
	if status := f.do(http.MethodGet, "/pi-appliance/api/cockpit/access", "", "", paul); !strings.Contains(status.Body.String(), `"allowed":false`) {
		t.Fatalf("état pour l'utilisateur : %s", status.Body)
	}
	// Réglage réservé à l'administrateur.
	if response := f.do(http.MethodPut, "/pi-appliance/api/cockpit/access", "application/json", `{"users": true}`, paul); response.Code != http.StatusForbidden {
		t.Fatalf("réglage par un utilisateur : %d", response.Code)
	}
	if response := f.do(http.MethodPut, "/pi-appliance/api/cockpit/access", "application/json", `{"users": true}`, admin); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"users":true`) {
		t.Fatalf("ouvert aux utilisateurs : %d %s", response.Code, response.Body)
	}
	response := forwardTo(f, path, "127.0.0.1:5000", paulForward, headers)
	raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(response.Header().Get("Authorization"), "Basic "))
	if response.Code != http.StatusOK || !strings.HasPrefix(string(raw), "paul:") || response.Header().Get("X-Superuser") != "any" {
		t.Fatalf("utilisateur admis : %d %q", response.Code, raw)
	}
	if status := f.do(http.MethodGet, "/pi-appliance/api/cockpit/access", "", "", paul); !strings.Contains(status.Body.String(), `"allowed":true`) || !strings.Contains(status.Body.String(), `"installed":true`) {
		t.Fatalf("état pour l'utilisateur admis : %s", status.Body)
	}
	if response := forwardTo(f, path, "127.0.0.1:5000", inesForward, headers); response.Code != http.StatusForbidden || response.Header().Get("Authorization") != "" {
		t.Fatalf("invitée : %d", response.Code)
	}

	// Depuis internet : refusé tant que Cockpit n'y est pas ouvert.
	outside := map[string]string{"X-Forwarded-For": "203.0.113.9"}
	for key, value := range headers {
		outside[key] = value
	}
	if response := forwardTo(f, path, "127.0.0.1:5000", paulForward, outside); response.Code != http.StatusForbidden || response.Header().Get("Authorization") != "" {
		t.Fatalf("depuis internet, fermé : %d", response.Code)
	}
	// L'ouvrir sur internet depuis internet : refusé ; depuis la maison : permis.
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/cockpit/access", `{"users": true, "internet": true}`, admin); response.Code == http.StatusOK {
		t.Fatalf("ouvert depuis internet : %d", response.Code)
	}
	if response := f.do(http.MethodPut, "/pi-appliance/api/cockpit/access", "application/json", `{"users": true, "internet": true}`, admin); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"console_internet":false`) {
		t.Fatalf("ouvert sur internet : %d %s", response.Code, response.Body)
	}
	// Console fermée sur internet : Cockpit ne l'est pas davantage.
	if response := forwardTo(f, path, "127.0.0.1:5000", paulForward, outside); response.Code != http.StatusForbidden {
		t.Fatalf("console fermée sur internet : %d", response.Code)
	}
	if response := f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", "", admin); response.Code != http.StatusOK {
		t.Fatalf("console ouverte : %d %s", response.Code, response.Body)
	}
	if response := forwardTo(f, path, "127.0.0.1:5000", paulForward, outside); response.Code != http.StatusOK || response.Header().Get("Authorization") == "" {
		t.Fatalf("depuis internet, ouvert : %d", response.Code)
	}
	// Refermer depuis internet est permis ; l'accès est alors coupé tout de suite.
	if response := f.fromOutside(http.MethodPut, "/pi-appliance/api/cockpit/access", `{"users": false, "internet": false}`, admin); response.Code != http.StatusOK {
		t.Fatalf("refermé depuis internet : %d %s", response.Code, response.Body)
	}
	if response := forwardTo(f, path, "127.0.0.1:5000", paulForward, headers); response.Code != http.StatusForbidden {
		t.Fatalf("utilisateur après fermeture : %d", response.Code)
	}
}
