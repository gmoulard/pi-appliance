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
	mu     sync.Mutex
	calls  []string
	stdin  string
	joined chan struct{}
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
	case "wifi-join":
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
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	fake := &fakeNetwork{joined: make(chan struct{})}
	client := &network.Client{Runner: fake}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, Network: client}), accounts: accounts}, fake
}

func TestNetworkAdminOnly(t *testing.T) {
	f, fake := newNetworkFixture(t)
	for _, route := range [][2]string{
		{http.MethodGet, "/pi-appliance/api/network"},
		{http.MethodGet, "/pi-appliance/api/network/wifi"},
		{http.MethodPost, "/pi-appliance/api/network/wifi/scan"},
		{http.MethodPost, "/pi-appliance/api/network/wired"},
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

func TestNetworkUnavailable(t *testing.T) {
	f := newFixture(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/network", "", "", f.admin(t))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", response.Code)
	}
}
