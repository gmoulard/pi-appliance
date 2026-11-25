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

package oidc

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

type fakeAccounts map[string]auth.Account

func (f fakeAccounts) Get(name string) (auth.Account, error) {
	if a, ok := f[name]; ok {
		return a, nil
	}
	return auth.Account{}, auth.ErrNotFound
}

const (
	redirect = "https://wiki-maison.local/oidc/callback"
	verifier = "un-verificateur-pkce-assez-long-pour-la-rfc-7636-0123456789"
)

type fixture struct {
	provider *Provider
	mux      *http.ServeMux
	accounts fakeAccounts
	client   PublicClient
	secret   string
	now      time.Time
}

func newFixture(t *testing.T) *fixture {
	dir := t.TempDir()
	signer, err := LoadOrCreateSigner(filepath.Join(dir, "oidc-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	clients := &Clients{Path: filepath.Join(dir, "oidc-clients.json")}
	f := &fixture{now: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)}
	f.client, f.secret, err = clients.Register("wiki", []string{redirect}, f.now)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	f.accounts = fakeAccounts{
		"marie": {Name: "marie", Email: "marie@example.org", EmailVerified: true, Role: auth.RoleUser, Hash: "h", CreatedAt: created},
		"paul":  {Name: "paul", Role: auth.RoleUser, Hash: "h", CreatedAt: created},
	}
	f.provider = &Provider{
		Issuer:   func() string { return "https://maison.local/pi-appliance/oidc" },
		Signer:   signer,
		Clients:  clients,
		Accounts: f.accounts,
		Allowed:  func(a auth.Account, service string) bool { return a.Name != "paul" },
		LoginURL: func(id string) string { return "https://maison.local/pi-appliance/?oidc=" + id },
		Now:      func() time.Time { return f.now },
	}
	f.mux = http.NewServeMux()
	f.provider.Routes(f.mux, "/pi-appliance/oidc")
	return f
}

func (f *fixture) do(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, r)
	return w
}

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (f *fixture) authorize(t *testing.T, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(httptest.NewRequest(http.MethodGet, "/pi-appliance/oidc/authorize?"+params.Encode(), nil))
}

func (f *fixture) goodParams() url.Values {
	return url.Values{"response_type": {"code"}, "client_id": {f.client.ID}, "redirect_uri": {redirect}, "scope": {"openid profile email groups"},
		"state": {"etat-1"}, "nonce": {"n-1"}, "code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}}
}

// code : demande, puis accord pour account ; renvoie le code.
func (f *fixture) code(t *testing.T, account string) string {
	t.Helper()
	response := f.authorize(t, f.goodParams())
	location, _ := url.Parse(response.Header().Get("Location"))
	if response.Code != http.StatusFound || location.Host != "maison.local" {
		t.Fatalf("demande : %d %s", response.Code, response.Header().Get("Location"))
	}
	id := location.Query().Get("oidc")
	if service, ok := f.provider.Pending(id); !ok || service != "wiki" {
		t.Fatalf("demande en attente : %q %v", service, ok)
	}
	back, err := f.provider.Approve(id, f.accounts[account])
	if err != nil {
		t.Fatal(err)
	}
	target, _ := url.Parse(back)
	if !strings.HasPrefix(back, redirect+"?") || target.Query().Get("state") != "etat-1" || target.Query().Get("iss") != "https://maison.local/pi-appliance/oidc" {
		t.Fatalf("retour : %s", back)
	}
	if _, err := f.provider.Approve(id, f.accounts[account]); !errors.Is(err, ErrRequest) {
		t.Fatalf("demande servie deux fois : %v", err)
	}
	return target.Query().Get("code")
}

func (f *fixture) exchange(code, v, secret string) *httptest.ResponseRecorder {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {v}}
	r := httptest.NewRequest(http.MethodPost, "/pi-appliance/oidc/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.SetBasicAuth(f.client.ID, secret)
	return f.do(r)
}

func verifyJWT(t *testing.T, f *fixture, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jeton : %s", token)
	}
	var header map[string]string
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	json.Unmarshal(raw, &header)
	if header["alg"] != "ES256" || header["kid"] != f.provider.Signer.KeyID {
		t.Fatalf("en-tête : %v", header)
	}
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(signature) != 64 || !ecdsa.Verify(f.provider.Signer.Public(), digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Fatal("signature fausse")
	}
	var claims map[string]any
	raw, _ = base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(raw, &claims)
	return claims
}

func TestFlow(t *testing.T) {
	f := newFixture(t)
	discovery := f.do(httptest.NewRequest(http.MethodGet, "/pi-appliance/oidc/.well-known/openid-configuration", nil))
	var config map[string]any
	json.Unmarshal(discovery.Body.Bytes(), &config)
	if config["issuer"] != "https://maison.local/pi-appliance/oidc" || config["token_endpoint"] != "https://maison.local/pi-appliance/oidc/token" {
		t.Fatalf("découverte : %s", discovery.Body.String())
	}
	jwks := f.do(httptest.NewRequest(http.MethodGet, "/pi-appliance/oidc/jwks", nil)).Body.String()
	if !strings.Contains(jwks, f.provider.Signer.KeyID) || strings.Contains(jwks, `"d"`) {
		t.Fatalf("clés : %s", jwks)
	}
	code := f.code(t, "marie")
	response := f.exchange(code, verifier, f.secret)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("jeton : %d %s", response.Code, response.Body.String())
	}
	var tokens struct {
		Access string `json:"access_token"`
		ID     string `json:"id_token"`
	}
	json.Unmarshal(response.Body.Bytes(), &tokens)
	claims := verifyJWT(t, f, tokens.ID)
	if claims["iss"] != "https://maison.local/pi-appliance/oidc" || claims["aud"] != f.client.ID || claims["nonce"] != "n-1" || claims["email"] != "marie@example.org" ||
		claims["preferred_username"] != "marie" || claims["sub"] != Subject(f.accounts["marie"]) || claims["exp"].(float64) != float64(f.now.Add(time.Hour).Unix()) {
		t.Fatalf("revendications : %v", claims)
	}
	// Le code ne sert qu'une fois.
	if again := f.exchange(code, verifier, f.secret); again.Code != http.StatusBadRequest {
		t.Fatalf("code réutilisé : %d", again.Code)
	}
	userinfo := httptest.NewRequest(http.MethodGet, "/pi-appliance/oidc/userinfo", nil)
	userinfo.Header.Set("Authorization", "Bearer "+tokens.Access)
	if info := f.do(userinfo); info.Code != http.StatusOK || !strings.Contains(info.Body.String(), `"preferred_username":"marie"`) {
		t.Fatalf("userinfo : %d %s", info.Code, info.Body.String())
	}
	// Compte supprimé : le jeton d'accès ne vaut plus rien.
	delete(f.accounts, "marie")
	if info := f.do(userinfo); info.Code != http.StatusUnauthorized {
		t.Fatalf("userinfo, compte supprimé : %d", info.Code)
	}
}

