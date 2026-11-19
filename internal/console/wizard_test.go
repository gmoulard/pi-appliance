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
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/system"
	"github.com/gmoulard/pi-appliance/marketplace"
)

func newWizardFixture(t *testing.T) fixture {
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
	return fixture{handler: Handler(Config{
		Version:    "1.2.3",
		Catalog:    catalog,
		Services:   &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}},
		Accounts:   accounts,
		WizardFile: filepath.Join(dir, "wizard-done"),
		Resources: func() system.Resources {
			return system.Resources{Memory: &system.Gauge{TotalMB: 1024, UsedMB: 300, FreeMB: 724}}
		},
	}), accounts: accounts}
}

func TestWizard(t *testing.T) {
	f := newWizardFixture(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/wizard", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	if state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", cookie)); !state.Wizard {
		t.Fatalf("assistant attendu : %+v", state)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/wizard", "", "", cookie); response.Code != http.StatusNoContent {
		t.Fatalf("fin de l'assistant : code %d", response.Code)
	}
	if state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", cookie)); state.Wizard {
		t.Fatalf("assistant encore proposé : %+v", state)
	}
}

func TestWizardDisabled(t *testing.T) {
	f := newFixture(t)
	if state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", f.admin(t))); state.Wizard {
		t.Fatalf("assistant sans WizardFile : %+v", state)
	}
}

func TestResources(t *testing.T) {
	f := newWizardFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/system", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	response := f.do(http.MethodGet, "/pi-appliance/api/system", "", "", f.admin(t))
	if body := response.Body.String(); response.Code != http.StatusOK || !strings.Contains(body, `"free_mb":724`) || !strings.Contains(body, `"disk":null`) {
		t.Fatalf("code %d : %s", response.Code, body)
	}
}

func TestCountry(t *testing.T) {
	f, fake := newNetworkFixture(t)
	cookie := f.admin(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/network/country", "application/json", `{"country": "fr"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("code en minuscules : code %d", response.Code)
	}
	response := f.do(http.MethodPost, "/pi-appliance/api/network/country", "application/json", `{"country": "BE"}`, cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"BE"`) {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if got := strings.Join(fake.calls, ","); got != "country" {
		t.Fatalf("commandes %s", got)
	}
}
