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

package addresses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// putCounter : faux service d'adresses qui compte les envois de l'adresse IP.
type putCounter struct {
	mu  sync.Mutex
	ips []string
}

func (p *putCounter) Do(r *http.Request) (*http.Response, error) {
	var request Request
	json.NewDecoder(r.Body).Decode(&request)
	p.mu.Lock()
	p.ips = append(p.ips, request.IP)
	p.mu.Unlock()
	data, _ := json.Marshal(Record{Name: "maison", Hostname: "maison.pi-appliance.com", IP: request.IP})
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
}

func (p *putCounter) sent() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.ips...)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 200 {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(what)
}

func TestRunSendsOnStartChangeAndWake(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "address.json"), []byte(`{"name":"maison","hostname":"maison.pi-appliance.com","ip":"90.1.1.1","target":"alice.duckdns.org"}`), 0o600)
	key, err := LoadOrCreateKey(filepath.Join(dir, "address-key"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := OpenClient(filepath.Join(dir, "address.json"), key)
	if err != nil || client.Status().Name != "maison" {
		t.Fatalf("état d'avant gardé : %+v %v", client.Status(), err)
	}
	service := &putCounter{}
	client.HTTP = service
	var mu sync.Mutex
	public := "90.1.1.1"
	client.PublicIP = func(context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return public, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go client.Run(ctx, 5*time.Millisecond)

	// Démarrage de la console : un envoi, puis rien tant que l'adresse ne change pas.
	waitFor(t, "pas d'envoi au démarrage", func() bool { return len(service.sent()) == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := service.sent(); len(got) != 1 {
		t.Fatalf("envois sans changement : %v", got)
	}
	// Adresse IP de la box changée : envoyée au tour suivant.
	mu.Lock()
	public = "90.1.1.2"
	mu.Unlock()
	waitFor(t, "nouvelle adresse non envoyée", func() bool { return len(service.sent()) == 2 && client.Status().IP == "90.1.1.2" })
	// Chargement de la console : un envoi, un seul par minute.
	client.WakeUp()
	waitFor(t, "pas d'envoi au chargement", func() bool { return len(service.sent()) == 3 })
	client.WakeUp()
	time.Sleep(50 * time.Millisecond)
	if got := service.sent(); len(got) != 3 || got[2] != "90.1.1.2" {
		t.Fatalf("envois : %v", got)
	}
}

func TestNextDaily(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		paris = time.FixedZone("CET", 3600)
	}
	for _, c := range []struct {
		now, want time.Time
		random    time.Duration
	}{
		// Avant 5 h : le jour même.
		{time.Date(2026, 11, 26, 3, 0, 0, 0, paris), time.Date(2026, 11, 26, 6, 30, 0, 0, paris), 90 * time.Minute},
		// Après l'heure tirée : le lendemain.
		{time.Date(2026, 11, 26, 7, 0, 0, 0, paris), time.Date(2026, 11, 27, 5, 10, 0, 0, paris), 10 * time.Minute},
		{time.Date(2026, 11, 26, 22, 0, 0, 0, paris), time.Date(2026, 11, 27, 7, 59, 0, 0, paris), 179 * time.Minute},
	} {
		got := NextDaily(c.now, func(n time.Duration) time.Duration {
			if n != 3*time.Hour {
				t.Fatalf("tirage sur %v", n)
			}
			return c.random
		})
		if !got.Equal(c.want) {
			t.Errorf("après %v : %v, attendu %v", c.now, got, c.want)
		}
	}
}

func TestLookupPublicIP(t *testing.T) {
	answer := func(status int, body string) Doer {
		return doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
	}
	if ip, err := LookupPublicIP(context.Background(), answer(http.StatusOK, "90.12.34.56\n"), ""); err != nil || ip != "90.12.34.56" {
		t.Fatalf("%q %v", ip, err)
	}
	if _, err := LookupPublicIP(context.Background(), answer(http.StatusOK, "<html>"), ""); err == nil {
		t.Fatal("réponse illisible acceptée")
	}
	if _, err := LookupPublicIP(context.Background(), answer(http.StatusBadGateway, "90.12.34.56"), ""); err == nil {
		t.Fatal("erreur acceptée")
	}
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }
