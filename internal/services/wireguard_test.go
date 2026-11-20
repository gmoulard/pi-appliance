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

package services

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

func wireGuardApp(t *testing.T) marketplace.App {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get(WireGuardApp)
	if !ok {
		t.Fatal("wireguard absent du catalogue")
	}
	return app
}

func wireGuardManager(t *testing.T, runner *fakeRunner) *Manager {
	m := manager(t, runner)
	m.HomeNetworks = func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")} }
	return m
}

func TestInstallWireGuard(t *testing.T) {
	m := wireGuardManager(t, &fakeRunner{})
	result, err := m.Install(context.Background(), wireGuardApp(t), "vpn", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Instance.NoWebPage || len(result.Instance.HostPorts) != 1 {
		t.Fatalf("instance %+v", result.Instance)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "port UDP 51820") {
		t.Fatalf("redirection de port non signalée : %v", result.Warnings)
	}
	dir := m.serviceDir("vpn")
	var override map[string]any
	data, _ := os.ReadFile(filepath.Join(dir, overrideName))
	if err := json.Unmarshal(data, &override); err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "traefik") || !strings.Contains(text, `"51820:51820/udp"`) || !strings.Contains(text, filepath.Join(dir, wireGuardDir)) {
		t.Fatalf("override %s", text)
	}
	conf, err := os.ReadFile(filepath.Join(dir, wireGuardDir, wireGuardConf))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ListenPort = 51820", "Address = 10.13.13.1/24", "-d 192.168.1.0/24 -j ACCEPT", "FORWARD -i %i -j DROP"} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("wg0.conf sans %q :\n%s", want, conf)
		}
	}

	// Le port UDP ne peut servir qu'à une seule installation.
	if _, err := m.Install(context.Background(), wireGuardApp(t), "vpn2", nil); !errors.Is(err, ErrExists) {
		t.Fatalf("second VPN : %v", err)
	}
}

func TestWireGuardDevices(t *testing.T) {
	runner := &fakeRunner{}
	m := wireGuardManager(t, runner)
	if _, err := m.Install(context.Background(), wireGuardApp(t), "vpn", nil); err != nil {
		t.Fatal(err)
	}
	created, err := m.AddDevice(context.Background(), "vpn", "Téléphone de Camille")
	if err != nil {
		t.Fatal(err)
	}
	if created.Device.Address != "10.13.13.2" || created.Device.ID != 2 || !runner.called("restart") {
		t.Fatalf("appareil %+v", created.Device)
	}
	for _, want := range []string{"Endpoint = maison.example.org:51820", "AllowedIPs = 10.13.13.1/32, 192.168.1.0/24", "Address = 10.13.13.2/32", "PresharedKey = "} {
		if !strings.Contains(created.Config, want) {
			t.Errorf("configuration sans %q :\n%s", want, created.Config)
		}
	}
	if strings.Contains(created.Config, "0.0.0.0/0") {
		t.Error("tout le trafic passerait par la maison")
	}
	png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(created.QRCode, "data:image/png;base64,"))
	if err != nil || !strings.HasPrefix(string(png), "\x89PNG") {
		t.Fatalf("QR code illisible : %v", err)
	}

	dir := m.serviceDir("vpn")
	conf, _ := os.ReadFile(filepath.Join(dir, wireGuardDir, wireGuardConf))
	if !strings.Contains(string(conf), "PublicKey = "+created.Device.PublicKey) || !strings.Contains(string(conf), "AllowedIPs = 10.13.13.2/32") {
		t.Fatalf("wg0.conf sans l'appareil :\n%s", conf)
	}
	// La clé privée de l'appareil n'est pas gardée sur le Pi.
	private := strings.TrimSpace(strings.Split(strings.Split(created.Config, "PrivateKey = ")[1], "\n")[0])
	state, _ := os.ReadFile(filepath.Join(dir, wireGuardName))
	if strings.Contains(string(state), private) || strings.Contains(string(conf), private) {
		t.Fatal("clé privée de l'appareil gardée sur le Pi")
	}

	if _, err := m.AddDevice(context.Background(), "vpn", "téléphone de camille"); !errors.Is(err, ErrExists) {
		t.Fatalf("nom en double : %v", err)
	}
	if _, err := m.AddDevice(context.Background(), "vpn", "a\nb"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nom invalide : %v", err)
	}
	second, err := m.AddDevice(context.Background(), "vpn", "Tablette")
	if err != nil || second.Device.Address != "10.13.13.3" {
		t.Fatalf("second appareil %+v, %v", second.Device, err)
	}
	status, err := m.WireGuard("vpn")
	if err != nil || len(status.Devices) != 2 || status.Endpoint != "maison.example.org" || status.EndpointLocal || status.HomeNetworks[0] != "192.168.1.0/24" {
		t.Fatalf("état %+v, %v", status, err)
	}
	if strings.Contains(mustJSON(t, status), "preshared") {
		t.Fatal("clé partagée renvoyée par l'API")
	}

	if err := m.RemoveDevice(context.Background(), "vpn", 2); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveDevice(context.Background(), "vpn", 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("appareil déjà retiré : %v", err)
	}
	conf, _ = os.ReadFile(filepath.Join(dir, wireGuardDir, wireGuardConf))
	if strings.Contains(string(conf), created.Device.PublicKey) {
		t.Fatal("appareil retiré encore autorisé")
	}
	// Une place libérée est reprise.
	third, err := m.AddDevice(context.Background(), "vpn", "Ordinateur")
	if err != nil || third.Device.Address != "10.13.13.2" {
		t.Fatalf("troisième appareil %+v, %v", third.Device, err)
	}
}

