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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	zoidc "github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

const (
	requestTTL = 10 * time.Minute
	codeTTL    = 2 * time.Minute
	tokenTTL   = time.Hour
	maxPending = 10000
	maxQuery   = 4096
)

var (
	ErrRequest    = errors.New("demande de connexion inconnue ou expirée")
	ErrNotAllowed = errors.New("ce compte n'a pas accès à ce service")

	challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	paramPattern     = regexp.MustCompile(`^[\x20-\x7e]{0,512}$`)
)

type Accounts interface {
	Get(name string) (auth.Account, error)
}

type Provider struct {
	Issuer   func() string
	Signer   *Signer
	Clients  *Clients
	Accounts Accounts
	Allowed  func(account auth.Account, service string) bool
	LoginURL func(id string) string
	Now      func() time.Time

	once    sync.Once
	op      *op.Provider
	opErr   error
	storage *storage

	mu       sync.Mutex
	requests map[string]*authRequest
	codes    map[string]string
	tokens   map[string]*accessToken
}

func (p *Provider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func Subject(account auth.Account) string {
	sum := sha256.Sum256([]byte(account.Name + "|" + account.CreatedAt.UTC().Format(time.RFC3339Nano)))
	return b64(sum[:18])
}

func (p *Provider) setup() error {
	p.once.Do(func() {
		config := &op.Config{
			CodeMethodS256:  true,
			AuthMethodPost:  true,
			SupportedScopes: []string{"openid", "profile", "email", "groups"},
			SupportedClaims: []string{"sub", "iss", "aud", "exp", "iat", "auth_time", "nonce", "azp", "at_hash", "c_hash", "preferred_username", "name", "email", "email_verified", "groups"},
		}
		// NE PAS journaliser ni écrire cette clé : elle chiffre codes et jetons d'accès.
		if _, err := rand.Read(config.CryptoKey[:]); err != nil {
			p.opErr = err
			return
		}
		p.storage = &storage{p: p}
		issuer := func(bool) (op.IssuerFromRequest, error) {
			return func(*http.Request) string { return p.Issuer() }, nil
		}
		p.op, p.opErr = op.NewProvider(config, p.storage, issuer,
			op.WithCustomTokenEndpoint(op.NewEndpoint("token")),
			op.WithCustomKeysEndpoint(op.NewEndpoint("jwks")),
			op.WithCORSOptions(nil),
		)
	})
	return p.opErr
}

func (p *Provider) Routes(mux *http.ServeMux, base string) {
	if err := p.setup(); err != nil {
		log.Printf("OpenID Connect : fournisseur non démarré : %v", err)
		return
	}
	// NE PAS monter p.op en entier : il ouvrirait introspection, révocation, déconnexion et authorize/callback.
	library := http.StripPrefix(base, p.op)
	mux.HandleFunc("GET "+base+"/.well-known/openid-configuration", p.discovery)
	mux.Handle("GET "+base+"/jwks", library)
	mux.HandleFunc("GET "+base+"/authorize", func(w http.ResponseWriter, r *http.Request) { p.authorize(w, r, library) })
	mux.HandleFunc("POST "+base+"/token", func(w http.ResponseWriter, r *http.Request) { p.token(w, r, library) })
	userinfo := func(w http.ResponseWriter, r *http.Request) { p.userinfo(w, r, library) }
	mux.HandleFunc("GET "+base+"/userinfo", userinfo)
	mux.HandleFunc("POST "+base+"/userinfo", userinfo)
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(value)
}

func (p *Provider) discovery(w http.ResponseWriter, r *http.Request) {
	ctx := op.ContextWithIssuer(r.Context(), p.Issuer())
	config := op.CreateDiscoveryConfig(ctx, p.op, p.storage)
	config.IntrospectionEndpoint = ""
	config.RevocationEndpoint = ""
	config.EndSessionEndpoint = ""
	config.DeviceAuthorizationEndpoint = ""
	config.CheckSessionIframe = ""
	config.IntrospectionEndpointAuthMethodsSupported = nil
	config.IntrospectionEndpointAuthSigningAlgValuesSupported = nil
	config.RevocationEndpointAuthMethodsSupported = nil
	config.RevocationEndpointAuthSigningAlgValuesSupported = nil
	config.TokenEndpointAuthSigningAlgValuesSupported = nil
	config.RequestObjectSigningAlgValuesSupported = nil
	config.RequestParameterSupported = false
	config.ResponseTypesSupported = []string{string(zoidc.ResponseTypeCode)}
	config.ResponseModesSupported = []string{string(zoidc.ResponseModeQuery)}
	config.GrantTypesSupported = []zoidc.GrantType{zoidc.GrantTypeCode}
	config.CodeChallengeMethodsSupported = []zoidc.CodeChallengeMethod{zoidc.CodeChallengeMethodS256}
	config.TokenEndpointAuthMethodsSupported = []zoidc.AuthMethod{zoidc.AuthMethodBasic, zoidc.AuthMethodPost}
	writeJSON(w, http.StatusOK, config)
}

type capture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newCapture() *capture { return &capture{header: http.Header{}} }

func (c *capture) Header() http.Header { return c.header }
func (c *capture) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}
func (c *capture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *capture) send(w http.ResponseWriter) {
	for k, v := range c.header {
		w.Header()[k] = v
	}
	if c.status == 0 {
		c.status = http.StatusOK
	}
	w.WriteHeader(c.status)
	w.Write(c.body.Bytes())
}

