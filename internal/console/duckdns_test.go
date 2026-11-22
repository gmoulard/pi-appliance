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
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

const duckdnsToken = "a7c4d0ad-114e-40ef-ba1d-d217904a50f2"

func newDuckDNSFixture(t *testing.T) fixture {
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
	updater, err := ddns.Open(filepath.Join(dir, "duckdns.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, DuckDNS: updater}), accounts: accounts}
}

func TestDuckDNSAdminOnly(t *testing.T) {
	f := newDuckDNSFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if response := f.do(method, "/pi-appliance/api/duckdns", "application/json", `{}`, nil); response.Code != http.StatusUnauthorized {
			t.Errorf("%s sans session : code %d", method, response.Code)
		}
	}
}

func TestDuckDNSSaveAndStatus(t *testing.T) {
	f := newDuckDNSFixture(t)
	cookie := f.admin(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/duckdns", "", "", cookie)
	if status := decode[ddns.Status](t, response); response.Code != http.StatusOK || status.Configured {
		t.Fatalf("code %d : %+v", response.Code, status)
	}
	response = f.do(http.MethodPut, "/pi-appliance/api/duckdns", "application/json", `{"domain": "mon_pi", "token": "`+duckdnsToken+`"}`, cookie)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "identifiant") {
		t.Fatalf("identifiant invalide : code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodPut, "/pi-appliance/api/duckdns", "application/json", `{"domain": "monpi.duckdns.org", "token": "`+duckdnsToken+`"}`, cookie)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), duckdnsToken) {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/duckdns", "", "", cookie)
	body := response.Body.String()
	status := decode[ddns.Status](t, response)
	if !status.Configured || status.Hostname != "monpi.duckdns.org" || strings.Contains(body, duckdnsToken) {
		t.Fatalf("état %s", body)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/duckdns", "", "", cookie); response.Code != http.StatusNoContent {
		t.Fatalf("effacement : code %d", response.Code)
	}
}

func TestDuckDNSUnavailable(t *testing.T) {
	f := newFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/duckdns", "", "", f.admin(t)); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", response.Code)
	}
}
