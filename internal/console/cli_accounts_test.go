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
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// pia user : la console vue par la ligne de commande du Pi (jeton, 127.0.0.1).
func cliFixture(t *testing.T) fixture {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := auth.Open(filepath.Join(t.TempDir(), "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Accounts: accounts, CLIToken: "jeton",
		Hostname: func() (string, error) { return "pi-appliance-1234", nil }}), accounts: accounts}
}

func (f fixture) cli(method, path, body string) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, "http://127.0.0.1:8080/pi-appliance/api/"+path, reader)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "127.0.0.1:40000"
	request.Header.Set("Authorization", "Bearer jeton")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

// Sans langue de navigateur (pia), les e-mails partent en anglais.
func TestCLIMailLanguage(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/pi-appliance/api/accounts", nil)
	request.Header.Set("X-Pi-Appliance", "1")
	if lang := requestLanguage(request, ""); lang != "en" {
		t.Fatalf("sans Accept-Language : %q", lang)
	}
	request.Header.Set("Accept-Language", "fr-FR,fr;q=0.9")
	if lang := requestLanguage(request, ""); lang != "fr" {
		t.Fatalf("navigateur en français : %q", lang)
	}
}

func TestCLIAccounts(t *testing.T) {
	f := cliFixture(t)
	f.admin(t)
	created := f.cli(http.MethodPost, "accounts", `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	reply := decode[InvitationReply](t, created)
	// Le lien ne doit pas renvoyer à 127.0.0.1 : la personne ne l'ouvrirait pas.
	if created.Code != http.StatusCreated || !strings.HasPrefix(reply.Link, "https://pi-appliance-1234.local/pi-appliance/#invite=") {
		t.Fatalf("code %d, lien %q", created.Code, reply.Link)
	}
	token := reply.Link[strings.Index(reply.Link, "#invite=")+len("#invite="):]
	if _, err := f.accounts.AcceptInvitation(token, "huit car"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.accounts.CreateShare("paul", "pad", "courses", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	details := f.cli(http.MethodGet, "accounts/paul", "")
	got := decode[AccountDetails](t, details)
	if details.Code != http.StatusOK || got.Account.Name != "paul" || got.Account.Invited || len(got.Devices) != 0 || len(got.Shares) != 1 || got.Shares[0].Pad != "courses" {
		t.Fatalf("détail : code %d : %+v", details.Code, got)
	}
	if response := f.cli(http.MethodGet, "accounts/inconnu", ""); response.Code != http.StatusNotFound {
		t.Fatalf("compte inconnu : code %d", response.Code)
	}
	// Réservé à un administrateur.
	login := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "paul", "password": "huit car"}`, nil)
	if response := f.do(http.MethodGet, "/pi-appliance/api/accounts/paul", "", "", sessionCookie(t, login)); response.Code != http.StatusForbidden {
		t.Fatalf("utilisateur : code %d", response.Code)
	}
	if response := f.cli(http.MethodDelete, "accounts/marie", ""); response.Code != http.StatusConflict {
		t.Fatalf("dernier administrateur : code %d", response.Code)
	}
	if response := f.cli(http.MethodDelete, "accounts/paul", ""); response.Code != http.StatusNoContent {
		t.Fatalf("suppression : code %d", response.Code)
	}
}
