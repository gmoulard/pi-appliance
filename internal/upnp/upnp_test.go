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

package upnp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeBox : une box UPnP-IGD en mémoire (description et actions SOAP).
type fakeBox struct {
	mu        sync.Mutex
	mappings  map[string]string // "443/TCP" -> adresse interne
	leases    map[string]string
	permanent bool // n'accepte que les baux sans fin (code 725)
	refuse    bool // UPnP en lecture seule (code 606)
	actions   []string
	server    *httptest.Server
}

const description = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
<device><friendlyName>Box de test</friendlyName>
<deviceList><device><deviceList><device>
<serviceList><service>
<serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
<controlURL>/ctl/IPConn</controlURL>
</service></serviceList>
</device></deviceList></device></deviceList>
</device></root>`

func newFakeBox(t *testing.T) *fakeBox {
	box := &fakeBox{mappings: map[string]string{}, leases: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rootDesc.xml", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, description)
	})
	mux.HandleFunc("POST /ctl/IPConn", box.control)
	box.server = httptest.NewServer(mux)
	t.Cleanup(box.server.Close)
	return box
}

func (b *fakeBox) control(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	action := r.Header.Get("SOAPAction")
	action = strings.Trim(action[strings.Index(action, "#")+1:], `"`)
	values := xmlValues(data, "NewExternalPort", "NewProtocol", "NewInternalClient", "NewLeaseDuration")
	key := values["NewExternalPort"] + "/" + values["NewProtocol"]
	b.mu.Lock()
	defer b.mu.Unlock()
	b.actions = append(b.actions, action+" "+key)
	fail := func(code int) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>test</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, code)
	}
	reply := func(inner string) {
		fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">%s</u:%sResponse></s:Body></s:Envelope>`, action, inner, action)
	}
	switch action {
	case "GetExternalIPAddress":
		reply("<NewExternalIPAddress>203.0.113.7</NewExternalIPAddress>")
	case "AddPortMapping":
		if b.refuse {
			fail(CodeNotAuthorized)
			return
		}
		if b.permanent && values["NewLeaseDuration"] != "0" {
			fail(CodeOnlyPermanent)
			return
		}
		if owner, ok := b.mappings[key]; ok && owner != values["NewInternalClient"] {
			fail(CodeConflict)
			return
		}
		b.mappings[key] = values["NewInternalClient"]
		b.leases[key] = values["NewLeaseDuration"]
		reply("")
	case "DeletePortMapping":
		if _, ok := b.mappings[key]; !ok {
			fail(CodeNoSuchEntry)
			return
		}
		delete(b.mappings, key)
		reply("")
	case "GetSpecificPortMappingEntry":
		owner, ok := b.mappings[key]
		if !ok {
			fail(CodeNoSuchEntry)
			return
		}
		reply("<NewInternalClient>" + owner + "</NewInternalClient>")
	default:
		fail(401)
	}
}

// fakeSSDP répond aux recherches par l'adresse de la description de la box.
func fakeSSDP(t *testing.T, location string) string {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buffer := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			request := string(buffer[:n])
			if !strings.HasPrefix(request, "M-SEARCH") || !strings.Contains(request, "InternetGatewayDevice") {
				continue
			}
			// Un appareil de la maison qui renvoie ailleurs est ignoré.
			conn.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nST: upnp:rootdevice\r\nLOCATION: http://198.51.100.1/evil.xml\r\n\r\n"), from)
			conn.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=120\r\nST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\nLOCATION: "+location+"\r\n\r\n"), from)
		}
	}()
	return conn.LocalAddr().String()
}

