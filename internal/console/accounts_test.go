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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/pquerna/otp/totp"
)

// invite crée un compte par l'administrateur et renvoie le jeton de son lien.
func (f fixture) invite(t *testing.T, admin *http.Cookie, body string) string {
	t.Helper()
	response := f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/accounts", "application/json", body, admin)
	if response.Code != http.StatusCreated {
		t.Fatalf("création du compte : code %d : %s", response.Code, response.Body.String())
	}
	reply := decode[InvitationReply](t, response)
	prefix := "http://maison.example.org/pi-appliance/#invite="
	if reply.Sent || !strings.HasPrefix(reply.Link, prefix) {
		t.Fatalf("lien d'invitation : %+v", reply)
	}
	return strings.TrimPrefix(reply.Link, prefix)
}

func TestAccountsAdminOnly(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	check := f.do(http.MethodPost, "/pi-appliance/api/invitation/check", "application/json", `{"token": "`+token+`"}`, nil)
	if check.Code != http.StatusOK || decode[auth.Public](t, check).Name != "paul" {
		t.Fatalf("lecture de l'invitation : %d %s", check.Code, check.Body.String())
	}
	accept := f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil)
	if accept.Code != http.StatusOK {
		t.Fatalf("invitation acceptée : %d %s", accept.Code, accept.Body.String())
	}
	paul := sessionCookie(t, accept)
	for _, request := range [][2]string{
		{http.MethodGet, "/pi-appliance/api/accounts"},
		{http.MethodPost, "/pi-appliance/api/accounts"},
		{http.MethodDelete, "/pi-appliance/api/accounts/marie"},
		{http.MethodDelete, "/pi-appliance/api/accounts/marie/totp"},
	} {
		if response := f.do(request[0], request[1], "application/json", `{}`, paul); response.Code != http.StatusForbidden {
			t.Errorf("%s %s par un utilisateur : code %d", request[0], request[1], response.Code)
		}
	}
	list := decode[struct{ Accounts []auth.Public }](t, f.do(http.MethodGet, "/pi-appliance/api/accounts", "", "", admin))
	if len(list.Accounts) != 2 || strings.Contains(f.do(http.MethodGet, "/pi-appliance/api/accounts", "", "", admin).Body.String(), "hash") {
		t.Fatalf("comptes : %+v", list)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/accounts/marie", "", "", admin); response.Code != http.StatusConflict {
		t.Fatalf("supprimer son propre compte : %d", response.Code)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/accounts/paul", "", "", admin); response.Code != http.StatusNoContent {
		t.Fatalf("supprimer paul : %d %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", paul); response.Code != http.StatusUnauthorized {
		t.Fatalf("compte supprimé : %d", response.Code)
	}
}

func TestGuestNeedsEndDate(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/accounts", "application/json", `{"name": "invite", "email": "i@example.org", "role": "guest"}`, admin); response.Code != http.StatusBadRequest {
		t.Fatalf("invité sans date de fin : %d %s", response.Code, response.Body.String())
	}
	end := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	f.invite(t, admin, `{"name": "invite", "email": "i@example.org", "role": "guest", "expires": "`+end+`"}`)
}

func TestLoginWithCode(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	setup := decode[auth.TOTPSetup](t, f.do(http.MethodPost, "/pi-appliance/api/account/totp", "", "", admin))
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	confirm := f.do(http.MethodPost, "/pi-appliance/api/account/totp/confirm", "application/json", `{"code": "`+code+`"}`, admin)
	if confirm.Code != http.StatusOK {
		t.Fatalf("activation : %d %s", confirm.Code, confirm.Body.String())
	}
	codes := decode[struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}](t, confirm).RecoveryCodes
	login := f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+adminPassword+`"}`, nil)
	if login.Code != http.StatusUnauthorized || !strings.Contains(login.Body.String(), `"code_needed":true`) {
		t.Fatalf("connexion sans code : %d %s", login.Code, login.Body.String())
	}
	login = f.do(http.MethodPost, "/pi-appliance/api/login", "application/json", `{"name": "marie", "password": "`+adminPassword+`", "code": "`+codes[0]+`"}`, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("connexion avec un code de secours : %d %s", login.Code, login.Body.String())
	}
}

func TestRequiredTOTPBlocksConsole(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user", "totp_required": true}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", paul); response.Code != http.StatusForbidden {
		t.Fatalf("double authentification exigée, pas activée : %d", response.Code)
	}
	if response := f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/auth/return", "application/json", `{"return": "http://cloud.maison.example.org/"}`, paul); response.Code != http.StatusForbidden {
		t.Fatalf("pas de service avant l'activation : %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/account/totp", "", "", paul); response.Code != http.StatusOK {
		t.Fatalf("activation permise : %d %s", response.Code, response.Body.String())
	}
}

