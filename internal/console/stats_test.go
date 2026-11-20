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
	"github.com/gmoulard/pi-appliance/internal/stats"
	"github.com/gmoulard/pi-appliance/marketplace"
)

func TestStatsSetting(t *testing.T) {
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := stats.Open(filepath.Join(dir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	reporter.Version = "1.2.3"
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	f := fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, Stats: reporter}), accounts: accounts}
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		if response := f.do(method, "/pi-appliance/api/stats", "application/json", `{"enabled": true}`, nil); response.Code != http.StatusUnauthorized {
			t.Errorf("%s sans session : code %d", method, response.Code)
		}
	}
	cookie := f.admin(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/stats", "", "", cookie)
	status := decode[stats.Status](t, response)
	if response.Code != http.StatusOK || status.Enabled || status.Decided || status.Report.Version != "1.2.3" {
		t.Fatalf("code %d : %+v", response.Code, status)
	}
	if response := f.do(http.MethodPut, "/pi-appliance/api/stats", "application/json", `{}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("sans choix : code %d", response.Code)
	}
	response = f.do(http.MethodPut, "/pi-appliance/api/stats", "application/json", `{"enabled": true}`, cookie)
	if status := decode[stats.Status](t, response); response.Code != http.StatusOK || !status.Enabled || !status.Decided {
		t.Fatalf("code %d : %+v", response.Code, status)
	}
}

func TestStatsUnavailable(t *testing.T) {
	f := newFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/stats", "", "", f.admin(t)); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", response.Code)
	}
}
