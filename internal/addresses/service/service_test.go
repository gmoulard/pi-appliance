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

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
)

// fakeDNS garde les enregistrements A en mémoire (le faux OVH est dans ovh_test.go).
type fakeDNS struct {
	mu      sync.Mutex
	records map[string]string
	fail    bool
}

func (f *fakeDNS) Set(_ context.Context, hostname, ip string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("panne")
	}
	f.records[hostname] = ip
	f.records["*."+hostname] = ip
	return nil
}

func (f *fakeDNS) Remove(_ context.Context, hostname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errors.New("panne")
	}
	delete(f.records, hostname)
	delete(f.records, "*."+hostname)
	return nil
}

func (f *fakeDNS) get(hostname string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.records[hostname]
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fixture struct {
	dns     *fakeDNS
	clock   *clock
	service *Service
	server  *httptest.Server
	dir     string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	dns := &fakeDNS{records: map[string]string{}}
	service, err := Open(filepath.Join(dir, "registry.json"), dns)
	if err != nil {
		t.Fatal(err)
	}
	now := &clock{now: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)}
	service.Now = now.Now
	service.BehindProxy = true
	server := httptest.NewServer(service.Handler())
	t.Cleanup(server.Close)
	return &fixture{dns: dns, clock: now, service: service, server: server, dir: dir}
}

