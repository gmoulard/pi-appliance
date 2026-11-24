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

package oidc

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// Durées.
const (
	// requestTTL : temps pour se connecter à la console après la demande d'un service.
	requestTTL = 10 * time.Minute
	// codeTTL : un code d'autorisation sert une fois, dans les 2 minutes.
	codeTTL = 2 * time.Minute
	// tokenTTL : jeton d'identité et jeton d'accès (userinfo).
	tokenTTL = time.Hour
	// maxPending : demandes, codes et jetons gardés en mémoire au plus.
	maxPending = 10000
)

var (
	// ErrRequest : demande d'autorisation inconnue ou expirée.
	ErrRequest = errors.New("demande de connexion inconnue ou expirée")
	// ErrNotAllowed : le compte n'a pas accès à ce service.
	ErrNotAllowed = errors.New("ce compte n'a pas accès à ce service")

	verifierPattern  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
	challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	paramPattern     = regexp.MustCompile(`^[\x20-\x7e]{0,512}$`)
)

// Accounts : comptes de la console (*auth.Store).
type Accounts interface {
	Get(name string) (auth.Account, error)
}

// Provider : le fournisseur OpenID Connect.
type Provider struct {
	// Issuer : identifiant du fournisseur, https://<adresse>/pi-appliance/oidc.
	Issuer   func() string
	Signer   *Signer
	Clients  *Clients
	Accounts Accounts
	// Allowed : le compte a accès au service ; nil : tous les comptes actifs.
	Allowed func(account auth.Account, service string) bool
	// LoginURL : page de la console qui reprend la demande (id), après connexion.
	LoginURL func(id string) string
	Now      func() time.Time

	mu       sync.Mutex
	requests map[string]*request
	codes    map[string]*grant
	tokens   map[string]*grant
}

// request : une demande d'autorisation, en attente de la connexion.
type request struct {
	client    string
	redirect  string
	state     string
	nonce     string
	challenge string
	scopes    []string
	expires   time.Time
}

// grant : un code d'autorisation ou un jeton d'accès.
type grant struct {
	request
	account  string
	authTime time.Time
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Subject : identifiant stable d'un compte pour les services (« sub ») : tiré
// du nom et de la date de création, il change si un compte supprimé est
// recréé sous le même nom (ce n'est pas la même personne).
func Subject(account auth.Account) string {
	sum := sha256.Sum256([]byte(account.Name + "|" + account.CreatedAt.UTC().Format(time.RFC3339Nano)))
	return b64(sum[:18])
}

// Routes ajoute les adresses publiques du fournisseur sous base
// (/pi-appliance/oidc) : découverte, clés, autorisation, jeton, userinfo.
func (p *Provider) Routes(mux *http.ServeMux, base string) {
	mux.HandleFunc("GET "+base+"/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET "+base+"/jwks", p.jwks)
	mux.HandleFunc("GET "+base+"/authorize", p.authorize)
	mux.HandleFunc("POST "+base+"/token", p.token)
	mux.HandleFunc("GET "+base+"/userinfo", p.userinfo)
	mux.HandleFunc("POST "+base+"/userinfo", p.userinfo)
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(value)
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	issuer := p.Issuer()
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/authorize",
		"token_endpoint":                        issuer + "/token",
		"userinfo_endpoint":                     issuer + "/userinfo",
		"jwks_uri":                              issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"ES256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"claims_supported":                      []string{"sub", "iss", "aud", "exp", "iat", "auth_time", "nonce", "preferred_username", "name", "email", "email_verified", "groups"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, p.Signer.JWKS())
}

// purge retire ce qui a expiré ; le verrou est pris.
func (p *Provider) purge(now time.Time) {
	for id, r := range p.requests {
		if now.After(r.expires) {
			delete(p.requests, id)
		}
	}
	for _, m := range []map[string]*grant{p.codes, p.tokens} {
		for id, g := range m {
			if now.After(g.expires) {
				delete(m, id)
			}
		}
	}
}

// authorize : demande d'un service (navigation). Le client et l'adresse de
// retour sont vérifiés avant toute redirection (sinon page d'erreur) ; la
// demande est gardée et la console reprend la main (connexion si besoin).
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	client, ok := p.Clients.Get(q.Get("client_id"))
	redirect := q.Get("redirect_uri")
	if !ok || !slices.Contains(client.RedirectURIs, redirect) {
		http.Error(w, "Demande de connexion refusée : ce service n'est pas enregistré auprès de la console.", http.StatusBadRequest)
		return
	}
	for _, name := range []string{"state", "nonce", "scope"} {
		if !paramPattern.MatchString(q.Get(name)) {
			redirectError(w, r, redirect, q.Get("state"), "invalid_request")
			return
		}
	}
	scopes := strings.Fields(q.Get("scope"))
	switch {
	case q.Get("response_type") != "code":
		redirectError(w, r, redirect, q.Get("state"), "unsupported_response_type")
		return
	case !slices.Contains(scopes, "openid"):
		redirectError(w, r, redirect, q.Get("state"), "invalid_scope")
		return
	case q.Get("code_challenge_method") != "S256" || !challengePattern.MatchString(q.Get("code_challenge")):
		// PKCE obligatoire, en S256 seulement (pas de « plain »).
		redirectError(w, r, redirect, q.Get("state"), "invalid_request")
		return
	case q.Get("request") != "" || q.Get("request_uri") != "":
		redirectError(w, r, redirect, q.Get("state"), "request_not_supported")
		return
	}
	now := p.now()
	id := random(32)
	p.mu.Lock()
	if p.requests == nil {
		p.requests = map[string]*request{}
	}
	p.purge(now)
	if len(p.requests) >= maxPending {
		p.mu.Unlock()
		redirectError(w, r, redirect, q.Get("state"), "temporarily_unavailable")
		return
	}
	p.requests[id] = &request{client: client.ID, redirect: redirect, state: q.Get("state"), nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), scopes: scopes, expires: now.Add(requestTTL)}
	p.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, p.LoginURL(id), http.StatusFound)
}

