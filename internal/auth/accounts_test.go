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

func TestCreateAndAcceptInvitation(t *testing.T) {
	store, c := withAdmin(t)
	account, token, err := store.Create(" Paul ", "paul@example.org", RoleUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	if account.Name != "paul" || account.Hash != "" || !account.Public().Invited || token == "" {
		t.Fatalf("compte %+v, jeton %q", account, token)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) {
		t.Fatal("le jeton d'invitation est enregistré en clair")
	}
	if _, _, err := store.Create("paul", "autre@example.org", RoleUser, nil); !errors.Is(err, ErrExists) {
		t.Fatalf("doublon : %v", err)
	}
	if _, _, err := store.Login("paul", "", "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("invitation pas encore acceptée : %v", err)
	}
	if got, err := store.Invitation(token); err != nil || got.Name != "paul" {
		t.Fatalf("invitation : %+v, %v", got, err)
	}
	if _, err := store.AcceptInvitation(token, "court"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mot de passe trop court : %v", err)
	}
	if _, err := store.AcceptInvitation(token, "huit car"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptInvitation(token, "huit car"); !errors.Is(err, ErrNoInvitation) {
		t.Fatalf("lien utilisé deux fois : %v", err)
	}
	if _, _, err := store.Login("paul", "huit car", "", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}

	// Lien expiré.
	_, token, err = store.Create("lea", "lea@example.org", RoleUser, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(InvitationLifetime)
	if _, err := store.AcceptInvitation(token, "huit car"); !errors.Is(err, ErrNoInvitation) {
		t.Fatalf("invitation expirée : %v", err)
	}
	_, fresh, err := store.Invite("lea")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptInvitation(fresh, "huit car"); err != nil {
		t.Fatal(err)
	}
}

func TestInviteResetsPasswordAndClosesSessions(t *testing.T) {
	store, _ := withAdmin(t)
	_, token, _ := store.Create("paul", "paul@example.org", RoleUser, nil)
	if _, err := store.AcceptInvitation(token, "ancien mot"); err != nil {
		t.Fatal(err)
	}
	session, _, err := store.Login("paul", "ancien mot", "", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	_, token, err = store.Invite("paul")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Session(session); !ok {
		t.Fatal("le mot de passe actuel reste valable tant que le nouveau n'est pas choisi")
	}
	if _, err := store.AcceptInvitation(token, "nouveau mot"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Session(session); ok {
		t.Fatal("mot de passe changé : les sessions doivent être fermées")
	}
	if _, _, err := store.Login("paul", "ancien mot", "", "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("ancien mot de passe : %v", err)
	}
}

func TestGuestExpires(t *testing.T) {
	store, c := withAdmin(t)
	if _, _, err := store.Create("invite", "invite@example.org", RoleGuest, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invité sans date de fin : %v", err)
	}
	past := c.now.Add(-time.Hour)
	if _, _, err := store.Create("invite", "invite@example.org", RoleGuest, &past); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invité déjà expiré : %v", err)
	}
	if _, _, err := store.Create("x", "x@example.org", Role("root"), nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("rôle inconnu : %v", err)
	}
	end := c.now.Add(48 * time.Hour)
	_, token, err := store.Create("invite", "invite@example.org", RoleGuest, &end)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptInvitation(token, "huit car"); err != nil {
		t.Fatal(err)
	}
	session, _, err := store.Login("invite", "huit car", "", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	forward := forwardSession(t, store, session, "wiki-maison.local")
	c.now = end
	if _, ok := store.Session(session); ok {
		t.Fatal("invité expiré : session refusée")
	}
	if _, ok := store.ForwardSession(forward, "wiki-maison.local"); ok {
		t.Fatal("invité expiré : session déléguée refusée")
	}
	if _, _, err := store.Login("invite", "huit car", "", "10.0.0.2"); !errors.Is(err, ErrExpired) {
		t.Fatalf("connexion d'un invité expiré : %v", err)
	}
	later := c.now.Add(24 * time.Hour)
	if _, err := store.Update("invite", RoleGuest, &later, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Login("invite", "huit car", "", "10.0.0.2"); err != nil {
		t.Fatalf("invité prolongé : %v", err)
	}
	account, err := store.Update("invite", RoleUser, &later, false)
	if err != nil || account.ExpiresAt != nil {
		t.Fatalf("devenu utilisateur : %+v, %v", account, err)
	}
}

func TestLastAdmin(t *testing.T) {
	store, _ := withAdmin(t)
	if _, err := store.Update("marie", RoleUser, nil, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("rétrograder le dernier administrateur : %v", err)
	}
	if err := store.Delete("marie"); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("supprimer le dernier administrateur : %v", err)
	}
	// Un administrateur invité qui n'a pas encore de mot de passe ne compte pas.
	_, token, err := store.Create("jean", "jean@example.org", RoleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("marie"); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("seul administrateur avec mot de passe : %v", err)
	}
	if _, err := store.AcceptInvitation(token, "onze lettre"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("12 caractères pour un administrateur : %v", err)
	}
	if _, err := store.AcceptInvitation(token, password); err != nil {
		t.Fatal(err)
	}
	session, _, err := store.Login("marie", password, "", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("marie"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Session(session); ok {
		t.Fatal("compte supprimé : session refusée")
	}
	if err := store.Delete("marie"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("compte déjà supprimé : %v", err)
	}
	if list := store.List(); len(list) != 1 || list[0].Name != "jean" {
		t.Fatalf("comptes : %+v", list)
	}
}

// forwardSession : session déléguée pour host, ouverte par un code de la console.
func forwardSession(t *testing.T, store *Store, console, host string) string {
	t.Helper()
	code, ok := store.StartLoginCode(console, host, "etat")
	if !ok {
		t.Fatal("code de connexion refusé")
	}
	forward, ok := store.UseLoginCode(code, host, "etat")
	if !ok {
		t.Fatal("code de connexion non échangé")
	}
	return forward
}

func TestForwardSessionIsSeparate(t *testing.T) {
	store, _ := withAdmin(t)
	console, _, err := store.Login("marie", password, "", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	const host = "wiki-maison.local"
	forward := forwardSession(t, store, console, host)
	if _, ok := store.Session(forward); ok {
		t.Fatal("le cookie délégué, vu par les services, ne doit pas ouvrir la console")
	}
	if _, ok := store.ForwardSession(console, host); ok {
		t.Fatal("une session de la console n'est pas une session déléguée")
	}
	if account, ok := store.ForwardSession(forward, host); !ok || account.Name != "marie" {
		t.Fatalf("session déléguée : %+v, %v", account, ok)
	}
	if _, ok := store.ForwardSession(forward, "pad-maison.local"); ok {
		t.Fatal("une session déléguée ne vaut que pour son adresse")
	}
	store.Logout(forward)
	if _, ok := store.ForwardSession(forward, host); ok {
		t.Fatal("session déléguée fermée")
	}
	other := forwardSession(t, store, console, host)
	store.Logout(console)
	if _, ok := store.ForwardSession(other, host); ok {
		t.Fatal("la déconnexion de la console ferme ses sessions déléguées")
	}
}

func TestLoginCode(t *testing.T) {
	store, c := withAdmin(t)
	console, _, err := store.Login("marie", password, "", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	const host = "wiki-maison.local"
	if _, ok := store.StartLoginCode("inconnu", host, "etat"); ok {
		t.Fatal("code sans session de la console")
	}
	if _, ok := store.StartLoginCode(console, host, ""); ok {
		t.Fatal("code sans cookie du navigateur")
	}
	for _, test := range []struct{ name, host, state string }{
		{"autre adresse", "pad-maison.local", "etat"},
		{"autre navigateur", host, "autre"},
	} {
		code, _ := store.StartLoginCode(console, host, "etat")
		if _, ok := store.UseLoginCode(code, test.host, test.state); ok {
			t.Fatalf("%s : code accepté", test.name)
		}
		if _, ok := store.UseLoginCode(code, host, "etat"); ok {
			t.Fatalf("%s : code refusé une fois, accepté ensuite", test.name)
		}
	}
	code, _ := store.StartLoginCode(console, host, "etat")
	if _, ok := store.UseLoginCode(code, host, "etat"); !ok {
		t.Fatal("code refusé")
	}
	if _, ok := store.UseLoginCode(code, host, "etat"); ok {
		t.Fatal("code utilisé deux fois")
	}
	code, _ = store.StartLoginCode(console, host, "etat")
	c.now = c.now.Add(LoginCodeLifetime + time.Second)
	if _, ok := store.UseLoginCode(code, host, "etat"); ok {
		t.Fatal("code expiré accepté")
	}
	code, _ = store.StartLoginCode(console, host, "etat")
	store.Logout(console)
	if _, ok := store.UseLoginCode(code, host, "etat"); ok {
		t.Fatal("code accepté après la déconnexion de la console")
	}
}

func TestAccountsSurviveRestart(t *testing.T) {
	store, c := withAdmin(t)
	end := c.now.Add(time.Hour)
	if _, _, err := store.Create("invite", "invite@example.org", RoleGuest, &end); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	list := reopened.List()
	if len(list) != 2 || list[0].Name != "invite" || list[0].ExpiresAt == nil || !list[0].ExpiresAt.Equal(end) || !list[0].Invited {
		t.Fatalf("comptes relus : %+v", list)
	}
}

func TestChangePassword(t *testing.T) {
	store, _ := withAdmin(t)
	old, _, err := store.Login("marie", password, "", "client")
	if err != nil {
		t.Fatal(err)
	}
	next := "une toute nouvelle phrase"
	if _, _, err := store.ChangePassword("marie", "faux mot de passe", next); !errors.Is(err, ErrCredentials) {
		t.Fatalf("mauvais mot de passe actuel accepté : %v", err)
	}
	if _, _, err := store.ChangePassword("marie", password, "court"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mot de passe trop court accepté : %v", err)
	}
	token, _, err := store.ChangePassword("marie", password, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Session(old); ok {
		t.Fatal("l'ancienne session reste ouverte")
	}
	if _, ok := store.Session(token); !ok {
		t.Fatal("la nouvelle session n'est pas ouverte")
	}
	if _, _, err := store.Login("marie", password, "", "client"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("l'ancien mot de passe marche encore : %v", err)
	}
	if _, _, err := store.Login("marie", next, "", "client"); err != nil {
		t.Fatal(err)
	}
}

func TestFirstAdmin(t *testing.T) {
	store, c := withAdmin(t)
	c.now = c.now.Add(time.Hour)
	if _, _, err := store.Create("paul", "paul@example.org", RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}
	if !store.FirstAdmin("marie") || store.FirstAdmin("paul") || store.FirstAdmin("inconnu") {
		t.Fatal("premier administrateur mal reconnu")
	}
}

// Services autorisés par compte (spécification 8.1, TODO-4.1).
func TestAllowedServices(t *testing.T) {
	store, _ := withAdmin(t)
	if _, _, err := store.Create("paul", "paul@example.org", RoleUser, nil); err != nil {
		t.Fatal(err)
	}
	paul, _ := store.Get("paul")
	if !paul.MayOpen("wiki") {
		t.Fatal("par défaut, tous les services")
	}
	if _, err := store.SetServices("paul", true, []string{"wiki", "cloud", "wiki"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetServices("paul", true, []string{"../x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nom de service invalide : %v", err)
	}
	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	paul, _ = reopened.Get("paul")
	if !paul.MayOpen("wiki") || paul.MayOpen("site") || len(paul.Services) != 2 || paul.Services[0] != "cloud" || !paul.Public().RestrictServices {
		t.Fatalf("liste relue : %+v", paul)
	}
	admin, _ := reopened.Get(reopened.List()[0].Name)
	admin.RestrictServices = true
	if admin.Role == RoleAdmin && !admin.MayOpen("site") {
		t.Fatal("un administrateur ouvre tout")
	}
	if err := reopened.ForgetService("wiki"); err != nil {
		t.Fatal(err)
	}
	paul, _ = reopened.Get("paul")
	if paul.MayOpen("wiki") || !paul.MayOpen("cloud") {
		t.Fatalf("service désinstallé oublié : %+v", paul.Services)
	}
	paul, _ = reopened.SetServices("paul", false, []string{"cloud"})
	if !paul.MayOpen("site") || paul.Services != nil {
		t.Fatalf("restriction levée : %+v", paul)
	}
}
