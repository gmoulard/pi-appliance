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

// memoryDNS : enregistrements A gardés en mémoire à la place d'OVH.
type memoryDNS map[string]string

func (m memoryDNS) Set(_ context.Context, hostname, ip string) error {
	m[hostname] = ip
	return nil
}

func (m memoryDNS) Remove(_ context.Context, hostname string) error {
	delete(m, hostname)
	return nil
}

// boxIP : adresse IP publique de la box dans les essais.
const boxIP = "90.12.34.56"

// newAddressFixture : console reliée à un vrai service d'adresses, OVH en mémoire.
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
	client.PublicIP = func(context.Context) (string, error) { return boxIP, nil }
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
	// Sans DuckDNS : l'adresse IP publique de la box est envoyée au service.
	response = f.do(http.MethodPost, "/pi-appliance/api/address", "application/json", `{"name": "Maison"}`, cookie)
	if status := decode[AddressStatus](t, response); response.Code != http.StatusOK || status.Hostname != "maison.pi-appliance.com" || status.IP != boxIP {
		t.Fatalf("réservation : code %d : %+v", response.Code, status)
	}
	if dns["maison.pi-appliance.com"] != boxIP {
		t.Fatalf("enregistrement A : %v", dns)
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/address", "application/json", `{"name": "autre"}`, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Cette appliance a déjà l'adresse maison.pi-appliance.com") {
		t.Fatalf("deuxième adresse : code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/address", "", "", cookie)
	if status := decode[AddressStatus](t, response); !status.Service || status.Name != "maison" || status.IP != boxIP {
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