func discover(t *testing.T, box *fakeBox) *Gateway {
	t.Helper()
	address := fakeSSDP(t, box.server.URL+"/rootDesc.xml")
	gateway, err := Discoverer{Address: address, Timeout: 2 * time.Second}.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func TestDiscoverAndSOAP(t *testing.T) {
	box := newFakeBox(t)
	gateway := discover(t, box)
	if gateway.Name != "Box de test" || gateway.LocalIP != "127.0.0.1" || !strings.HasSuffix(gateway.ControlURL, "/ctl/IPConn") {
		t.Fatalf("box : %+v", gateway)
	}
	ip, err := gateway.ExternalIP(context.Background())
	if err != nil || ip != "203.0.113.7" {
		t.Fatalf("adresse publique : %q, %v", ip, err)
	}
	port := Mapping{Port: 443, Protocol: "TCP"}
	if err := gateway.Add(context.Background(), port, time.Hour, Description); err != nil {
		t.Fatal(err)
	}
	if box.leases["443/TCP"] != "3600" || box.mappings["443/TCP"] != "127.0.0.1" {
		t.Fatalf("ouverture : %v %v", box.mappings, box.leases)
	}
	if owner, err := gateway.Owner(context.Background(), port); err != nil || owner != "127.0.0.1" {
		t.Fatalf("propriétaire : %q, %v", owner, err)
	}
	if err := gateway.Delete(context.Background(), port); err != nil {
		t.Fatal(err)
	}
	err = gateway.Delete(context.Background(), port)
	if !isCode(err, CodeNoSuchEntry) {
		t.Fatalf("fermeture d'un port fermé : %v", err)
	}
}

func TestNoGateway(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = Discoverer{Address: conn.LocalAddr().String(), Timeout: 300 * time.Millisecond}.Discover(context.Background())
	if !errors.Is(err, ErrNoGateway) {
		t.Fatalf("sans box : %v", err)
	}
	if !strings.Contains(Explain(err), "activez l'UPnP") {
		t.Fatal(Explain(err))
	}
}

func TestSameHost(t *testing.T) {
	for location, want := range map[string]bool{
		"http://192.168.1.1:5000/rootDesc.xml": true,
		"http://192.168.1.2/rootDesc.xml":      false,
		"https://192.168.1.1/rootDesc.xml":     false,
		"http://box.lan/rootDesc.xml":          false,
	} {
		if got := sameHost(location, net.IPv4(192, 168, 1, 1)); got != want {
			t.Errorf("%s : %v", location, got)
		}
	}
	if sameHost("http://203.0.113.1/x.xml", net.ParseIP("203.0.113.1")) {
		t.Error("adresse publique acceptée")
	}
}

func newManager(t *testing.T, box *fakeBox, ports *[]Mapping) *Manager {
	gateway := discover(t, box)
	path := filepath.Join(t.TempDir(), "upnp.json")
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	m.Ports = func() []Mapping { return *ports }
	m.Find = func(context.Context) (*Gateway, error) { return gateway, nil }
	return m
}

func TestManagerLifecycle(t *testing.T) {
	box := newFakeBox(t)
	ports := []Mapping{{80, "TCP"}, {443, "TCP"}}
	m := newManager(t, box, &ports)
	// Sans accord, rien n'est ouvert.
	m.Sync(context.Background())
	if len(box.mappings) != 0 {
		t.Fatalf("ouvert sans accord : %v", box.mappings)
	}
	status, err := m.Enable(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.ExternalIP != "203.0.113.7" || len(status.Ports) != 2 || !status.Ports[0].Open || !status.Ports[1].Open || status.Error != "" {
		t.Fatalf("état : %+v", status)
	}
	// WireGuard installé : 51820/UDP ajouté ; puis retiré : refermé.
	ports = append(ports, Mapping{51820, "UDP"})
	m.Sync(context.Background())
	if box.mappings["51820/UDP"] != "127.0.0.1" {
		t.Fatalf("WireGuard : %v", box.mappings)
	}
	ports = ports[:2]
	m.Sync(context.Background())
	if _, ok := box.mappings["51820/UDP"]; ok {
		t.Fatal("port de WireGuard resté ouvert")
	}
	// L'accord est gardé dans le fichier.
	again, err := Open(m.Path)
	if err != nil || !again.Status().Enabled || len(again.settings.Opened) != 2 {
		t.Fatalf("relu : %+v, %v", again.settings, err)
	}
	info, _ := os.Stat(m.Path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("droits : %v", info.Mode())
	}
	// Retrait de l'accord : tout est refermé.
	status, err = m.Disable(context.Background(), "admin")
	if err != nil || status.Enabled || len(box.mappings) != 0 {
		t.Fatalf("retrait : %+v, %v, %v", status, err, box.mappings)
	}
}

func TestManagerPermanentAndConflict(t *testing.T) {
	box := newFakeBox(t)
	box.permanent = true
	box.mappings["80/TCP"] = "192.168.1.50"
	ports := []Mapping{{80, "TCP"}, {443, "TCP"}}
	m := newManager(t, box, &ports)
	status, _ := m.Enable(context.Background(), "admin")
	if box.leases["443/TCP"] != "0" || !status.Ports[1].Open {
		t.Fatalf("bail sans fin : %v %+v", box.leases, status)
	}
	if status.Ports[0].Open || !strings.Contains(status.Ports[0].Error, "192.168.1.50") || status.Error == "" {
		t.Fatalf("conflit : %+v", status)
	}
	if box.mappings["80/TCP"] != "192.168.1.50" {
		t.Fatal("redirection d'un autre appareil retirée")
	}
	// Ancienne adresse du Pi : la redirection est refaite vers la nouvelle.
	m.settings.LocalIP = "192.168.1.50"
	m.Sync(context.Background())
	if box.mappings["80/TCP"] != "127.0.0.1" {
		t.Fatalf("ancienne adresse du Pi : %v", box.mappings)
	}
}

func TestManagerRefused(t *testing.T) {
	box := newFakeBox(t)
	box.refuse = true
	ports := []Mapping{{443, "TCP"}}
	m := newManager(t, box, &ports)
	status, _ := m.Enable(context.Background(), "admin")
	if status.Ports[0].Open || !strings.Contains(status.Ports[0].Error, "activez l'UPnP") {
		t.Fatalf("refus : %+v", status)
	}
	m.Find = func(context.Context) (*Gateway, error) { return nil, ErrNoGateway }
	if next := m.Sync(context.Background()); next != retryDelay {
		t.Fatalf("nouvel essai : %v", next)
	}
	if status := m.Status(); !strings.Contains(status.Error, "ne répond pas") {
		t.Fatalf("sans box : %+v", status)
	}
}