// appliance : console d'une appliance, avec sa clé, sa connexion vue par le
// reverse proxy (ip) et l'adresse IP publique de sa box (public).
func (f *fixture) appliance(t *testing.T, name, ip, public string) *addresses.Client {
	t.Helper()
	key, err := addresses.LoadOrCreateKey(filepath.Join(f.dir, name, "address-key"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := addresses.OpenClient(filepath.Join(f.dir, name, "address.json"), key)
	if err != nil {
		t.Fatal(err)
	}
	client.URL = f.server.URL
	client.Now = f.clock.Now
	client.HTTP = ipDoer{ip: ip}
	client.PublicIP = func(context.Context) (string, error) { return public, nil }
	return client
}

// ipDoer pose X-Real-IP comme le reverse proxy du Pi du projet.
type ipDoer struct{ ip string }

func (d ipDoer) Do(r *http.Request) (*http.Response, error) {
	r.Header.Set("X-Real-IP", d.ip)
	return http.DefaultClient.Do(r)
}

func refusal(t *testing.T, err error, status int, text string) {
	t.Helper()
	var refused *addresses.ServiceError
	if !errors.As(err, &refused) || refused.Status != status || !strings.Contains(refused.Message, text) {
		t.Fatalf("erreur %v, attendu %d « %s »", err, status, text)
	}
}

func TestReserveUpdateRelease(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.appliance(t, "alice", "203.0.113.1", "90.1.1.1")
	bob := f.appliance(t, "bob", "203.0.113.2", "90.1.1.3")

	if a, err := alice.Check(ctx, "maison"); err != nil || !a.Available || a.Hostname != "maison.pi-appliance.com" {
		t.Fatalf("libre : %+v %v", a, err)
	}
	if a, err := alice.Check(ctx, "www"); err != nil || a.Available || a.Reason != "ce nom est réservé au projet" {
		t.Fatalf("www : %+v %v", a, err)
	}
	state, err := alice.Reserve(ctx, "Maison")
	if err != nil || state.Hostname != "maison.pi-appliance.com" || state.IP != "90.1.1.1" {
		t.Fatalf("réservation : %+v %v", state, err)
	}
	if f.dns.get("maison.pi-appliance.com") != "90.1.1.1" || f.dns.get("*.maison.pi-appliance.com") != "90.1.1.1" {
		t.Fatalf("alias : %v", f.dns.records)
	}
	if a, _ := bob.Check(ctx, "maison"); a.Available || a.Reason != "ce nom est déjà pris" {
		t.Fatalf("pris : %+v", a)
	}
	_, err = bob.Reserve(ctx, "maison")
	refusal(t, err, http.StatusConflict, "déjà pris")
	_, err = alice.Reserve(ctx, "autre")
	refusal(t, err, http.StatusConflict, "déjà l'adresse maison.pi-appliance.com")

	// Le registre ne garde pas l'adresse de la connexion, seulement l'adresse IP écrite dans la zone.
	data, err := os.ReadFile(filepath.Join(f.dir, "registry.json"))
	if err != nil || strings.Contains(string(data), "203.0.113") || !strings.Contains(string(data), `"ip": "90.1.1.1"`) {
		t.Fatalf("registre : %s %v", data, err)
	}

	// Nouvelle adresse IP de la box : l'enregistrement A est remplacé.
	alice.PublicIP = func(context.Context) (string, error) { return "90.1.1.2", nil }
	if err := alice.Sync(ctx); err != nil || alice.Status().IP != "90.1.1.2" || f.dns.get("maison.pi-appliance.com") != "90.1.1.2" {
		t.Fatalf("mise à jour : %+v %v", alice.Status(), err)
	}

	// Une autre appliance ne peut ni changer ni libérer l'adresse.
	request, _ := http.NewRequest(http.MethodDelete, f.server.URL+"/api/names/maison", nil)
	other, _ := addresses.LoadOrCreateKey(filepath.Join(f.dir, "bob", "address-key"))
	other.Sign(request, nil, f.clock.Now())
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("libération par une autre : %v %v", response, err)
	}
	response.Body.Close()

	// Une fois libéré, le nom attend 30 jours avant d'être repris par une autre appliance.
	if err := alice.Release(ctx); err != nil || alice.Status().Name != "" || f.dns.get("maison.pi-appliance.com") != "" {
		t.Fatalf("libération : %+v %v", alice.Status(), err)
	}
	_, err = bob.Reserve(ctx, "maison")
	refusal(t, err, http.StatusConflict, "pas libre pour l'instant")
	f.clock.add(addresses.Cooldown)
	f.service.Expire(ctx)
	if _, err := bob.Reserve(ctx, "maison"); err != nil {
		t.Fatalf("reprise après 30 jours : %v", err)
	}
}

func TestRecoverAndSignature(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.appliance(t, "alice", "203.0.113.1", "90.1.1.1")
	if _, err := alice.Reserve(ctx, "maison"); err != nil {
		t.Fatal(err)
	}
	// Fichier d'état perdu, clé gardée : l'adresse revient du service.
	os.Remove(filepath.Join(f.dir, "alice", "address.json"))
	again := f.appliance(t, "alice", "203.0.113.1", "90.1.1.1")
	if err := again.Recover(ctx); err != nil || again.Status().Name != "maison" {
		t.Fatalf("reprise : %+v %v", again.Status(), err)
	}
	// Horloge du Pi décalée : la demande est refusée.
	again.Now = func() time.Time { return f.clock.Now().Add(time.Hour) }
	refusal(t, again.Sync(ctx), http.StatusUnauthorized, "date et l'heure")
	// Corps modifié après la signature.
	request, _ := http.NewRequest(http.MethodPut, f.server.URL+"/api/names/maison", strings.NewReader(`{"ip":"90.6.6.6"}`))
	key, _ := addresses.LoadOrCreateKey(filepath.Join(f.dir, "alice", "address-key"))
	key.Sign(request, []byte(`{"ip":"90.1.1.1"}`), f.clock.Now())
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("corps modifié : %v %v", response, err)
	}
	response.Body.Close()
}

func TestExpiry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.appliance(t, "alice", "203.0.113.1", "90.1.1.1")
	if _, err := alice.Reserve(ctx, "maison"); err != nil {
		t.Fatal(err)
	}
	f.clock.add(addresses.Expiry - time.Hour)
	f.service.Expire(ctx)
	if f.dns.get("maison.pi-appliance.com") == "" {
		t.Fatal("libérée trop tôt")
	}
	f.clock.add(time.Hour)
	f.service.Expire(ctx)
	if f.dns.get("maison.pi-appliance.com") != "" {
		t.Fatal("pas libérée après 90 jours")
	}
	// La console l'apprend au signe de vie suivant.
	if err := alice.Sync(ctx); err == nil || alice.Status().Name != "" || !strings.Contains(alice.Status().Error, "libérée") {
		t.Fatalf("console : %+v %v", alice.Status(), err)
	}
}

