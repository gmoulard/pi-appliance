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
	"encoding/base64"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
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
	logins := &cockpit.Logins{
		Client: client,
		File:   filepath.Join(t.TempDir(), "cockpit-accounts.json"),
		Admins: func() []string {
			var names []string
			for _, account := range f.accounts.List() {
				if account.Role == auth.RoleAdmin {
					names = append(names, account.Name)
				}
			}
			return names
		},
	}
	f.handler = Handler(Config{Version: "1.2.3", Services: f.manager, Accounts: f.accounts, ACMEEmailFile: f.acmeEmailFile, Cockpit: client, CockpitLogins: logins})
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
