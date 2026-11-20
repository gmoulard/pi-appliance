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
