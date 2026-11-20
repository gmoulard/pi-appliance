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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/addresses/service"
	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// memoryDNS : alias gardés en mémoire à la place de Route 53.
type memoryDNS map[string]string

func (m memoryDNS) Set(_ context.Context, hostname, target string) error {
	m[hostname] = target
	return nil
}

func (m memoryDNS) Remove(_ context.Context, hostname, _ string) error {
	delete(m, hostname)
	return nil
}

// newAddressFixture : console reliée à un vrai service d'adresses, Route 53 en mémoire.
func newAddressFixture(t *testing.T) (fixture, memoryDNS) {
	t.Helper()
	dir := t.TempDir()
	dns := memoryDNS{}
	registry, err := service.Open(filepath.Join(dir, "registry.json"), dns)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(registry.Handler())
	t.Cleanup(server.Close)
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	updater, err := ddns.Open(filepath.Join(dir, "duckdns.json"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := addresses.LoadOrCreateKey(filepath.Join(dir, "address-key"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := addresses.OpenClient(filepath.Join(dir, "address.json"), key)
	if err != nil {
		t.Fatal(err)
	}
	client.URL = server.URL
	client.Target = func() string { return updater.Status().Hostname }
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, DuckDNS: updater, Address: client}), accounts: accounts}, dns
}

func TestAddressRulesWithoutService(t *testing.T) {
	f := newDuckDNSFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/address/check?name=maison", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/address/check?name=+Maison.pi-appliance.com", "", "", cookie)
	if check := decode[AddressCheck](t, response); response.Code != http.StatusOK || !check.Valid || check.Name != "maison" || check.Hostname != "maison.pi-appliance.com" {
		t.Fatalf("code %d : %+v", response.Code, check)
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/address/check?name=www", "", "", cookie)
	if check := decode[AddressCheck](t, response); check.Valid || check.Reason != "ce nom est réservé au projet" || check.Hostname != "" {
		t.Fatalf("www : %+v", check)
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/address", "application/json", `{"name": "maison"}`, cookie)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("réservation sans service : code %d", response.Code)
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/address", "", "", cookie)
	if status := decode[AddressStatus](t, response); status.Service || status.Name != "" {
		t.Fatalf("état : %+v", status)
	}
}

func TestAddressReserveAndRelease(t *testing.T) {
	f, dns := newAddressFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if response := f.do(method, "/pi-appliance/api/address", "application/json", `{}`, nil); response.Code != http.StatusUnauthorized {
			t.Errorf("%s sans session : code %d", method, response.Code)
		}
	}
	cookie := f.admin(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/address/check?name=maison", "", "", cookie)
	if check := decode[AddressCheck](t, response); !check.Valid || !check.Available {
		t.Fatalf("libre : %+v", check)
	}
	// Sans DuckDNS, l'adresse n'a vers quoi pointer.
	response = f.do(http.MethodPost, "/pi-appliance/api/address", "application/json", `{"name": "maison"}`, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Réglez d'abord DuckDNS") {
		t.Fatalf("sans DuckDNS : code %d : %s", response.Code, response.Body.String())
	}
	f.do(http.MethodPut, "/pi-appliance/api/duckdns", "application/json", `{"domain": "monpi", "token": "`+duckdnsToken+`"}`, cookie)
	response = f.do(http.MethodPost, "/pi-appliance/api/address", "application/json", `{"name": "Maison"}`, cookie)
	if status := decode[AddressStatus](t, response); response.Code != http.StatusOK || status.Hostname != "maison.pi-appliance.com" || status.Target != "monpi.duckdns.org" {
		t.Fatalf("réservation : code %d : %+v", response.Code, status)
	}
	if dns["maison.pi-appliance.com"] != "monpi.duckdns.org" {
		t.Fatalf("alias : %v", dns)
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/address", "application/json", `{"name": "autre"}`, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Cette appliance a déjà l'adresse maison.pi-appliance.com") {
		t.Fatalf("deuxième adresse : code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/address", "", "", cookie)
	if status := decode[AddressStatus](t, response); !status.Service || status.Name != "maison" || status.DuckDNS != "monpi.duckdns.org" {
		t.Fatalf("état : %+v", status)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/address", "", "", cookie); response.Code != http.StatusNoContent {
		t.Fatalf("libération : code %d : %s", response.Code, response.Body.String())
	}
	if len(dns) != 0 {
		t.Fatalf("alias restants : %v", dns)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/address", "", "", cookie); response.Code != http.StatusConflict {
		t.Fatalf("deuxième libération : code %d", response.Code)
	}
}
