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
	"path/filepath"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/updates"
	"github.com/gmoulard/pi-appliance/marketplace"
)

func newUpdatesFixture(t *testing.T) fixture {
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
	checker := &updates.Checker{Version: "1.2.3"}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, Updates: checker}), accounts: accounts}
}

func TestUpdatesAdminOnly(t *testing.T) {
	f := newUpdatesFixture(t)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/pi-appliance/api/updates"},
		{http.MethodPost, "/pi-appliance/api/updates/check"},
		{http.MethodPost, "/pi-appliance/api/updates/system"},
	} {
		if response := f.do(route.method, route.path, "", "", nil); response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sans session : code %d", route.method, route.path, response.Code)
		}
	}
	response := f.do(http.MethodGet, "/pi-appliance/api/updates", "", "", f.admin(t))
	if status := decode[updates.Status](t, response); response.Code != http.StatusOK || status.Appliance.Current != "1.2.3" || status.System != nil {
		t.Fatalf("code %d : %+v", response.Code, status)
	}
}

func TestUpdatesAbsent(t *testing.T) {
	f := newFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/updates", "", "", f.admin(t)); response.Code == http.StatusOK {
		t.Fatalf("sans mises à jour : code %d", response.Code)
	}
}
