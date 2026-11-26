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

package server

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/tunnel"
)

const zone = "pi-appliance.test"

type fakeNames map[string]string // nom -> clé

func (f fakeNames) Owner(name string) (string, bool) { key, ok := f[name]; return key, ok }
func (f fakeNames) NameOf(key string) (string, bool) {
	for name, k := range f {
		if k == key {
			return name, true
		}
	}
	return "", false
}

// certificate : certificat de test pour names, et le pool qui le reconnaît.
func certificate(t *testing.T, names ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: names[0]}, DNSNames: names,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, pool
}

func key(t *testing.T) *addresses.Key {
	t.Helper()
	k, err := addresses.LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

type relay struct {
	server       *Server
	https, http  string
	relayPool    *x509.CertPool
	visitorsCert tls.Certificate
	visitorsPool *x509.CertPool
}

func startRelay(t *testing.T, names fakeNames, max int) *relay {
	t.Helper()
	cert, pool := certificate(t, "relais."+zone)
	places, err := OpenPlaces(filepath.Join(t.TempDir(), "places.json"), max)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Zone: zone, RelayName: "relais." + zone, TLS: &tls.Config{Certificates: []tls.Certificate{cert}},
		Names: names, Places: places, RateBits: tunnel.RateBits, PoolLimit: 4, MaxVisits: 8, AnswerTimeout: 3 * time.Second}
	https, plain := listen(t), listen(t)
	go s.ServeHTTPS(https)
	go s.ServeHTTP(plain)
	visitorsCert, visitorsPool := certificate(t, "monpi."+zone)
	return &relay{server: s, https: https.Addr().String(), http: plain.Addr().String(), relayPool: pool, visitorsCert: visitorsCert, visitorsPool: visitorsPool}
}

// pi : appliance de test ; ses « Traefik » 80 et 443 répondent « bonjour ».
type pi struct {
	client *tunnel.Client
	mu     sync.Mutex
	visits []tunnel.Visit
}

func (p *pi) seen() []tunnel.Visit {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]tunnel.Visit(nil), p.visits...)
}

func startPi(t *testing.T, r *relay, k *addresses.Key) *pi {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, "bonjour") })
	web, secure := listen(t), listen(t)
	go http.Serve(web, handler)
	tlsServer := &http.Server{Handler: handler, TLSConfig: &tls.Config{Certificates: []tls.Certificate{r.visitorsCert}}}
	go tlsServer.ServeTLS(secure, "", "")
	p := &pi{}
	p.client = &tunnel.Client{Relay: r.https, Key: k, Pool: 2, Retry: 100 * time.Millisecond,
		TLS: &tls.Config{ServerName: "relais." + zone, RootCAs: r.relayPool},
		Target: func(ctx context.Context, v tunnel.Visit) (net.Conn, error) {
			p.mu.Lock()
			p.visits = append(p.visits, v)
			p.mu.Unlock()
			if v.Port == 80 {
				return net.Dial("tcp", web.Addr().String())
			}
			return net.Dial("tcp", secure.Addr().String())
		}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.client.Run(ctx)
	return p
}

func waitState(t *testing.T, c *tunnel.Client, state string) tunnel.Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.Status(); s.State == state {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("état %q attendu, %+v", state, c.Status())
	return tunnel.Status{}
}

func get(t *testing.T, address, host string) (int, string) {
	t.Helper()
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	io.WriteString(conn, "GET / HTTP/1.1\r\nHost: "+host+"\r\nConnection: close\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return 0, err.Error()
	}
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

func TestRelayHTTPAndHTTPS(t *testing.T) {
	k := key(t)
	r := startRelay(t, fakeNames{"monpi": k.Public(), "autre": "clé"}, 1)
	p := startPi(t, r, k)
	waitState(t, p.client, tunnel.StateReady)

	if code, body := get(t, r.http, "monpi."+zone); code != 200 || body != "bonjour" {
		t.Fatalf("HTTP : %d %q", code, body)
	}
	// HTTPS : le certificat est celui du Pi, le relais ne déchiffre rien.
	visitor := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: r.visitorsPool},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", r.https)
		}}}
	response, err := visitor.Get("https://monpi." + zone + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "bonjour" {
		t.Fatalf("HTTPS : %q", body)
	}
	visits := p.seen()
	if len(visits) != 2 || visits[0].Port != 80 || visits[1].Port != 443 || visits[0].From.Addr() != netip.MustParseAddr("127.0.0.1") {
		t.Fatalf("visites : %+v", visits)
	}
	// Octets comptés pour le quota du mois.
	r.server.mu.Lock()
	counted := r.server.Places.data.Places[k.Public()].Bytes
	r.server.mu.Unlock()
	if counted == 0 {
		t.Fatal("octets non comptés")
	}
	// Un nom réservé mais pas relayé : 503 en HTTP.
	if code, _ := get(t, r.http, "autre."+zone); code != 503 {
		t.Fatalf("nom non relayé : %d", code)
	}
	// Rendre la place : plus de visite.
	if err := p.client.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.server.mu.Lock()
	_, kept := r.server.Places.data.Places[k.Public()]
	r.server.mu.Unlock()
	if kept {
		t.Fatal("place gardée après DELETE")
	}
}

