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
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const password = "une phrase de passe"

func init() {
	Iterations = 1000
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func open(t *testing.T) (*Store, *clock) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := &clock{now: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	store.Now = c.Now
	return store, c
}

func withAdmin(t *testing.T) (*Store, *clock) {
	t.Helper()
	store, c := open(t)
	if _, err := store.CreateAdmin("Marie", "marie@example.org", password); err != nil {
		t.Fatal(err)
	}
	return store, c
}

func TestCreateAdmin(t *testing.T) {
	store, _ := open(t)
	if !store.SetupNeeded() {
		t.Fatal("aucun compte : création attendue")
	}
	account, err := store.CreateAdmin(" Marie ", "marie@example.org", password)
	if err != nil {
		t.Fatal(err)
	}
	if account.Name != "marie" || account.Role != RoleAdmin || account.EmailVerified || strings.Contains(account.Hash, password) {
		t.Fatalf("compte %+v", account)
	}
	if store.SetupNeeded() {
		t.Fatal("compte créé : plus de création attendue")
	}
	if _, err := store.CreateAdmin("pirate", "pirate@example.org", password); !errors.Is(err, ErrSetupDone) {
		t.Fatalf("second administrateur : %v", err)
	}
	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.SetupNeeded() {
		t.Fatal("compte perdu à la relecture")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("droits %v (%v)", info.Mode().Perm(), err)
		}
	}
}

func TestCreateAdminInvalid(t *testing.T) {
	cases := map[string][3]string{
		"mot de passe de 11 caractères": {"marie", "marie@example.org", "onze caract"},
		"e-mail":                        {"marie", "pas-un-email", password},
		"e-mail avec un nom":            {"marie", "Marie <marie@example.org>", password},
		"identifiant vide":              {"", "marie@example.org", password},
		"identifiant avec espace":       {"marie curie", "marie@example.org", password},
		"mot de passe trop long":        {"marie", "marie@example.org", strings.Repeat("a", 257)},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			store, _ := open(t)
			if _, err := store.CreateAdmin(c[0], c[1], c[2]); !errors.Is(err, ErrInvalid) {
				t.Fatalf("erreur %v", err)
			}
			if !store.SetupNeeded() {
				t.Fatal("compte créé malgré l'erreur")
			}
		})
	}
}

func TestLoginAndSession(t *testing.T) {
	store, c := withAdmin(t)
	if _, _, err := store.Login("marie", "mauvais mot de passe", "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("mauvais mot de passe : %v", err)
	}
	if _, _, err := store.Login("inconnu", password, "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("compte inconnu : %v", err)
	}
	token, account, err := store.Login("Marie", password, "", "10.0.0.2")
	if err != nil || account.Name != "marie" {
		t.Fatalf("connexion : %v %+v", err, account)
	}
	if got, ok := store.Session(token); !ok || got.Name != "marie" {
		t.Fatal("session introuvable")
	}
	c.now = c.now.Add(SessionLifetime - time.Hour)
	if _, ok := store.Session(token); !ok {
		t.Fatal("session expirée trop tôt")
	}
	c.now = c.now.Add(SessionLifetime - time.Hour)
	if _, ok := store.Session(token); !ok {
		t.Fatal("session non prolongée par l'usage")
	}
	store.Logout(token)
	if _, ok := store.Session(token); ok {
		t.Fatal("session encore valide après la déconnexion")
	}
	token = store.StartSession("marie")
	c.now = c.now.Add(SessionLifetime + time.Minute)
	if _, ok := store.Session(token); ok {
		t.Fatal("session valide après expiration")
	}
	if _, ok := store.Session(""); ok {
		t.Fatal("jeton vide accepté")
	}
}

func TestLoginLimit(t *testing.T) {
	store, c := withAdmin(t)
	for i := 0; i < freeAttempts; i++ {
		if _, _, err := store.Login("marie", "faux", "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
			t.Fatalf("essai %d : %v", i, err)
		}
	}
	_, _, err := store.Login("marie", password, "", "10.0.0.2")
	var tooMany TooManyError
	if !errors.As(err, &tooMany) || tooMany.Wait != time.Minute {
		t.Fatalf("après 5 échecs : %v", err)
	}
	if _, _, err := store.Login("marie", password, "", "10.0.0.3"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("compte bloqué depuis un autre client : %v", err)
	}
	c.now = c.now.Add(time.Minute)
	if _, _, err := store.Login("marie", "faux", "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("après l'attente : %v", err)
	}
	if _, _, err := store.Login("marie", password, "", "10.0.0.2"); !errors.As(err, &tooMany) || tooMany.Wait != 2*time.Minute {
		t.Fatalf("attente doublée attendue : %v", err)
	}
	c.now = c.now.Add(2 * time.Minute)
	if _, _, err := store.Login("marie", password, "", "10.0.0.2"); err != nil {
		t.Fatalf("bon mot de passe après l'attente : %v", err)
	}
	if _, _, err := store.Login("marie", "faux", "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("compteur non remis à zéro : %v", err)
	}
}