func TestTokenRefusals(t *testing.T) {
	f := newFixture(t)
	if r := f.exchange(f.code(t, "marie"), verifier, "faux"); r.Code != http.StatusUnauthorized {
		t.Fatalf("mauvais secret : %d", r.Code)
	}
	if r := f.exchange(f.code(t, "marie"), strings.Repeat("a", 43), f.secret); r.Code != http.StatusBadRequest {
		t.Fatalf("mauvais vérificateur PKCE : %d", r.Code)
	}
	code := f.code(t, "marie")
	f.now = f.now.Add(3 * time.Minute)
	if r := f.exchange(code, verifier, f.secret); r.Code != http.StatusBadRequest {
		t.Fatalf("code expiré : %d", r.Code)
	}
	// Invité expiré entre l'accord et l'échange.
	code = f.code(t, "marie")
	expired := f.now.Add(-time.Hour)
	marie := f.accounts["marie"]
	marie.ExpiresAt = &expired
	f.accounts["marie"] = marie
	if r := f.exchange(code, verifier, f.secret); r.Code != http.StatusBadRequest {
		t.Fatalf("invité expiré : %d", r.Code)
	}
}

func TestAuthorizeRefusals(t *testing.T) {
	f := newFixture(t)
	params := f.goodParams()
	params.Set("redirect_uri", "https://ailleurs.example/callback")
	if r := f.authorize(t, params); r.Code != http.StatusBadRequest || r.Header().Get("Location") != "" {
		t.Fatalf("adresse de retour inconnue : %d %s", r.Code, r.Header().Get("Location"))
	}
	params = f.goodParams()
	params.Set("client_id", "inconnu")
	if r := f.authorize(t, params); r.Code != http.StatusBadRequest {
		t.Fatalf("client inconnu : %d", r.Code)
	}
	for name, change := range map[string][2]string{
		"sans PKCE":      {"code_challenge_method", "plain"},
		"sans openid":    {"scope", "profile"},
		"flux implicite": {"response_type", "token"},
	} {
		params = f.goodParams()
		params.Set(change[0], change[1])
		r := f.authorize(t, params)
		location, _ := url.Parse(r.Header().Get("Location"))
		if r.Code != http.StatusFound || !strings.HasPrefix(location.String(), redirect) || location.Query().Get("error") == "" || location.Query().Get("state") != "etat-1" {
			t.Errorf("%s : %d %s", name, r.Code, location)
		}
	}
	// Compte sans accès au service.
	r := f.authorize(t, f.goodParams())
	location, _ := url.Parse(r.Header().Get("Location"))
	if _, err := f.provider.Approve(location.Query().Get("oidc"), f.accounts["paul"]); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("compte sans accès : %v", err)
	}
	if _, err := f.provider.Approve("inconnue", f.accounts["marie"]); !errors.Is(err, ErrRequest) {
		t.Fatalf("demande inconnue : %v", err)
	}
}

func TestClientsAndKey(t *testing.T) {
	dir := t.TempDir()
	clients := &Clients{Path: filepath.Join(dir, "clients.json")}
	if _, _, err := clients.Register("wiki", []string{"http://wiki-maison.local/cb"}, time.Now()); !errors.Is(err, ErrInvalidClient) {
		t.Fatalf("adresse en http : %v", err)
	}
	first, secret, err := clients.Register("wiki", []string{redirect}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	second, _, _ := clients.Register("wiki", []string{redirect}, time.Now())
	if list, _ := clients.List(); len(list) != 1 || list[0].ID != second.ID {
		t.Fatalf("client remplacé : %+v", list)
	}
	if _, ok := clients.Authenticate(first.ID, secret); ok {
		t.Fatal("ancien client encore accepté")
	}
	data, _ := os.ReadFile(clients.Path)
	if strings.Contains(string(data), secret) {
		t.Fatal("secret gardé en clair")
	}
	clients.RemoveService("wiki")
	if list, _ := (&Clients{Path: clients.Path}).List(); len(list) != 0 {
		t.Fatalf("client retiré : %+v", list)
	}
	a, err := LoadOrCreateSigner(filepath.Join(dir, "key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadOrCreateSigner(filepath.Join(dir, "key.pem"))
	if err != nil || a.KeyID != b.KeyID {
		t.Fatalf("clé relue : %v", err)
	}
	if info, _ := os.Stat(filepath.Join(dir, "key.pem")); info.Mode().Perm() != 0o600 {
		t.Fatalf("droits de la clé : %v", info.Mode())
	}
}