func TestRelayWaitingListAndRefusal(t *testing.T) {
	first, second, stranger := key(t), key(t), key(t)
	r := startRelay(t, fakeNames{"un": first.Public(), "deux": second.Public()}, 1)
	a := startPi(t, r, first)
	waitState(t, a.client, tunnel.StateReady)
	b := startPi(t, r, second)
	if s := waitState(t, b.client, tunnel.StateWaiting); s.Position != 1 || !strings.Contains(s.Message, "liste d'attente") {
		t.Fatalf("attente : %+v", s)
	}
	c := startPi(t, r, stranger)
	if s := waitState(t, c.client, tunnel.StateRefused); !strings.Contains(s.Message, "Réservez") {
		t.Fatalf("sans adresse : %+v", s)
	}
	// La première rend sa place : la suivante l'obtient.
	if err := a.client.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitState(t, b.client, tunnel.StateReady)
}

func TestRelayFallback(t *testing.T) {
	backend := listen(t)
	go http.Serve(backend, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { io.WriteString(w, "projet") }))
	r := startRelay(t, fakeNames{}, 1)
	r.server.FallbackHTTP = backend.Addr().String()
	if code, body := get(t, r.http, "adresses."+zone); code != 200 || body != "projet" {
		t.Fatalf("renvoi : %d %q", code, body)
	}
}

func TestPlaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "places.json")
	if _, err := OpenPlaces(path, 0); err == nil {
		t.Fatal("maximum absent accepté")
	}
	p, _ := OpenPlaces(path, 1)
	p.MonthlyBytes = 100
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	if s := p.Admit("a", "un", now); s.State != tunnel.StateReady {
		t.Fatalf("a : %+v", s)
	}
	if s := p.Admit("b", "deux", now); s.State != tunnel.StateWaiting || s.Position != 1 {
		t.Fatalf("b : %+v", s)
	}
	if s := p.Admit("c", "trois", now); s.State != tunnel.StateWaiting || s.Position != 2 {
		t.Fatalf("c : %+v", s)
	}
	// Quota du mois.
	if !p.Count("a", 60, now) || p.Count("a", 60, now) {
		t.Fatal("quota mal compté")
	}
	if s := p.Admit("a", "un", now); s.State != tunnel.StateQuota || p.Has("a", now) {
		t.Fatalf("quota : %+v", s)
	}
	next := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if s := p.Admit("a", "un", next); s.State != tunnel.StateReady {
		t.Fatalf("mois suivant : %+v", s)
	}
	if err := p.Save(); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %v", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "127.0.0.1") {
		t.Fatal("adresse IP écrite")
	}
	// Relu : même liste d'attente.
	again, err := OpenPlaces(path, 1)
	if err != nil || len(again.data.Waiting) != 2 {
		t.Fatalf("relecture : %v %+v", err, again.data)
	}
	// Muets des mois durant : personne ne perd sa place ni son rang.
	later := next.AddDate(1, 0, 0)
	if s := again.Admit("c", "trois", later); s.State != tunnel.StateWaiting || s.Position != 2 {
		t.Fatalf("rang perdu : %+v", s)
	}
	if !again.Has("a", later) {
		t.Fatal("place rendue d'office")
	}
	// « a » rend sa place : « b » la prend, « c » passe premier.
	again.Release("a", later)
	if s := again.Admit("b", "deux", later); s.State != tunnel.StateReady {
		t.Fatalf("b après départ de a : %+v", s)
	}
	if s := again.Admit("c", "trois", later); s.State != tunnel.StateWaiting || s.Position != 1 {
		t.Fatalf("c après départ de a : %+v", s)
	}
}

func TestSniffSNI(t *testing.T) {
	server, client := net.Pipe()
	go tls.Client(client, &tls.Config{ServerName: "Monpi." + zone}).Handshake()
	name, seen, err := sniffSNI(server)
	if err != nil || name != "monpi."+zone || len(seen) == 0 {
		t.Fatalf("%q %v", name, err)
	}
	client.Close()
}

func TestApplianceName(t *testing.T) {
	s := &Server{Zone: zone, RelayName: "relais." + zone}
	for host, want := range map[string]string{"monpi." + zone: "monpi", "x.monpi." + zone: "monpi", "relais." + zone: "", zone: "", "monpi.example.org": "", "-x." + zone: ""} {
		if got, _ := s.applianceName(host); got != want {
			t.Errorf("%s : %q, attendu %q", host, got, want)
		}
	}
}
