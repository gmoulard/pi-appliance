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
	"encoding/json"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func TestWireGuardAPI(t *testing.T) {
	f := newFixture(t)
	f.manager.HomeNetworks = func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")} }
	if response := f.do(http.MethodGet, "/pi-appliance/api/services/vpn/wireguard", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/services", "application/json", `{"app": "wireguard", "name": "vpn"}`, cookie)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "port UDP 51820") {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}

	response = f.do(http.MethodPost, "/pi-appliance/api/services/vpn/wireguard/devices", "application/json", `{"name": "Téléphone"}`, cookie)
	if response.Code != http.StatusCreated || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("ajout : code %d : %s", response.Code, response.Body.String())
	}
	var created struct {
		Device struct {
			ID int `json:"id"`
		} `json:"device"`
		Config string `json:"config"`
		QRCode string `json:"qr_code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(created.Config, "AllowedIPs = 10.13.13.1/32, 192.168.1.0/24") || !strings.HasPrefix(created.QRCode, "data:image/png;base64,") {
		t.Fatalf("configuration %+v", created)
	}

	response = f.do(http.MethodGet, "/pi-appliance/api/services/vpn/wireguard", "", "", cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"Téléphone"`) || strings.Contains(response.Body.String(), "PrivateKey") {
		t.Fatalf("liste : code %d : %s", response.Code, response.Body.String())
	}

	if response := f.do(http.MethodDelete, "/pi-appliance/api/services/vpn/wireguard/devices/2", "", "", cookie); response.Code != http.StatusNoContent {
		t.Fatalf("suppression : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/services/vpn/wireguard/devices/x", "", "", cookie); response.Code != http.StatusNotFound {
		t.Fatalf("appareil illisible : code %d", response.Code)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services/absent/wireguard", "", "", cookie); response.Code != http.StatusNotFound {
		t.Fatalf("service absent : code %d", response.Code)
	}
}
