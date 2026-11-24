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

package console

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/oidc"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

func newOIDCFixture(t *testing.T) fixture {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := oidc.LoadOrCreateSigner(filepath.Join(dir, "oidc-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &oidc.Provider{Signer: signer, Clients: &oidc.Clients{Path: filepath.Join(dir, "oidc-clients.json")}}
	manager := &services.Manager{Dir: dir, Domain: "maison.local", Runner: okRunner{}}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, OIDC: provider}), accounts: accounts, manager: manager}
}

// Parcours OpenID Connect par la console (spécification 8.1, TODO-4.1) :
// service relié par l'administrateur, demande du service, reprise par la
// console connectée, échange du code ; services autorisés par compte.
func TestOIDCThroughConsole(t *testing.T) {
	f := newOIDCFixture(t)
	admin := f.admin(t)
	installed(t, f, "wiki", "wiki-maison.local", services.AccessUsers)
	installed(t, f, "site", "site-maison.local", services.AccessUsers)
	callback := "https://wiki-maison.local/oidc/callback"
	if r := f.do(http.MethodPost, "/pi-appliance/api/oidc/clients", "application/json", `{"service": "absent", "redirect_uris": ["`+callback+`"]}`, admin); r.Code != http.StatusNotFound {
		t.Fatalf("service absent : %d", r.Code)
	}
	reply := decode[OIDCClientReply](t, f.do(http.MethodPost, "/pi-appliance/api/oidc/clients", "application/json", `{"service": "wiki", "redirect_uris": ["`+callback+`"]}`, admin))
	if reply.Secret == "" || reply.Issuer != "https://maison.local/pi-appliance/oidc" {
		t.Fatalf("client : %+v", reply)
	}
	if list := f.do(http.MethodGet, "/pi-appliance/api/oidc/clients", "", "", admin).Body.String(); strings.Contains(list, reply.Secret) || !strings.Contains(list, reply.Client.ID) {
		t.Fatalf("liste des clients : %s", list)
	}
	discovery := f.do(http.MethodGet, "/pi-appliance/oidc/.well-known/openid-configuration", "", "", nil)
	if !strings.Contains(discovery.Body.String(), `"authorization_endpoint":"https://maison.local/pi-appliance/oidc/authorize"`) {
		t.Fatalf("découverte : %s", discovery.Body.String())
	}
	verifier := strings.Repeat("v", 50)
	sum := sha256.Sum256([]byte(verifier))
	params := url.Values{"response_type": {"code"}, "client_id": {reply.Client.ID}, "redirect_uri": {callback}, "scope": {"openid profile"}, "state": {"s"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	begin := func() string {
		r := f.do(http.MethodGet, "/pi-appliance/oidc/authorize?"+params.Encode(), "", "", nil)
		location, _ := url.Parse(r.Header().Get("Location"))
		if r.Code != http.StatusFound || location.Host != "maison.local" || location.Path != "/pi-appliance/" || location.Query().Get("oidc") == "" {
			t.Fatalf("demande du service : %d %s", r.Code, location)
		}
		return location.Query().Get("oidc")
	}
	id := begin()
	if r := f.do(http.MethodPost, "/pi-appliance/api/oidc/authorize", "application/json", `{"request": "`+id+`"}`, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : %d", r.Code)
	}
	back := decode[map[string]string](t, f.do(http.MethodPost, "/pi-appliance/api/oidc/authorize", "application/json", `{"request": "`+id+`"}`, admin))["url"]
	target, _ := url.Parse(back)
	if !strings.HasPrefix(back, callback+"?") || target.Query().Get("code") == "" {
		t.Fatalf("retour au service : %s", back)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {target.Query().Get("code")}, "redirect_uri": {callback}, "code_verifier": {verifier},
		"client_id": {reply.Client.ID}, "client_secret": {reply.Secret}}
	request := httptest.NewRequest(http.MethodPost, "/pi-appliance/oidc/token", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "172.18.0.5:4000"
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"id_token"`) {
		t.Fatalf("jeton : %d %s", recorder.Code, recorder.Body.String())
	}

	// Un utilisateur limité à « site » n'ouvre pas « wiki », ni par OpenID
	// Connect ni par l'authentification déléguée.
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	if r := f.do(http.MethodPut, "/pi-appliance/api/accounts/paul/services", "application/json", `{"restrict": true, "services": ["site"]}`, paul); r.Code != http.StatusForbidden {
		t.Fatalf("réglé par un utilisateur : %d", r.Code)
	}
	set := f.do(http.MethodPut, "/pi-appliance/api/accounts/paul/services", "application/json", `{"restrict": true, "services": ["site"]}`, admin)
	if public := decode[auth.Public](t, set); !public.RestrictServices || len(public.Services) != 1 {
		t.Fatalf("services de paul : %+v", public)
	}
	if r := f.do(http.MethodPost, "/pi-appliance/api/oidc/authorize", "application/json", `{"request": "`+begin()+`"}`, paul); r.Code != http.StatusForbidden {
		t.Fatalf("OpenID Connect, service non autorisé : %d", r.Code)
	}
	wiki := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "wiki-maison.local", "X-Forwarded-Uri": "/", "X-Forwarded-Method": "GET"}
	site := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "site-maison.local", "X-Forwarded-Uri": "/", "X-Forwarded-Method": "GET"}
	siteCookie := forwardCookieFor(t, f, paul, "/pi-appliance/api/auth/forward", site)
	if r := forwardRequest(f, "127.0.0.1:5000", siteCookie, site); r.Code != http.StatusOK {
		t.Fatalf("service autorisé : %d", r.Code)
	}
	paulWiki := forwardCookieFor(t, f, paul, "/pi-appliance/api/auth/forward", wiki)
	if r := forwardRequest(f, "127.0.0.1:5000", paulWiki, wiki); r.Code != http.StatusForbidden {
		t.Fatalf("service non autorisé, authentification déléguée : %d", r.Code)
	}
	adminWiki := forwardCookieFor(t, f, admin, "/pi-appliance/api/auth/forward", wiki)
	if r := forwardRequest(f, "127.0.0.1:5000", adminWiki, wiki); r.Code != http.StatusOK {
		t.Fatalf("administrateur : %d", r.Code)
	}
	// Désinstallé : le client et l'autorisation sont oubliés.
	if r := f.do(http.MethodDelete, "/pi-appliance/api/services/wiki?data=delete", "", "", admin); r.Code != http.StatusNoContent {
		t.Fatalf("désinstallation : %d %s", r.Code, r.Body.String())
	}
	if list := f.do(http.MethodGet, "/pi-appliance/api/oidc/clients", "", "", admin).Body.String(); strings.Contains(list, reply.Client.ID) {
		t.Fatalf("client oublié : %s", list)
	}
}
