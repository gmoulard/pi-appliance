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
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/network"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// fakeNetwork remplace network.sh.
type fakeNetwork struct {
	mu          sync.Mutex
	calls       []string
	stdin       string
	joined      chan struct{}
	rebootFails bool
}

func (f *fakeNetwork) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args[0])
	switch args[0] {
	case "status":
		return []byte(`{"ok": true, "accessPoint": {"open": true, "ssid": "pi-appliance", "reason": "first-boot", "closesInSeconds": 3600}, "configured": "", "wired": {"connected": false}, "wifi": {"connected": false, "ssid": ""}, "addresses": [], "lastError": ""}`), nil
	case "wifi-list", "wifi-scan":
		return []byte(`{"scannedAt": "2026-11-19T10:00:00Z", "networks": [{"ssid": "maison", "signal": 80, "security": "wpa"}]}`), nil
	case "wired":
		return []byte(`{"ok": false, "error": "no-cable", "message": "Aucun câble réseau branché, ou la box ne répond pas."}`), errors.New("exit status 1")
	case "country":
		return []byte(`{"ok": true, "country": "` + args[1] + `"}`), nil
	case "name":
		return []byte(`{"ok": true, "name": "` + args[1] + `"}`), nil
	case "test":
		return []byte(`{"ok": true, "network": "wifi", "ssid": "maison", "name": "pi-appliance-1a2b", "addresses": ["192.168.1.20"], "checks": {"connected": true, "address": true, "box": true, "internet": false, "saved": true, "localName": true}, "passed": true}`), nil
	case "reboot":
		if f.rebootFails {
			return []byte(`{"ok": false, "error": "failed", "message": "Le Pi n'a pas pu redémarrer. Débranchez-le puis rebranchez-le."}`), errors.New("exit status 1")
		}
		return []byte(`{"ok": true, "inSeconds": 5}`), nil
	case "wifi-join", "wifi-wps":
		f.stdin = string(stdin)
		close(f.joined)
		return []byte(`{"ok": true, "ssid": "maison"}`), nil
	}
	return nil, errors.New("commande inconnue")
}

func newNetworkFixture(t *testing.T) (fixture, *fakeNetwork) {
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
	manager := &services.Manager{Dir: dir, Domain: "pi-appliance-1a2b.local", Runner: okRunner{}, FollowName: true}
	fake := &fakeNetwork{joined: make(chan struct{})}
	client := &network.Client{Runner: fake}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, Network: client}), accounts: accounts, manager: manager}, fake
}

func TestNetworkAdminOnly(t *testing.T) {
	f, fake := newNetworkFixture(t)
	for _, route := range [][2]string{
		{http.MethodGet, "/pi-appliance/api/network"},
		{http.MethodGet, "/pi-appliance/api/network/wifi"},
		{http.MethodPost, "/pi-appliance/api/network/wifi/scan"},
		{http.MethodPost, "/pi-appliance/api/network/wired"},
		{http.MethodPost, "/pi-appliance/api/network/wifi/wps"},
	} {
		if response := f.do(route[0], route[1], "", "", nil); response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s sans session : code %d", route[0], route[1], response.Code)
		}
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/network/wifi", "application/json", `{"ssid": "maison"}`, nil); response.Code != http.StatusUnauthorized {
		t.Errorf("connexion au Wi-Fi sans session : code %d", response.Code)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("network.sh lancé sans session : %v", fake.calls)
	}
}

func TestNetworkStatusAndList(t *testing.T) {
	f, fake := newNetworkFixture(t)
	cookie := f.admin(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/network", "", "", cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"closesInSeconds": 3600`) {
		t.Fatalf("état : code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/network/wifi", "", "", cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ssid": "maison"`) {
		t.Fatalf("liste : code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/network/wifi/scan", "", "", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("recherche : code %d", response.Code)
	}
	if got := strings.Join(fake.calls, ","); got != "status,wifi-list,wifi-scan" {
		t.Fatalf("commandes %s", got)
	}
}

func TestNetworkWiredFailure(t *testing.T) {
	f, _ := newNetworkFixture(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/network/wired", "", "", f.admin(t))
	body := decode[map[string]string](t, response)
	if response.Code != http.StatusConflict || body["code"] != "no-cable" || !strings.HasPrefix(body["error"], "Aucun câble") {
		t.Fatalf("code %d : %v", response.Code, body)
	}
}

func TestNetworkJoin(t *testing.T) {
	f, fake := newNetworkFixture(t)
	cookie := f.admin(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/network/wifi", "application/json", `{"ssid": "maison", "password": "court"}`, cookie)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("mot de passe trop court : code %d", response.Code)
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/network/wifi", "application/json", `{"ssid": "maison", "password": "mot de passe", "hidden": false}`, cookie)
	if response.Code != http.StatusAccepted {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "mot de passe") {
		t.Fatalf("mot de passe renvoyé : %s", response.Body.String())
	}
	<-fake.joined
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !strings.Contains(fake.stdin, `"password":"mot de passe"`) {
		t.Fatalf("entrée standard %s", fake.stdin)
	}
}

func TestNetworkWPS(t *testing.T) {
	f, fake := newNetworkFixture(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/network/wifi/wps", "", "", f.admin(t))
	if response.Code != http.StatusAccepted {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	<-fake.joined
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got := fake.calls[len(fake.calls)-1]; got != "wifi-wps" {
		t.Fatalf("commande %s", got)
	}
}

func TestNetworkUnavailable(t *testing.T) {
	f := newFixture(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/network", "", "", f.admin(t))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", response.Code)
	}
}

func TestNetworkTest(t *testing.T) {
	f, fake := newNetworkFixture(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/network/test", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	response := f.do(http.MethodPost, "/pi-appliance/api/network/test", "", "", f.admin(t))
	if body := response.Body.String(); response.Code != http.StatusOK || !strings.Contains(body, `"passed": true`) || !strings.Contains(body, `"192.168.1.20"`) {
		t.Fatalf("code %d : %s", response.Code, body)
	}
	if got := strings.Join(fake.calls, ","); got != "test" {
		t.Fatalf("commandes %s", got)
	}
}

func TestReboot(t *testing.T) {
	f, fake := newNetworkFixture(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/system/reboot", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("redémarrage sans session : %v", fake.calls)
	}
	cookie := f.admin(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/system/reboot", "", "", cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"inSeconds": 5`) {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if got := strings.Join(fake.calls, ","); got != "reboot" {
		t.Fatalf("commandes %s", got)
	}
	fake.rebootFails = true
	response = f.do(http.MethodPost, "/pi-appliance/api/system/reboot", "", "", cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "rebranchez") {
		t.Fatalf("échec : code %d : %s", response.Code, response.Body.String())
	}
}
