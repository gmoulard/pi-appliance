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
)

const factoryPath = "/pi-appliance/api/factory-reset"

func TestFactoryResetNeedsAdminPassword(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true}`, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"password": "`+adminPassword+`"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("sans confirmation : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true, "password": "pas le bon"}`, cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("mauvais mot de passe : code %d : %s", response.Code, response.Body.String())
	}
	if f.accounts.SetupNeeded() {
		t.Fatal("comptes effacés malgré un refus")
	}
}

func TestFactoryResetKeepsServices(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	if response := f.install(`{"app": "dokuwiki", "name": "wiki"}`, cookie); response.Code != http.StatusCreated {
		t.Fatalf("installation : %d %s", response.Code, response.Body.String())
	}
	response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true, "password": "`+adminPassword+`"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if !f.accounts.SetupNeeded() {
		t.Fatal("comptes gardés")
	}
	if _, err := os.Stat(f.acmeEmailFile); !os.IsNotExist(err) {
		t.Fatalf("adresse e-mail de l'administrateur gardée : %v", err)
	}
	instances, err := f.manager.List()
	if err != nil || len(instances) != 1 {
		t.Fatalf("services %v %v", instances, err)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("ancienne session encore ouverte : %d", response.Code)
	}
}

func TestFactoryResetDeletesServicesOnRequest(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	f.install(`{"app": "dokuwiki", "name": "wiki"}`, cookie)
	response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true, "delete_services": true, "password": "`+adminPassword+`"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if instances, _ := f.manager.List(); len(instances) != 0 {
		t.Fatalf("services gardés : %v", instances)
	}
}

func TestFactoryResetWithResetFile(t *testing.T) {
	f := newFixture(t)
	f.admin(t)
	file := filepath.Join(t.TempDir(), "reset")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.ConsumeResetFile(file); err != nil {
		t.Fatal(err)
	}
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true}`, nil); response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if !f.accounts.SetupNeeded() || f.accounts.ResetPending() {
		t.Fatal("pas revenu à la création du compte")
	}
}

func TestFactoryResetPage(t *testing.T) {
	response := get(t, "/pi-appliance/reset-to-factory")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "factorySection") {
		t.Fatalf("code %d", response.Code)
	}
}