func redirectError(w http.ResponseWriter, r *http.Request, redirect, state, code string) {
	target, _ := url.Parse(redirect)
	values := target.Query()
	values.Set("error", code)
	if state != "" && paramPattern.MatchString(state) {
		values.Set("state", state)
	}
	target.RawQuery = values.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

// Pending : nom du service d'une demande en attente (écran de la console).
func (p *Provider) Pending(id string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	req, ok := p.requests[id]
	if !ok || p.now().After(req.expires) {
		return "", false
	}
	client, ok := p.Clients.Get(req.client)
	return client.Service, ok
}

// Approve : la personne connectée à la console (account) accepte la demande
// id ; renvoie l'adresse de retour du service, avec le code d'autorisation.
// La demande ne sert qu'une fois, même refusée.
func (p *Provider) Approve(id string, account auth.Account) (string, error) {
	now := p.now()
	p.mu.Lock()
	req, ok := p.requests[id]
	delete(p.requests, id)
	p.mu.Unlock()
	if !ok || now.After(req.expires) {
		return "", ErrRequest
	}
	client, ok := p.Clients.Get(req.client)
	if !ok {
		return "", ErrRequest
	}
	if !p.active(account, client.Service, now) {
		return "", ErrNotAllowed
	}
	code := random(32)
	p.mu.Lock()
	if p.codes == nil {
		p.codes = map[string]*grant{}
	}
	p.purge(now)
	if len(p.codes) >= maxPending {
		p.mu.Unlock()
		return "", ErrRequest
	}
	g := &grant{request: *req, account: account.Name, authTime: now}
	g.expires = now.Add(codeTTL)
	p.codes[hash(code)] = g
	p.mu.Unlock()
	target, _ := url.Parse(req.redirect)
	values := target.Query()
	values.Set("code", code)
	if req.state != "" {
		values.Set("state", req.state)
	}
	values.Set("iss", p.Issuer())
	target.RawQuery = values.Encode()
	return target.String(), nil
}

// active : compte qui existe, a un mot de passe, n'a pas expiré et a accès au service.
func (p *Provider) active(account auth.Account, service string, now time.Time) bool {
	if account.Name == "" || account.Hash == "" || account.Expired(now) {
		return false
	}
	return p.Allowed == nil || p.Allowed(account, service)
}

func tokenError(w http.ResponseWriter, code int, kind string) {
	if code == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="pi-appliance"`)
	}
	writeJSON(w, code, map[string]string{"error": kind})
}

// clientAuth : client_secret_basic ou client_secret_post, pas les deux.
func (p *Provider) clientAuth(r *http.Request) (Client, bool) {
	id, secret, basic := r.BasicAuth()
	if basic {
		if r.PostForm.Get("client_secret") != "" {
			return Client{}, false
		}
		// RFC 6749 2.3.1 : identifiant et secret encodés en x-www-form-urlencoded.
		if v, err := url.QueryUnescape(id); err == nil {
			id = v
		}
		if v, err := url.QueryUnescape(secret); err == nil {
			secret = v
		}
	} else {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	return p.Clients.Authenticate(id, secret)
}

// token : échange du code contre les jetons, par le service (pas le navigateur).
func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		tokenError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	client, ok := p.clientAuth(r)
	if !ok {
		tokenError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		tokenError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	now := p.now()
	p.mu.Lock()
	key := hash(r.PostForm.Get("code"))
	g, found := p.codes[key]
	// Un code ne sert qu'une fois, même si l'échange échoue.
	delete(p.codes, key)
	p.mu.Unlock()
	verifier := r.PostForm.Get("code_verifier")
	switch {
	case !found || now.After(g.expires) || g.client != client.ID || g.redirect != r.PostForm.Get("redirect_uri"):
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	case !verifierPattern.MatchString(verifier) || pkce(verifier) != g.challenge:
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	account, err := p.Accounts.Get(g.account)
	if err != nil || !p.active(account, client.Service, now) {
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	claims := p.claims(account, g.scopes)
	claims["iss"] = p.Issuer()
	claims["aud"] = client.ID
	claims["azp"] = client.ID
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(tokenTTL).Unix()
	claims["auth_time"] = g.authTime.Unix()
	if g.nonce != "" {
		claims["nonce"] = g.nonce
	}
	idToken, err := p.Signer.Sign(claims)
	if err != nil {
		tokenError(w, http.StatusInternalServerError, "server_error")
		return
	}
	access := random(32)
	p.mu.Lock()
	if p.tokens == nil {
		p.tokens = map[string]*grant{}
	}
	p.purge(now)
	if len(p.tokens) < maxPending {
		t := *g
		t.expires = now.Add(tokenTTL)
		p.tokens[hash(access)] = &t
	}
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(tokenTTL / time.Second),
		"id_token":     idToken,
		"scope":        strings.Join(g.scopes, " "),
	})
}

// pkce : BASE64URL(SHA256(verifier)), RFC 7636.
func pkce(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return b64(sum[:])
}

// claims : ce que le service apprend du compte, selon les scopes demandés.
func (p *Provider) claims(account auth.Account, scopes []string) map[string]any {
	claims := map[string]any{"sub": Subject(account)}
	if slices.Contains(scopes, "profile") {
		claims["preferred_username"] = account.Name
		claims["name"] = account.Name
	}
	if slices.Contains(scopes, "email") && account.Email != "" {
		claims["email"] = account.Email
		claims["email_verified"] = account.EmailVerified
	}
	if slices.Contains(scopes, "groups") {
		claims["groups"] = []string{string(account.Role)}
	}
	return claims
}

// userinfo : le compte du jeton d'accès, s'il est toujours actif.
func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="pi-appliance"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	now := p.now()
	p.mu.Lock()
	g, found := p.tokens[hash(token)]
	p.mu.Unlock()
	var account auth.Account
	var err error
	if found {
		account, err = p.Accounts.Get(g.account)
	}
	client, known := Client{}, false
	if found {
		client, known = p.Clients.Get(g.client)
	}
	if !found || now.After(g.expires) || err != nil || !known || !p.active(account, client.Service, now) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="pi-appliance", error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	writeJSON(w, http.StatusOK, p.claims(account, g.scopes))
}
