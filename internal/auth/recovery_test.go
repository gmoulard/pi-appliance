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

package auth

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResetLink(t *testing.T) {
	store, c := withAdmin(t)
	session, _, err := store.Login("marie", password, "", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	links, err := store.RequestReset(" Marie@Example.org ", "10.0.0.2")
	if err != nil || len(links) != 1 || links[0].Account.Name != "marie" || links[0].Token == "" {
		t.Fatalf("par adresse e-mail : %+v, %v", links, err)
	}
	token := links[0].Token
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("le jeton est enregistré en clair")
	}
	// Le mot de passe actuel reste valable tant que le nouveau n'est pas choisi.
	if _, _, err := store.Login("marie", password, "", "10.0.0.2"); err != nil {
		t.Fatalf("ancien mot de passe avant le changement : %v", err)
	}
	if got, err := store.ResetLinkAccount(token); err != nil || got.Name != "marie" {
		t.Fatalf("lien : %+v, %v", got, err)
	}
	if _, err := store.UseResetLink(token, "court"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mot de passe trop court : %v", err)
	}
	if _, err := store.UseResetLink("faux", "un nouveau mot de passe"); !errors.Is(err, ErrNoResetLink) {
		t.Fatalf("faux jeton : %v", err)
	}
	if _, err := store.UseResetLink(token, "un nouveau mot de passe"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Session(session); ok {
		t.Fatal("les sessions du compte doivent être fermées")
	}
	if _, err := store.UseResetLink(token, "encore un autre mot"); !errors.Is(err, ErrNoResetLink) {
		t.Fatalf("lien utilisé deux fois : %v", err)
	}
	if _, _, err := store.Login("marie", password, "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("ancien mot de passe : %v", err)
	}
	if _, _, err := store.Login("marie", "un nouveau mot de passe", "", "10.0.0.2"); err != nil {
		t.Fatalf("nouveau mot de passe : %v", err)
	}
	// Un lien expire après une heure.
	links, err = store.RequestReset("marie", "10.0.0.3")
	if err != nil || len(links) != 1 {
		t.Fatalf("par identifiant : %+v, %v", links, err)
	}
	c.now = c.now.Add(ResetLinkLifetime)
	if _, err := store.UseResetLink(links[0].Token, "un nouveau mot de passe"); !errors.Is(err, ErrNoResetLink) {
		t.Fatalf("lien expiré : %v", err)
	}
}

func TestResetLinkIgnoredAccounts(t *testing.T) {
	store, c := withAdmin(t)
	if _, _, err := store.Create("paul", "paul@example.org", RoleUser, nil); err != nil {
		t.Fatal(err)
	}
	end := c.now.Add(time.Hour)
	_, token, err := store.Create("ines", "ines@example.org", RoleGuest, &end)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptInvitation(token, "huit car"); err != nil {
		t.Fatal(err)
	}
	c.now = end
	for _, who := range []string{"inconnu", "paul", "paul@example.org", "ines"} {
		links, err := store.RequestReset(who, "10.0.0.2")
		if err != nil || len(links) != 0 {
			t.Fatalf("%s : %+v, %v (inconnu, invitation pas acceptée ou invité expiré : rien)", who, links, err)
		}
	}
	if _, err := store.RequestReset("  ", "10.0.0.2"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("vide : %v", err)
	}
}

func TestResetLinkLimits(t *testing.T) {
	store, c := withAdmin(t)
	for i := range resetRequestsPerAccount {
		if _, err := store.RequestReset("marie", "10.0.0.2"); err != nil {
			t.Fatalf("demande %d : %v", i+1, err)
		}
	}
	var tooMany TooManyError
	if _, err := store.RequestReset("marie", "10.0.0.9"); !errors.As(err, &tooMany) {
		t.Fatalf("trop de demandes pour un compte : %v", err)
	}
	// Même limite pour un compte qui n'existe pas : rien n'est révélé.
	for range resetRequestsPerAccount {
		store.RequestReset("inconnu", "10.0.0.3")
	}
	if _, err := store.RequestReset("inconnu", "10.0.0.4"); !errors.As(err, &tooMany) {
		t.Fatalf("trop de demandes pour un compte inconnu : %v", err)
	}
	for i := range resetRequestsPerClient {
		store.RequestReset("x"+string(rune('a'+i)), "10.0.0.5")
	}
	if _, err := store.RequestReset("autre", "10.0.0.5"); !errors.As(err, &tooMany) {
		t.Fatalf("trop de demandes d'un client : %v", err)
	}
	c.now = c.now.Add(resetRequestWindow)
	if links, err := store.RequestReset("marie", "10.0.0.5"); err != nil || len(links) != 1 {
		t.Fatalf("une heure plus tard : %+v, %v", links, err)
	}
}

func TestResetLinkKeepsTOTP(t *testing.T) {
	store, c := withAdmin(t)
	secret, _ := enableTOTP(t, store, c)
	links, err := store.RequestReset("marie", "10.0.0.2")
	if err != nil || len(links) != 1 {
		t.Fatalf("%+v, %v", links, err)
	}
	account, err := store.UseResetLink(links[0].Token, "un nouveau mot de passe")
	if err != nil || account.TOTPSecret == "" {
		t.Fatalf("la double authentification doit rester : %+v, %v", account, err)
	}
	if _, _, err := store.Login("marie", "un nouveau mot de passe", "", "10.0.0.2"); !errors.Is(err, ErrCodeNeeded) {
		t.Fatalf("code demandé : %v", err)
	}
	c.now = c.now.Add(time.Minute)
	if _, _, err := store.Login("marie", "un nouveau mot de passe", code(t, secret, c.now), "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordChangeCancelsResetLink(t *testing.T) {
	store, _ := withAdmin(t)
	links, err := store.RequestReset("marie", "10.0.0.2")
	if err != nil || len(links) != 1 {
		t.Fatalf("%+v, %v", links, err)
	}
	if _, _, err := store.ChangePassword("marie", password, "un autre mot de passe"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UseResetLink(links[0].Token, "un nouveau mot de passe"); !errors.Is(err, ErrNoResetLink) {
		t.Fatalf("lien après changement du mot de passe : %v", err)
	}
}
