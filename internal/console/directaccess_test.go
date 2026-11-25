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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/upnp"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// soapBox : fausse box qui accepte tout et dit son adresse publique.
type soapBox struct {
	mu       sync.Mutex
	external string
	actions  []string
}

func (b *soapBox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	action := r.Header.Get("SOAPAction")
	action = strings.Trim(action[strings.Index(action, "#")+1:], `"`)
	b.mu.Lock()
	port := ""
	if start := strings.Index(string(body), "<NewExternalPort>"); start >= 0 {
		port = string(body)[start+17 : start+17+strings.Index(string(body)[start+17:], "<")]
	}
	b.actions = append(b.actions, action+" "+port)
	b.mu.Unlock()
	fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="x"><NewExternalIPAddress>%s</NewExternalIPAddress></u:%sResponse></s:Body></s:Envelope>`, action, b.external, action)
}

func newAccessFixture(t *testing.T, external string, found bool) (fixture, *soapBox) {
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
	if err := updater.Save(ddns.Settings{Domain: "monpi", Token: duckdnsToken}); err != nil {
		t.Fatal(err)
	}
	box := &soapBox{external: external}
	server := httptest.NewServer(box)
	t.Cleanup(server.Close)
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	mapper, err := upnp.Open(filepath.Join(dir, "upnp.json"))
	if err != nil {
		t.Fatal(err)
	}
	mapper.Ports = func() []upnp.Mapping { return DirectPorts(manager) }
	mapper.Find = func(context.Context) (*upnp.Gateway, error) {
		if !found {
			return nil, upnp.ErrNoGateway
		}
		return &upnp.Gateway{Name: "Livebox", ControlURL: server.URL, ServiceType: "urn:schemas-upnp-org:service:WANIPConnection:1", LocalIP: "192.168.1.10"}, nil
	}
	config := Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, DuckDNS: updater, PortMapper: mapper,
		PublicIP: func(context.Context) (string, error) { return "203.0.113.7", nil },
		LookupHost: func(_ context.Context, host string) ([]string, error) {
			if host == "monpi.duckdns.org" {
				return []string{"203.0.113.7"}, nil
			}
			return nil, errors.New("introuvable")
		},
	}
	return fixture{handler: Handler(config), accounts: accounts, manager: manager}, box
}

func TestDirectPorts(t *testing.T) {
	ports := DirectPorts(nil)
	if len(ports) != 2 || ports[0] != (upnp.Mapping{Port: 80, Protocol: "TCP"}) || ports[1] != (upnp.Mapping{Port: 443, Protocol: "TCP"}) {
		t.Fatalf("ports : %v", ports)
	}
}

func TestInternetAccessAdminOnly(t *testing.T) {
	f, _ := newAccessFixture(t, "203.0.113.7", true)
	for _, request := range [][2]string{{http.MethodGet, "/pi-appliance/api/internet-access"}, {http.MethodPut, "/pi-appliance/api/internet-access"}, {http.MethodPost, "/pi-appliance/api/internet-access/test"}, {http.MethodGet, "/pi-appliance/api/address/service"}} {
		if response := f.do(request[0], request[1], "application/json", `{"enabled": true}`, nil); response.Code != http.StatusUnauthorized {
			t.Errorf("%s sans session : code %d", request[1], response.Code)
		}
	}
}

func TestInternetAccessEnable(t *testing.T) {
	f, box := newAccessFixture(t, "203.0.113.7", true)
	cookie := f.admin(t)
	status := decode[AccessStatus](t, f.do(http.MethodGet, "/pi-appliance/api/internet-access", "", "", cookie))
	if !status.Automatic || status.UPnP == nil || status.UPnP.Enabled || len(status.Ports) != 2 || len(box.actions) != 0 {
		t.Fatalf("avant l'accord : %+v, %v", status, box.actions)
	}
	response := f.do(http.MethodPut, "/pi-appliance/api/internet-access", "application/json", `{}`, cookie)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("sans choix : code %d", response.Code)
	}
	response = f.do(http.MethodPut, "/pi-appliance/api/internet-access", "application/json", `{"enabled": true}`, cookie)
	status = decode[AccessStatus](t, response)
	if response.Code != http.StatusOK || !status.UPnP.Enabled || status.UPnP.ChangedBy != "marie" || status.LocalIP != "192.168.1.10" || !status.UPnP.Ports[0].Open || !status.UPnP.Ports[1].Open {
		t.Fatalf("accord : code %d : %+v", response.Code, status)
	}
	if !strings.Contains(strings.Join(box.actions, ","), "AddPortMapping 443") {
		t.Fatalf("actions : %v", box.actions)
	}
	test := decode[AccessTest](t, f.do(http.MethodPost, "/pi-appliance/api/internet-access/test", "", "", cookie))
	if !test.Box || test.BoxName != "Livebox" || test.Shared || test.DoubleBox || !test.Automatic || len(test.Ports) != 2 {
		t.Fatalf("test : %+v", test)
	}
	if len(test.DNS) != 2 || test.DNS[1].Matches || test.DNS[1].Error == "" || test.DNS[0].Host != "monpi.duckdns.org" || !test.DNS[0].Matches || test.URL != "https://monpi.duckdns.org/pi-appliance/" {
		t.Fatalf("adresse : %+v", test)
	}
	response = f.do(http.MethodPut, "/pi-appliance/api/internet-access", "application/json", `{"enabled": false}`, cookie)
	status = decode[AccessStatus](t, response)
	if status.UPnP.Enabled || !strings.Contains(strings.Join(box.actions, ","), "DeletePortMapping 443") {
		t.Fatalf("retrait : %+v, %v", status, box.actions)
	}
}

func TestInternetAccessSharedAddress(t *testing.T) {
	f, _ := newAccessFixture(t, "100.64.12.3", true)
	cookie := f.admin(t)
	test := decode[AccessTest](t, f.do(http.MethodPost, "/pi-appliance/api/internet-access/test", "", "", cookie))
	if !test.Box || !test.Shared || test.DoubleBox {
		t.Fatalf("adresse partagée : %+v", test)
	}
	f, _ = newAccessFixture(t, "198.51.100.4", true)
	cookie = f.admin(t)
	test = decode[AccessTest](t, f.do(http.MethodPost, "/pi-appliance/api/internet-access/test", "", "", cookie))
	if test.Shared || !test.DoubleBox {
		t.Fatalf("deux box : %+v", test)
	}
}

func TestInternetAccessNoBox(t *testing.T) {
	f, _ := newAccessFixture(t, "", false)
	cookie := f.admin(t)
	test := decode[AccessTest](t, f.do(http.MethodPost, "/pi-appliance/api/internet-access/test", "", "", cookie))
	if test.Box || !strings.Contains(test.BoxError, "UPnP") || test.PublicIP != "203.0.113.7" {
		t.Fatalf("sans box : %+v", test)
	}
	if reachable := decode[map[string]bool](t, f.do(http.MethodGet, "/pi-appliance/api/address/service", "", "", cookie)); reachable["reachable"] {
		t.Fatal("service d'adresses joignable sans service")
	}
}

// Depuis internet, ouvrir les ports de la box est confirmé (mot de passe et code).
func TestInternetAccessConfirmed(t *testing.T) {
	if !slices.Contains(confirmPaths, "PUT /pi-appliance/api/internet-access") {
		t.Fatal("ouverture des ports sans confirmation depuis internet")
	}
}
