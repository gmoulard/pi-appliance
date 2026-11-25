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

package backup

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/addresses"
)

// peerFixture : deux Pi, celui de la maison (fixture) et celui du proche
// (host), qui l'accueille derrière un serveur HTTPS de test.
type peerFixture struct {
	*fixture
	host       *fixture
	hostedRoot string
	server     *httptest.Server
}

func newPeerFixture(t *testing.T) *peerFixture {
	t.Helper()
	p := &peerFixture{fixture: newFixture(t), host: newFixture(t)}
	p.host.Host = "proche"
	p.hostedRoot = filepath.Join(t.TempDir(), "proches")
	p.host.Hosted = func() (string, error) { return p.hostedRoot, nil }
	mux := http.NewServeMux()
	p.host.Routes(mux, "/pi-appliance", func(next http.HandlerFunc) http.HandlerFunc { return next })
	p.server = httptest.NewTLSServer(mux)
	t.Cleanup(p.server.Close)
	key, err := addresses.LoadOrCreateKey(filepath.Join(t.TempDir(), "address-key"))
	if err != nil {
		t.Fatal(err)
	}
	p.PeerKey = key
	p.HTTP = p.server.Client()
	return p
}

func (p *peerFixture) pair(t *testing.T, quotaMB int64) {
	t.Helper()
	code, _, err := p.host.HostingCode("Paul", quotaMB)
	if err != nil {
		t.Fatal(err)
	}
	// Le code se saisit sans tiret ni majuscules.
	if err := p.PairPeer(context.Background(), p.server.URL, strings.ToLower(strings.ReplaceAll(code, "-", ""))); err != nil {
		t.Fatal(err)
	}
}

func TestPeerBackupAndRestore(t *testing.T) {
	defer func(size int) { onlinePartSize = size }(onlinePartSize)
	onlinePartSize = 1024
	p := newPeerFixture(t)
	p.install(t, "blog", "index.html", strings.Repeat("blog ", 2000))
	p.pair(t, 2048)
	ctx := context.Background()
	snapshot, err := p.Backup(ctx, Manual)
	if err != nil {
		t.Fatal(err)
	}
	status := p.host.hostingStatus()
	if len(status.Guests) != 1 || status.Guests[0].Name != "Paul" || status.Guests[0].QuotaMB != 2048 {
		t.Fatalf("proches accueillis : %+v", status)
	}
	// Le proche ne peut rien lire : ni données ni noms des services.
	guestDir := filepath.Join(p.hostedRoot, status.Guests[0].ID)
	if !exists(filepath.Join(guestDir, snapshot.ID, "services", "2.enc")) {
		t.Fatal("sauvegarde attendue chez le proche")
	}
	filepath.WalkDir(guestDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, _ := os.ReadFile(path)
		for _, secret := range []string{"photo v1", "SECRET=v1", "cloud", "blog"} {
			if bytes.Contains(data, []byte(secret)) || strings.Contains(path, secret) {
				t.Errorf("%s contient %q en clair", path, secret)
			}
		}
		return nil
	})
	// Disque de la maison perdu : restauration depuis le proche.
	p.Target = func() (string, error) { return "", ErrNoDisk }
	p.forget()
	snapshots, places, err := p.snapshotsByPlace(ctx)
	if err != nil || len(snapshots) != 1 || places[snapshot.ID][0] != PlacePeer {
		t.Fatalf("chez le proche : %+v %v %v", snapshots, places, err)
	}
	write(t, filepath.Join(p.volume("cloud"), "data", "photo.jpg"), "photo v2")
	if err := p.Restore(ctx, snapshot.ID, "cloud", ""); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(p.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("données : %q", got)
	}
	// Le proche arrête de l'accueillir : ses sauvegardes sont effacées, ce Pi n'est plus reconnu.
	if err := p.host.RemoveGuest(status.Guests[0].ID); err != nil {
		t.Fatal(err)
	}
	if exists(guestDir) {
		t.Fatal("sauvegardes du proche à effacer")
	}
	p.Target = func() (string, error) { return p.root, nil }
	p.forget()
	if _, err := p.Backup(ctx, Manual); err == nil || !strings.Contains(p.Last().Message, "chez votre proche") {
		t.Fatalf("proche disparu : %v %+v", err, p.Last())
	}
}

func TestPeerPairingAndQuota(t *testing.T) {
	p := newPeerFixture(t)
	ctx := context.Background()
	var refused *ErrPeer
	if err := p.PairPeer(ctx, p.server.URL, "AAAAA-AAAAA"); !errors.As(err, &refused) || refused.Code != "code" {
		t.Fatalf("sans code tiré : %v", err)
	}
	code, _, err := p.host.HostingCode("Paul", 1024)
	if err != nil {
		t.Fatal(err)
	}
	// 5 essais faux : le code est perdu, même juste ensuite.
	for i := 0; i < 5; i++ {
		p.PairPeer(ctx, p.server.URL, "AAAAA-AAAAA")
	}
	if err := p.PairPeer(ctx, p.server.URL, code); !errors.As(err, &refused) || refused.Code != "code" {
		t.Fatalf("code après 5 essais faux : %v", err)
	}
	for _, bad := range []string{"http://exemple.com", "https://exemple.com/chemin", ""} {
		if err := p.PairPeer(ctx, bad, "x"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q : %v", bad, err)
		}
	}
	if _, _, err := p.host.HostingCode("Paul", 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("espace trop petit : %v", err)
	}
	// Un code ne sert qu'une fois.
	code, _, _ = p.host.HostingCode("Paul", 1024)
	if err := p.PairPeer(ctx, p.server.URL, code); err != nil {
		t.Fatal(err)
	}
	if err := p.PairPeer(ctx, p.server.URL, code); !errors.As(err, &refused) {
		t.Fatalf("code resservi : %v", err)
	}
	// Une demande non signée, ou signée par un Pi inconnu, est refusée.
	response, err := p.server.Client().Get(p.server.URL + PeerPath + "/snapshots")
	if err != nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("non signée : %v %v", response.StatusCode, err)
	}
	stranger, _ := addresses.LoadOrCreateKey(filepath.Join(t.TempDir(), "k"))
	other := &peerStore{base: p.server.URL + PeerPath, key: stranger, http: p.server.Client(), now: p.Manager.now}
	if _, _, err := other.list(ctx); !errors.As(err, &refused) || refused.Code != "unknown" {
		t.Fatalf("Pi inconnu : %v", err)
	}
	// Espace réservé plein.
	guest := p.host.hostingStatus().Guests[0]
	if err := p.host.SetGuestQuota(guest.ID, 1024); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(p.hostedRoot, guest.ID), 0o700)
	big, _ := os.Create(filepath.Join(p.hostedRoot, guest.ID, "plein"))
	big.Truncate(1024 << 20)
	big.Close()
	// Fichier creux : dirSize compte la taille annoncée.
	w, _ := p.peerStore().create(ctx, "20261120-030000", configName)
	w.Write([]byte("x"))
	if err := w.Close(); !errors.As(err, &refused) || refused.Code != "full" {
		t.Fatalf("espace plein : %v", err)
	}
	if err := p.RemovePeer(); err != nil || p.peerStore() != nil {
		t.Fatalf("arrêt de l'envoi : %v", err)
	}
}