func forwardRequest(f fixture, remote string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/pi-appliance/api/auth/forward", nil)
	request.RemoteAddr = remote
	if cookie != nil {
		request.AddCookie(cookie)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

func TestForwardAuth(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	service := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "cloud.maison.example.org", "X-Forwarded-Uri": "/apps/files?dir=/", "X-Forwarded-Method": "GET"}

	if response := forwardRequest(f, "192.168.1.20:5000", nil, service); response.Code != http.StatusForbidden {
		t.Fatalf("appel hors de Traefik : %d", response.Code)
	}
	response := forwardRequest(f, "127.0.0.1:5000", nil, service)
	want := "https://maison.example.org/pi-appliance/?return=" + url.QueryEscape("https://cloud.maison.example.org/apps/files?dir=/")
	if response.Code != http.StatusFound || response.Header().Get("Location") != want {
		t.Fatalf("sans session : %d %q", response.Code, response.Header().Get("Location"))
	}
	post := map[string]string{"X-Forwarded-Host": "cloud.maison.example.org", "X-Forwarded-Method": "POST"}
	if response := forwardRequest(f, "172.18.0.2:5000", nil, post); response.Code != http.StatusUnauthorized {
		t.Fatalf("POST sans session : %d", response.Code)
	}
	// Le cookie de la console n'ouvre pas les services.
	if response := forwardRequest(f, "127.0.0.1:5000", &http.Cookie{Name: forwardCookie, Value: admin.Value}, service); response.Code != http.StatusFound {
		t.Fatalf("jeton de la console comme cookie délégué : %d", response.Code)
	}

	back := f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/auth/return", "application/json", `{"return": "https://cloud.maison.example.org/apps/files"}`, admin, "X-Forwarded-Proto", "https")
	if back.Code != http.StatusBadRequest {
		t.Fatalf("https demandé hors de Traefik : %d", back.Code)
	}
	back = f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/auth/return", "application/json", `{"return": "http://cloud.maison.example.org/apps/files"}`, admin)
	if back.Code != http.StatusOK || decode[map[string]string](t, back)["url"] != "http://cloud.maison.example.org/apps/files" {
		t.Fatalf("retour au service : %d %s", back.Code, back.Body.String())
	}
	var forward *http.Cookie
	for _, cookie := range back.Result().Cookies() {
		if cookie.Name == forwardCookie {
			forward = cookie
		}
	}
	if forward == nil || forward.Domain != "maison.example.org" || forward.Path != "/" || !forward.HttpOnly || forward.SameSite != http.SameSiteLaxMode || forward.Value == admin.Value {
		t.Fatalf("cookie délégué : %+v", forward)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", &http.Cookie{Name: cookieName, Value: forward.Value}); response.Code != http.StatusUnauthorized {
		t.Fatalf("le cookie délégué ouvre la console : %d", response.Code)
	}
	response = forwardRequest(f, "127.0.0.1:5000", forward, service)
	if response.Code != http.StatusOK || response.Header().Get("Remote-User") != "marie" || response.Header().Get("Remote-Email") != "marie@example.org" || response.Header().Get("Remote-Groups") != "admin" {
		t.Fatalf("avec session déléguée : %d %v", response.Code, response.Header())
	}

	logout := f.do(http.MethodPost, "http://maison.example.org/pi-appliance/api/logout", "", "", admin, "Cookie", forwardCookie+"="+forward.Value+"; "+cookieName+"="+admin.Value)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("déconnexion : %d", logout.Code)
	}
	if response := forwardRequest(f, "127.0.0.1:5000", forward, service); response.Code != http.StatusFound {
		t.Fatalf("après déconnexion : %d", response.Code)
	}
}

func TestReturnURL(t *testing.T) {
	for _, test := range []struct {
		raw, host string
		ok        bool
	}{
		{"https://cloud.maison.example.org/x", "maison.example.org", true},
		{"https://maison.example.org/pi-appliance/", "maison.example.org", true},
		{"https://CLOUD.Maison.example.org./", "maison.example.org", true},
		{"https://evil.example/", "maison.example.org", false},
		{"https://maison.example.org.evil.example/", "maison.example.org", false},
		{"https://evilmaison.example.org/", "maison.example.org", false},
		{"https://marie@cloud.maison.example.org/", "maison.example.org", false},
		{"http://cloud.maison.example.org/", "maison.example.org", false},
		{"//cloud.maison.example.org/", "maison.example.org", false},
		{"javascript:alert(1)", "maison.example.org", false},
		{"https://1.168.192.0/", "192.168.1.20", false},
		{"https://192.168.1.20/x", "192.168.1.20", true},
	} {
		if _, ok := returnURL(test.raw, test.host, true); ok != test.ok {
			t.Errorf("returnURL(%q, %q) = %v", test.raw, test.host, ok)
		}
	}
	if cookieDomain("192.168.1.20") != "" || cookieDomain("localhost") != "" || cookieDomain("maison.example.org") != "maison.example.org" {
		t.Error("domaine du cookie délégué")
	}
	if parentHost("cloud.maison.example.org") != "maison.example.org" || parentHost("example.org") != "example.org" || parentHost("cloud.maison.example.org:8443") != "maison.example.org:8443" {
		t.Error("adresse de la console")
	}
}
