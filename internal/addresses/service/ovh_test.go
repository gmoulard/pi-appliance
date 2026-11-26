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
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeOVH : zone en mémoire derrière l'API OVH, qui vérifie chaque signature.
type fakeOVH struct {
	t        *testing.T
	creds    Credentials
	server   *httptest.Server
	clock    int64 // heure d'OVH
	mu       sync.Mutex
	next     int64
	records  map[int64]ovhRecord
	refresh  int
	timeHits int
}

func newFakeOVH(t *testing.T) *fakeOVH {
	f := &fakeOVH{t: t, creds: Credentials{ApplicationKey: "ak", ApplicationSecret: "as", ConsumerKey: "ck"}, clock: 1_800_000_000, next: 1, records: map[int64]ovhRecord{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOVH) add(fieldType, sub, target string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[f.next] = ovhRecord{ID: f.next, FieldType: fieldType, SubDomain: sub, Target: target, TTL: 3600}
	f.next++
}

// find : enregistrements de ce sous-domaine, par type.
func (f *fakeOVH) find(sub string) map[string][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	found := map[string][]string{}
	for _, r := range f.records {
		if r.SubDomain == sub {
			found[r.FieldType] = append(found[r.FieldType], r.Target)
		}
	}
	return found
}

func (f *fakeOVH) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/1.0/auth/time" {
		f.timeHits++
		io.WriteString(w, strconv.FormatInt(f.clock, 10))
		return
	}
	body, _ := io.ReadAll(r.Body)
	timestamp, _ := strconv.ParseInt(r.Header.Get("X-Ovh-Timestamp"), 10, 64)
	want := ovhSignature(f.creds.ApplicationSecret, f.creds.ConsumerKey, r.Method, f.server.URL+r.URL.RequestURI(), string(body), timestamp)
	if r.Header.Get("X-Ovh-Application") != "ak" || r.Header.Get("X-Ovh-Consumer") != "ck" || r.Header.Get("X-Ovh-Signature") != want || timestamp < f.clock-5 || timestamp > f.clock+5 {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"message":"Invalid signature"}`)
		return
	}
	const base = "/1.0/domain/zone/pi-appliance.com/"
	path, found := strings.CutPrefix(r.URL.Path, base)
	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch {
	case r.Method == http.MethodPost && path == "refresh":
		f.refresh++
	case r.Method == http.MethodGet && path == "record":
		ids := []int64{}
		for id, record := range f.records {
			if record.FieldType == r.URL.Query().Get("fieldType") && record.SubDomain == r.URL.Query().Get("subDomain") {
				ids = append(ids, id)
			}
		}
		json.NewEncoder(w).Encode(ids)
	case r.Method == http.MethodPost && path == "record":
		var record ovhRecord
		json.Unmarshal(body, &record)
		record.ID = f.next
		f.records[f.next] = record
		f.next++
		json.NewEncoder(w).Encode(record)
	case strings.HasPrefix(path, "record/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(path, "record/"), 10, 64)
		record, ok := f.records[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"not found"}`)
			return
		}
		switch r.Method {
		case http.MethodPut:
			var change ovhRecord
			json.Unmarshal(body, &change)
			record.Target, record.TTL = change.Target, change.TTL
			f.records[id] = record
		case http.MethodDelete:
			delete(f.records, id)
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"message":"unexpected"}`)
	}
}

func TestOVHSignature(t *testing.T) {
	// Forme donnée par OVH : "$1$" + SHA-1 de AS+CK+méthode+URL+corps+horodatage, séparés par « + ».
	sum := sha1.Sum([]byte("as+ck+POST+https://eu.api.ovh.com/1.0/domain/zone/pi-appliance.com/refresh++1800000000"))
	want := "$1$" + hex.EncodeToString(sum[:])
	if got := ovhSignature("as", "ck", "POST", "https://eu.api.ovh.com/1.0/domain/zone/pi-appliance.com/refresh", "", 1800000000); got != want {
		t.Fatalf("signature %q, attendu %q", got, want)
	}
}

func TestOVHSetReplaceRemove(t *testing.T) {
	fake := newFakeOVH(t)
	// Ancien alias vers DuckDNS, venu de Route 53, et une autre adresse à ne pas toucher.
	fake.add("CNAME", "maison", "alice.duckdns.org.")
	fake.add("A", "voisin", "90.9.9.9")
	ovh := &OVH{Zone: "pi-appliance.com", Credentials: fake.creds, Endpoint: fake.server.URL + "/1.0", Now: func() time.Time { return time.Unix(1_700_000_000, 0) }}
	ctx := context.Background()

	if err := ovh.Set(ctx, "maison.pi-appliance.com", "90.1.1.1"); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"maison", "*.maison"} {
		if got := fake.find(sub); len(got) != 1 || len(got["A"]) != 1 || got["A"][0] != "90.1.1.1" {
			t.Fatalf("%s : %v", sub, got)
		}
	}
	// L'horloge du Pi est décalée de 100 000 000 s : l'heure d'OVH sert, lue une fois.
	if fake.timeHits != 1 || fake.refresh != 1 {
		t.Fatalf("heure lue %d fois, zone rafraîchie %d fois", fake.timeHits, fake.refresh)
	}

	// Nouvelle adresse : l'enregistrement est remplacé, pas doublé.
	if err := ovh.Set(ctx, "maison.pi-appliance.com", "90.1.1.2"); err != nil {
		t.Fatal(err)
	}
	if got := fake.find("maison"); len(got["A"]) != 1 || got["A"][0] != "90.1.1.2" {
		t.Fatalf("remplacé : %v", got)
	}

	// Libération : les deux enregistrements sont retirés ; une deuxième fois, pas d'erreur.
	if err := ovh.Remove(ctx, "maison.pi-appliance.com"); err != nil {
		t.Fatal(err)
	}
	if err := ovh.Remove(ctx, "maison.pi-appliance.com"); err != nil {
		t.Fatal(err)
	}
	if len(fake.find("maison")) != 0 || len(fake.find("*.maison")) != 0 || len(fake.find("voisin")["A"]) != 1 {
		t.Fatalf("après libération : %v", fake.records)
	}

	// Mauvaises clés : refus d'OVH remonté.
	ovh.Credentials.ApplicationSecret = "faux"
	err := ovh.Set(ctx, "maison.pi-appliance.com", "90.1.1.1")
	if refused, ok := err.(*OVHError); !ok || refused.Status != http.StatusForbidden || strings.Contains(err.Error(), "faux") {
		t.Fatalf("refus : %v", err)
	}
	if err := ovh.Set(ctx, "autre.example.org", "90.1.1.1"); err == nil {
		t.Fatal("nom hors de la zone accepté")
	}
}

func TestLoadCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ovh")
	os.WriteFile(path, []byte("# clés OVH\nOVH_Application_key=A\nOVH_Application_secret = \"B\"\nOVH_Consumer_Key=C\n"), 0o600)
	if c, err := LoadCredentials(path); err != nil || c.ApplicationKey != "A" || c.ApplicationSecret != "B" || c.ConsumerKey != "C" {
		t.Fatalf("%+v %v", c, err)
	}
	os.WriteFile(path, []byte("OVH_Application_key=A\n"), 0o600)
	if _, err := LoadCredentials(path); err == nil {
		t.Fatal("clés incomplètes acceptées")
	}
}