func TestWireGuardRestartFailureRestores(t *testing.T) {
	runner := &fakeRunner{}
	m := wireGuardManager(t, runner)
	if _, err := m.Install(context.Background(), wireGuardApp(t), "vpn", nil); err != nil {
		t.Fatal(err)
	}
	runner.fail = map[string]bool{"restart": true}
	if _, err := m.AddDevice(context.Background(), "vpn", "Téléphone"); err == nil {
		t.Fatal("échec du redémarrage non signalé")
	}
	status, err := m.WireGuard("vpn")
	if err != nil || len(status.Devices) != 0 {
		t.Fatalf("appareil gardé malgré l'échec : %+v, %v", status, err)
	}
}

func TestWireGuardOtherService(t *testing.T) {
	m := wireGuardManager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.WireGuard("cloud"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Nextcloud pris pour un VPN : %v", err)
	}
	if _, err := m.WireGuard("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service absent : %v", err)
	}
}

func TestWireGuardNoHomeNetwork(t *testing.T) {
	m := wireGuardManager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), wireGuardApp(t), "vpn", nil); err != nil {
		t.Fatal(err)
	}
	m.HomeNetworks = func() []netip.Prefix { return nil }
	if _, err := m.AddDevice(context.Background(), "vpn", "Téléphone"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sans réseau de la maison : %v", err)
	}
}

// Les clés suivent le format de « wg genkey » et « wg pubkey ».
func TestWireGuardKeys(t *testing.T) {
	// Vecteur de la RFC 7748 (section 6.1), clé privée d'Alice.
	private := base64.StdEncoding.EncodeToString(mustHex(t, "77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a"))
	public, err := publicKey(private)
	if err != nil {
		t.Fatal(err)
	}
	if want := base64.StdEncoding.EncodeToString(mustHex(t, "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")); public != want {
		t.Fatalf("clé publique %s, attendu %s", public, want)
	}
	key, err := wireGuardKey()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := base64.StdEncoding.DecodeString(key)
	if len(raw) != 32 || raw[0]&7 != 0 || raw[31]&128 != 0 || raw[31]&64 == 0 {
		t.Fatalf("clé non bornée : %x", raw)
	}
}

func TestHomePrefix(t *testing.T) {
	for address, want := range map[string]string{
		"192.168.1.196/24": "192.168.1.0/24",
		"10.0.0.5/16":      "10.0.0.0/16",
		"10.0.0.5/8":       "", // contient le réseau du VPN
		"10.42.0.1/24":     "", // Wi-Fi de configuration
		"172.18.0.1/16":    "", // Docker
		"203.0.113.4/24":   "", // adresse publique
		"192.168.1.7/32":   "",
	} {
		ip, network, err := net.ParseCIDR(address)
		if err != nil {
			t.Fatal(err)
		}
		network.IP = ip
		prefix, ok := homePrefix(network)
		if got := map[bool]string{true: prefix.String(), false: ""}[ok]; got != want {
			t.Errorf("%s : %q, attendu %q", address, got, want)
		}
	}
}

func mustHex(t *testing.T, text string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
