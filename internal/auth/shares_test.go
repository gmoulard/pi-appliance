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

package auth

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestShareLifecycle(t *testing.T) {
	store, c := withAdmin(t)
	share, token, err := store.CreateShare("marie", "pad", "notes", 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || share.Pad != "notes" || share.Service != "pad" || share.Owner != "marie" || !share.ExpiresAt.Equal(c.now.Add(7*24*time.Hour)) {
		t.Fatalf("lien %+v", share)
	}
	data, err := os.ReadFile(store.sharesPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("le jeton du lien est enregistré en clair")
	}
	if got, ok := store.Share(token, "pad"); !ok || got.ID != share.ID {
		t.Fatalf("lien valable refusé : %+v %v", got, ok)
	}
	for _, c := range []struct{ token, service string }{{token, "pad2"}, {token + "x", "pad"}, {"", "pad"}, {token, ""}} {
		if _, ok := store.Share(c.token, c.service); ok {
			t.Fatalf("lien accepté : %q pour %q", c.token, c.service)
		}
	}
	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Now = c.Now
	if _, ok := reopened.Share(token, "pad"); !ok {
		t.Fatal("lien perdu au redémarrage de la console")
	}
	c.now = c.now.Add(7 * 24 * time.Hour)
	if _, ok := store.Share(token, "pad"); ok {
		t.Fatal("lien expiré accepté")
	}
	if list := store.Shares("pad", Account{Name: "marie", Role: RoleAdmin}); len(list) != 0 {
		t.Fatalf("lien expiré listé : %+v", list)
	}
}

func TestShareLimits(t *testing.T) {
	store, _ := withAdmin(t)
	for _, c := range []struct {
		owner, service, pad string
		lifetime            time.Duration
	}{
		{"marie", "pad", "notes", 0},
		{"marie", "pad", "notes", ShareMaxLifetime + time.Second},
		{"marie", "", "notes", time.Hour},
		{"marie", "pad", "", time.Hour},
	} {
		if _, _, err := store.CreateShare(c.owner, c.service, c.pad, c.lifetime); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v : %v", c, err)
		}
	}
	if _, _, err := store.CreateShare("inconnu", "pad", "notes", time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("compte inconnu : %v", err)
	}
	if _, _, err := store.CreateShare("marie", "pad", "notes", ShareMaxLifetime); err != nil {
		t.Fatal(err)
	}
}

// Un lien vit tant que le compte qui l'a créé peut ouvrir les services.
func TestShareFollowsOwner(t *testing.T) {
	store, c := withAdmin(t)
	end := c.now.Add(48 * time.Hour)
	if _, _, err := store.Create("paul", "paul@example.org", RoleGuest, &end); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreateShare("paul", "pad", "notes", time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invité sans mot de passe : %v", err)
	}
	store.mu.Lock()
	paul := store.accounts["paul"]
	paul.Hash = "x"
	store.accounts["paul"] = paul
	store.mu.Unlock()
	share, token, err := store.CreateShare("paul", "pad", "notes", 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !share.ExpiresAt.Equal(end) {
		t.Fatalf("lien d'un invité au-delà de son compte : %v", share.ExpiresAt)
	}
	if err := store.RevokeShare("pad", share.ID, Account{Name: "autre", Role: RoleUser}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retiré par un autre compte : %v", err)
	}
	if list := store.Shares("pad", Account{Name: "autre", Role: RoleUser}); len(list) != 0 {
		t.Fatalf("lien d'un autre compte listé : %+v", list)
	}
	if list := store.Shares("pad", Account{Name: "marie", Role: RoleAdmin}); len(list) != 1 {
		t.Fatalf("l'administrateur voit tous les liens : %+v", list)
	}
	if err := store.Delete("paul"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Share(token, "pad"); ok {
		t.Fatal("lien d'un compte supprimé accepté")
	}
}

func TestRevokeShares(t *testing.T) {
	store, _ := withAdmin(t)
	admin := Account{Name: "marie", Role: RoleAdmin}
	first, token, err := store.CreateShare("marie", "pad", "notes", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeShare("pad2", first.ID, admin); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retiré par le mauvais service : %v", err)
	}
	if err := store.RevokeShare("pad", first.ID, admin); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Share(token, "pad"); ok {
		t.Fatal("lien retiré accepté")
	}
	_, other, _ := store.CreateShare("marie", "pad", "a", time.Hour)
	_, kept, _ := store.CreateShare("marie", "pad2", "b", time.Hour)
	if err := store.RevokeServiceShares("pad"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Share(other, "pad"); ok {
		t.Fatal("lien d'un service désinstallé accepté")
	}
	if _, ok := store.Share(kept, "pad2"); !ok {
		t.Fatal("lien d'un autre service retiré")
	}
	if err := store.FactoryReset(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.sharesPath()); !os.IsNotExist(err) {
		t.Fatalf("liens gardés après la remise à zéro : %v", err)
	}
}
