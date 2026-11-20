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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/services"
)

func listAccess(t *testing.T, f fixture, cookie *http.Cookie) string {
	t.Helper()
	list := decode[struct {
		Services []ServiceView `json:"services"`
	}](t, f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie))
	if len(list.Services) != 1 {
		t.Fatalf("liste %+v", list.Services)
	}
	return list.Services[0].Access
}

func TestServiceAccess(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	if response := f.install(`{"app": "nextcloud", "access": "partout"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("accès inconnu : code %d", response.Code)
	}
	if response := f.install(`{"app": "nextcloud", "access": "home"}`, cookie); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	if got := listAccess(t, f, cookie); got != services.AccessHome {
		t.Fatalf("accès %q", got)
	}
	set := func(name, body string, cookie *http.Cookie) int {
		return f.do(http.MethodPut, "/pi-appliance/api/services/"+name+"/access", "application/json", body, cookie).Code
	}
	if code := set("cloud", `{"access": "users"}`, cookie); code != http.StatusOK {
		t.Fatalf("changement : code %d", code)
	}
	if got := listAccess(t, f, cookie); got != services.AccessUsers {
		t.Fatalf("accès %q", got)
	}
	cases := map[string]struct {
		name, body string
		cookie     *http.Cookie
		code       int
	}{
		"niveau inconnu":  {"cloud", `{"access": "partout"}`, cookie, http.StatusBadRequest},
		"service inconnu": {"autre", `{"access": "home"}`, cookie, http.StatusNotFound},
		"sans session":    {"cloud", `{"access": "public"}`, nil, http.StatusUnauthorized},
	}
	for label, c := range cases {
		if code := set(c.name, c.body, c.cookie); code != c.code {
			t.Fatalf("%s : code %d, attendu %d", label, code, c.code)
		}
	}
}

func TestInstallUsesManifestAccess(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	if code := f.install(`{"app": "dokuwiki"}`, cookie).Code; code != http.StatusCreated {
		t.Fatalf("installation : code %d", code)
	}
	if got := listAccess(t, f, cookie); got != services.AccessUsers {
		t.Fatalf("accès %q, attendu celui du manifeste", got)
	}
}

// Portainer a la main sur tout le Pi (accès à Docker) : installation confirmée,
// accès « administrateurs, à la maison » posé par le manifeste et gardé.
func TestInstallPortainer(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	if response := f.install(`{"app": "portainer", "access": "public", "confirm_risks": true}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("accès public : code %d", response.Code)
	}
	response := f.install(`{"app": "portainer"}`, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"docker-socket"`) {
		t.Fatalf("sans confirmation : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.install(`{"app": "portainer", "confirm_risks": true}`, cookie); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	if got := listAccess(t, f, cookie); got != services.AccessAdmins {
		t.Fatalf("accès %q", got)
	}
	override, err := os.ReadFile(filepath.Join(f.manager.Dir, "services", "portainer", "pi-appliance.override.json"))
	if err != nil || !strings.Contains(string(override), `"pi-appliance-home@file,pi-appliance-admins@file"`) {
		t.Fatalf("labels Traefik : %v\n%s", err, override)
	}
	for _, access := range []string{"public", "home", "admins"} {
		if code := f.do(http.MethodPut, "/pi-appliance/api/services/portainer/access", "application/json", `{"access": "`+access+`"}`, cookie).Code; code != http.StatusBadRequest {
			t.Fatalf("changement vers %s : code %d", access, code)
		}
	}
	if response := f.install(`{"app": "dokuwiki", "name": "wiki", "access": "admins"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("accès admins choisi pour un autre service : code %d", response.Code)
	}
}
