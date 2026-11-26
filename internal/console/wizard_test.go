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
		Version:          "1.2.3",
		Catalog:          catalog,
		Services:         &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}},
		Accounts:         accounts,
		WizardFile:       filepath.Join(dir, "wizard-done"),
		WizardUpdateFile: filepath.Join(dir, "wizard-update-done"),
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

// Mise à jour du premier démarrage (spécification 5.1) : notée une fois,
// rendue par la session tant que l'assistant n'est pas fini, effacée par la
// remise à zéro.
func TestWizardUpdate(t *testing.T) {
	f := newWizardFixture(t)
	cookie := f.admin(t)
	if state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", cookie)); !state.Wizard || state.WizardUpdated {
		t.Fatalf("assistant sans mise à jour attendu : %+v", state)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/wizard/update", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/wizard/update", "", "", cookie); response.Code != http.StatusNoContent {
		t.Fatalf("mise à jour : code %d", response.Code)
	}
	if state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", cookie)); !state.Wizard || !state.WizardUpdated {
		t.Fatalf("mise à jour notée attendue : %+v", state)
	}
	response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true, "password": "`+adminPassword+`"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("remise à zéro : code %d : %s", response.Code, response.Body.String())
	}
	cookie = f.admin(t)
	if state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", cookie)); !state.Wizard || state.WizardUpdated {
		t.Fatalf("mise à jour effacée par la remise à zéro attendue : %+v", state)
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

func TestSetName(t *testing.T) {
	f, fake := newNetworkFixture(t)
	cookie := f.admin(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/network/name", "application/json", `{"name": "Salon"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("majuscules : code %d", response.Code)
	}
	if got := f.manager.CurrentDomain(); got != "pi-appliance-1a2b.local" {
		t.Fatalf("adresse après un nom refusé : %s", got)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/network/name", "application/json", `{"name": "controltower"}`, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	response := f.do(http.MethodPost, "/pi-appliance/api/network/name", "application/json", `{"name": "controltower"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if got := strings.Join(fake.calls, ","); got != "name" {
		t.Fatalf("commandes %s", got)
	}
	if got := f.manager.CurrentDomain(); got != "controltower.local" {
		t.Fatalf("adresse des nouveaux services : %s", got)
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