func TestLimits(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := range addresses.ReservationsPerIP {
		client := f.appliance(t, "pi"+string(rune('a'+i)), "198.51.100.7", "90.2.0.1")
		if _, err := client.Reserve(ctx, "maison-"+string(rune('a'+i))); err != nil {
			t.Fatalf("réservation %d : %v", i, err)
		}
	}
	extra := f.appliance(t, "piz", "198.51.100.7", "90.2.0.1")
	_, err := extra.Reserve(ctx, "maison-z")
	refusal(t, err, http.StatusTooManyRequests, "depuis votre connexion")
	f.clock.add(24 * time.Hour)
	if _, err := extra.Reserve(ctx, "maison-z"); err != nil {
		t.Fatalf("le lendemain : %v", err)
	}

	// Changements d'adresse IP limités par jour ; le signe de vie, lui, passe toujours.
	for i := range addresses.IPChangesPerDay {
		extra.PublicIP = func(context.Context) (string, error) { return "90.2.0." + strconv.Itoa(i+10), nil }
		if err := extra.Sync(ctx); err != nil {
			t.Fatalf("changement %d : %v", i, err)
		}
	}
	if err := extra.Sync(ctx); err != nil {
		t.Fatalf("signe de vie : %v", err)
	}
	extra.PublicIP = func(context.Context) (string, error) { return "90.2.1.1", nil }
	refusal(t, extra.Sync(ctx), http.StatusTooManyRequests, "trop souvent")

	// Questions « libre ? » limitées par minute et par connexion.
	var last addresses.Availability
	for range addresses.ChecksPerMinute + 1 {
		request, _ := http.NewRequest(http.MethodGet, f.server.URL+"/api/names/test-nom", nil)
		request.Header.Set("X-Real-IP", "192.0.2.9")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode == http.StatusTooManyRequests {
			response.Body.Close()
			return
		}
		json.NewDecoder(response.Body).Decode(&last)
		response.Body.Close()
	}
	t.Fatalf("pas de limite aux questions : %+v", last)
}

func TestDNSFailure(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.dns.fail = true
	alice := f.appliance(t, "alice", "203.0.113.1", "90.1.1.1")
	_, err := alice.Reserve(ctx, "maison")
	refusal(t, err, http.StatusServiceUnavailable, "réessayez plus tard")
	f.dns.fail = false
	if a, _ := alice.Check(ctx, "maison"); !a.Available {
		t.Fatal("nom pris malgré l'échec")
	}
	if _, err := alice.Reserve(ctx, "maison"); err != nil {
		t.Fatal(err)
	}
}

func TestIPMustBePublic(t *testing.T) {
	f := newFixture(t)
	key, err := addresses.LoadOrCreateKey(filepath.Join(f.dir, "pirate", "address-key"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"192.168.1.20", "127.0.0.1", "2001:db8::1", "pirate.example.org", ""} {
		body := []byte(`{"name":"maison","ip":"` + ip + `"}`)
		request, _ := http.NewRequest(http.MethodPost, f.server.URL+"/api/names", strings.NewReader(string(body)))
		key.Sign(request, body, f.clock.Now())
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		var refused addresses.ErrorBody
		json.NewDecoder(response.Body).Decode(&refused)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(refused.Error, "adresse IP publique") {
			t.Errorf("%q : %d %+v", ip, response.StatusCode, refused)
		}
	}
	if f.dns.get("maison.pi-appliance.com") != "" {
		t.Fatal("adresse écrite malgré le refus")
	}
	// Adresse IP de la box illisible côté console : rien n'est demandé au service.
	alice := f.appliance(t, "alice", "203.0.113.1", "")
	alice.PublicIP = func(context.Context) (string, error) { return "", errors.New("hors ligne") }
	if _, err := alice.Reserve(context.Background(), "maison"); !errors.Is(err, addresses.ErrNoIP) {
		t.Fatalf("sans adresse IP : %v", err)
	}
}