type ctxKey int

const requestURIKey ctxKey = 0

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request, library http.Handler) {
	w.Header().Set("Cache-Control", "no-store")
	if len(r.URL.RawQuery) > maxQuery {
		http.Error(w, "Demande de connexion refusée : elle est trop longue.", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("request_uri") != "" {
		r = r.WithContext(context.WithValue(r.Context(), requestURIKey, true))
	}
	c := newCapture()
	library.ServeHTTP(c, r)
	// NE PAS renvoyer le texte de la bibliothèque : page vue par la personne, en anglais technique.
	if c.status >= 400 && c.header.Get("Location") == "" {
		http.Error(w, "Demande de connexion refusée : ce service n'est pas enregistré auprès de la console, ou sa demande est incomplète.", http.StatusBadRequest)
		return
	}
	c.send(w)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request, library http.Handler) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	// NE PAS retirer : la bibliothèque accepte toujours jwt-bearer, sans le dire dans la découverte.
	if r.PostForm.Get("grant_type") != string(zoidc.GrantTypeCode) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		return
	}
	if _, _, basic := r.BasicAuth(); basic && r.PostForm.Get("client_secret") != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	if r.PostForm.Get("client_assertion") != "" || r.PostForm.Get("client_assertion_type") != "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	c := newCapture()
	library.ServeHTTP(c, r)
	if c.status == http.StatusUnauthorized {
		c.header.Set("WWW-Authenticate", `Basic realm="pi-appliance"`)
	}
	c.send(w)
}

func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request, library http.Handler) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	w.Header().Set("Cache-Control", "no-store")
	c := newCapture()
	library.ServeHTTP(c, r)
	if c.status != http.StatusOK {
		w.Header().Set("WWW-Authenticate", `Bearer realm="pi-appliance", error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	c.send(w)
}

func (p *Provider) Pending(id string) (string, bool) {
	p.mu.Lock()
	req, ok := p.requests[id]
	ok = ok && !req.done && !p.now().After(req.expires)
	p.mu.Unlock()
	if !ok {
		return "", false
	}
	client, ok := p.Clients.Get(req.ClientID)
	return client.Service, ok
}

func (p *Provider) Approve(id string, account auth.Account) (string, error) {
	if err := p.setup(); err != nil {
		return "", err
	}
	now := p.now()
	p.mu.Lock()
	req, ok := p.requests[id]
	if !ok || req.done || now.After(req.expires) {
		// NE PAS supprimer une demande déjà acceptée : son code attend l'échange.
		if ok && !req.done {
			delete(p.requests, id)
		}
		p.mu.Unlock()
		return "", ErrRequest
	}
	client, known := p.Clients.Get(req.ClientID)
	if !known || !p.active(account, client.Service, now) {
		delete(p.requests, id)
		p.mu.Unlock()
		if !known {
			return "", ErrRequest
		}
		return "", ErrNotAllowed
	}
	req.done = true
	req.account = account.Name
	req.subject = Subject(account)
	req.authTime = now
	req.expires = now.Add(codeTTL)
	p.mu.Unlock()
	issuer := p.Issuer()
	ctx := op.ContextWithIssuer(context.Background(), issuer)
	back, err := op.BuildAuthResponseCallbackURL(ctx, req, p.op)
	if err != nil {
		p.mu.Lock()
		delete(p.requests, id)
		p.mu.Unlock()
		return "", ErrRequest
	}
	target, err := url.Parse(back)
	if err != nil {
		return "", ErrRequest
	}
	values := target.Query()
	values.Set("iss", issuer)
	target.RawQuery = values.Encode()
	return target.String(), nil
}

func (p *Provider) active(account auth.Account, service string, now time.Time) bool {
	if account.Name == "" || account.Hash == "" || account.Expired(now) {
		return false
	}
	return p.Allowed == nil || p.Allowed(account, service)
}

func (p *Provider) activeAccount(name, clientID string, now time.Time) (auth.Account, bool) {
	account, err := p.Accounts.Get(name)
	if err != nil {
		return auth.Account{}, false
	}
	client, ok := p.Clients.Get(clientID)
	if !ok || !p.active(account, client.Service, now) {
		return auth.Account{}, false
	}
	return account, true
}

func fill(info *zoidc.UserInfo, account auth.Account, scopes []string) {
	info.Subject = Subject(account)
	for _, scope := range scopes {
		switch scope {
		case zoidc.ScopeProfile:
			info.PreferredUsername = account.Name
			info.Name = account.Name
		case zoidc.ScopeEmail:
			if account.Email != "" {
				info.Email = account.Email
				info.EmailVerified = zoidc.Bool(account.EmailVerified)
			}
		case "groups":
			info.AppendClaims("groups", []string{string(account.Role)})
		}
	}
}

func pkceOK(challenge string, method zoidc.CodeChallengeMethod) bool {
	return method == zoidc.CodeChallengeMethodS256 && challengePattern.MatchString(challenge)
}

func (p *Provider) purge(now time.Time) {
	for id, r := range p.requests {
		if now.After(r.expires) {
			delete(p.requests, id)
		}
	}
	for code, id := range p.codes {
		if _, ok := p.requests[id]; !ok {
			delete(p.codes, code)
		}
	}
	for id, t := range p.tokens {
		if now.After(t.expires) {
			delete(p.tokens, id)
		}
	}
}