func TestLoginLimitCapped(t *testing.T) {
	store, c := withAdmin(t)
	for i := 0; i < 40; i++ {
		store.failures["account:marie"] = failure{count: freeAttempts + i, last: c.now}
		if wait := store.blocked("account:marie", c.now); wait > maxBlock || wait <= 0 {
			t.Fatalf("%d échecs : attente %s", freeAttempts+i, wait)
		}
	}
}

func TestResetFile(t *testing.T) {
	store, _ := withAdmin(t)
	token := store.StartSession("marie")
	reset := filepath.Join(t.TempDir(), "reset")
	if found, err := store.ConsumeResetFile(reset); found || err != nil {
		t.Fatalf("sans fichier : %v %v", found, err)
	}
	if _, err := store.ResetAdmin("nouvelle phrase de passe"); !errors.Is(err, ErrNoReset) {
		t.Fatalf("réinitialisation sans fichier : %v", err)
	}
	if err := os.WriteFile(reset, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := store.ConsumeResetFile(reset); !found || err != nil {
		t.Fatalf("avec fichier : %v %v", found, err)
	}
	if _, err := os.Stat(reset); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fichier reset non supprimé")
	}
	if !store.ResetPending() {
		t.Fatal("réinitialisation non ouverte")
	}
	if _, err := store.ResetAdmin("court"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mot de passe court : %v", err)
	}
	account, err := store.ResetAdmin("nouvelle phrase de passe")
	if err != nil || account.Name != "marie" || account.Email != "marie@example.org" {
		t.Fatalf("réinitialisation : %v %+v", err, account)
	}
	if store.ResetPending() {
		t.Fatal("réinitialisation encore ouverte")
	}
	if _, ok := store.Session(token); ok {
		t.Fatal("ancienne session encore valide")
	}
	if _, _, err := store.Login("marie", password, "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("ancien mot de passe accepté : %v", err)
	}
	if _, _, err := store.Login("marie", "nouvelle phrase de passe", "", "10.0.0.2"); err != nil {
		t.Fatalf("nouveau mot de passe : %v", err)
	}
}

func TestResetSurvivesRestart(t *testing.T) {
	store, _ := withAdmin(t)
	reset := filepath.Join(t.TempDir(), "reset")
	if err := os.WriteFile(reset, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := store.ConsumeResetFile(reset); !found || err != nil {
		t.Fatalf("avec fichier : %v %v", found, err)
	}
	restarted, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.ResetPending() {
		t.Fatal("réinitialisation perdue au redémarrage")
	}
	if found, err := restarted.ConsumeResetFile(reset); found || err != nil {
		t.Fatalf("fichier déjà supprimé : %v %v", found, err)
	}
	if !restarted.ResetPending() {
		t.Fatal("réinitialisation fermée sans fichier reset")
	}
	if _, err := restarted.ResetAdmin("nouvelle phrase de passe"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(restarted.pendingPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reset-pending non effacé")
	}
	again, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if again.ResetPending() {
		t.Fatal("réinitialisation rouverte après le nouveau mot de passe")
	}
	if _, _, err := again.Login("marie", "nouvelle phrase de passe", "", "10.0.0.2"); err != nil {
		t.Fatalf("nouveau mot de passe : %v", err)
	}
}

func TestResetFileWithoutAccount(t *testing.T) {
	store, _ := open(t)
	reset := filepath.Join(t.TempDir(), "reset")
	if err := os.WriteFile(reset, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := store.ConsumeResetFile(reset); found || err != nil {
		t.Fatalf("sans compte : %v %v", found, err)
	}
	if store.ResetPending() {
		t.Fatal("réinitialisation ouverte sans compte")
	}
	if _, err := os.Stat(reset); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fichier reset non supprimé")
	}
	if _, err := os.Stat(store.pendingPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reset-pending écrit sans compte")
	}
}

func TestHash(t *testing.T) {
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := hashPassword(password)
	if hash == other {
		t.Fatal("sel identique pour deux hachages")
	}
	if !checkHash(hash, password) || checkHash(hash, password+"x") {
		t.Fatal("vérification du hachage")
	}
	for _, bad := range []string{"", "md5$1$a$b", "pbkdf2-sha256$x$a$b", "pbkdf2-sha256$0$a$b", "pbkdf2-sha256$10$!!$b"} {
		if checkHash(bad, password) {
			t.Fatalf("hachage invalide accepté : %q", bad)
		}
	}
}
