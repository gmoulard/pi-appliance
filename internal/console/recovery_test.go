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

package console

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/mail"
	"github.com/gmoulard/pi-appliance/marketplace"
)

type sentMail struct {
	kind, name, email, link, lang string
}

type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

func (m *fakeMailer) Invite(account auth.Public, link, lang string) error {
	return m.add("invite", account, link, lang)
}

func (m *fakeMailer) PasswordReset(account auth.Public, link, lang string) error {
	return m.add("reset", account, link, lang)
}

func (m *fakeMailer) add(kind string, account auth.Public, link, lang string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{kind, account.Name, account.Email, link, lang})
	return nil
}

func (m *fakeMailer) take() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	sent := m.sent
	m.sent = nil
	return sent
}

// forgot : POST api/password/forgot arrivé à l'adresse host.
func (f fixture) forgot(host, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/pi-appliance/api/password/forgot", strings.NewReader(body))
	request.RemoteAddr = "192.168.1.20:5000"
	request.Host = host
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

func mailFixture(t *testing.T, mailer auth.Mailer) fixture {
	t.Helper()
	previous := sendAsync
	sendAsync = func(send func()) { send() }
	t.Cleanup(func() { sendAsync = previous })
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := auth.Open(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		handler: Handler(Config{
			Version:  "1.2.3",
			Catalog:  catalog,
			Accounts: accounts,
			Mailer:   mailer,
			Hostname: func() (string, error) { return "pi-appliance-1234", nil },
		}),
		accounts: accounts,
	}
}

func TestForgotPasswordByEmail(t *testing.T) {
	mailer := &fakeMailer{}
	f := mailFixture(t, mailer)
	f.admin(t)
	state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", nil))
	if !state.RecoveryEmail {
		t.Fatal("récupération par e-mail attendue")
	}
	// Compte inconnu : même réponse, aucun e-mail.
	response := f.forgot("pi-appliance-1234.local", `{"name": "personne"}`)
	if response.Code != http.StatusAccepted || len(mailer.take()) != 0 {
		t.Fatalf("compte inconnu : code %d : %s", response.Code, response.Body.String())
	}
	response = f.forgot("pi-appliance-1234.local", `{"name": "marie@example.org", "lang": "en"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	sent := mailer.take()
	if len(sent) != 1 || sent[0].kind != "reset" || sent[0].email != "marie@example.org" || sent[0].lang != "en" {
		t.Fatalf("e-mails : %+v", sent)
	}
	link := sent[0].link
	if !strings.HasPrefix(link, "http://pi-appliance-1234.local/pi-appliance/#reset=") {
		t.Fatalf("lien %q", link)
	}
	if _, err := (mail.Request{Kind: mail.KindReset, To: "marie@example.org", Name: "marie", Link: link}).Check(); err != nil {
		t.Fatalf("lien refusé par le service d'envoi : %v", err)
	}
	token := link[strings.Index(link, "#reset=")+len("#reset="):]
	response = f.do(http.MethodPost, "/pi-appliance/api/password/check", "application/json", `{"token": "`+token+`"}`, nil)
	if account := decode[auth.Public](t, response); response.Code != http.StatusOK || account.Name != "marie" {
		t.Fatalf("lien : code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/password/reset", "application/json", `{"token": "`+token+`", "password": "un nouveau mot de passe"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("nouveau mot de passe : code %d : %s", response.Code, response.Body.String())
	}
	sessionCookie(t, response)
	response = f.do(http.MethodPost, "/pi-appliance/api/password/reset", "application/json", `{"token": "`+token+`", "password": "un nouveau mot de passe"}`, nil)
	if response.Code != http.StatusGone {
		t.Fatalf("lien utilisé deux fois : code %d", response.Code)
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "un nouveau mot de passe"}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("connexion : code %d : %s", response.Code, response.Body.String())
	}
}

// Le lien n'est jamais construit sur une adresse inconnue du Pi (en-tête Host
// choisi par l'auteur de la demande).
func TestForgotPasswordLinkHost(t *testing.T) {
	for host, want := range map[string]string{
		"pirate.example.org":          "https://pi-appliance-1234.local/",
		"8.8.8.8":                     "https://pi-appliance-1234.local/",
		"192.168.1.30":                "http://192.168.1.30/",
		"PI-APPLIANCE-1234.local:443": "http://pi-appliance-1234.local:443/",
	} {
		// Un Pi par adresse : 3 demandes par heure et par compte.
		mailer := &fakeMailer{}
		f := mailFixture(t, mailer)
		f.admin(t)
		response := f.forgot(host, `{"name": "marie"}`)
		if response.Code != http.StatusAccepted {
			t.Fatalf("%s : code %d : %s", host, response.Code, response.Body.String())
		}
		sent := mailer.take()
		if len(sent) != 1 || !strings.HasPrefix(sent[0].link, want+"pi-appliance/#reset=") {
			t.Fatalf("Host %s : %+v, attendu %s…", host, sent, want)
		}
	}
}

func TestForgotPasswordWithoutMailer(t *testing.T) {
	f := mailFixture(t, nil)
	f.admin(t)
	state := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", nil))
	if state.RecoveryEmail {
		t.Fatal("pas de récupération par e-mail sans service d'envoi")
	}
	response := f.do(http.MethodPost, "/pi-appliance/api/password/forgot", "application/json", `{"name": "marie"}`, nil)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "fichier reset") {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
}

func TestForgotPasswordLimit(t *testing.T) {
	mailer := &fakeMailer{}
	f := mailFixture(t, mailer)
	f.admin(t)
	var last int
	for range 4 {
		last = f.do(http.MethodPost, "/pi-appliance/api/password/forgot", "application/json", `{"name": "marie"}`, nil).Code
	}
	if last != http.StatusTooManyRequests || len(mailer.take()) != 3 {
		t.Fatalf("4e demande : code %d", last)
	}
}
