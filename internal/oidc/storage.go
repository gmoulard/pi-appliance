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
	"context"
	"errors"
	"slices"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	zoidc "github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
)

var errUnsupported = errors.New("non pris en charge par la console")

type authRequest struct {
	ID          string
	ClientID    string
	RedirectURI string
	State       string
	Nonce       string
	Challenge   string
	Scopes      []string
	expires     time.Time

	done     bool
	account  string
	subject  string
	authTime time.Time
	coded    bool
}

func (a *authRequest) GetID() string                       { return a.ID }
func (a *authRequest) GetACR() string                      { return "" }
func (a *authRequest) GetAMR() []string                    { return nil }
func (a *authRequest) GetAudience() []string               { return []string{a.ClientID} }
func (a *authRequest) GetAuthTime() time.Time              { return a.authTime }
func (a *authRequest) GetClientID() string                 { return a.ClientID }
func (a *authRequest) GetNonce() string                    { return a.Nonce }
func (a *authRequest) GetRedirectURI() string              { return a.RedirectURI }
func (a *authRequest) GetResponseType() zoidc.ResponseType { return zoidc.ResponseTypeCode }
func (a *authRequest) GetResponseMode() zoidc.ResponseMode { return zoidc.ResponseModeQuery }
func (a *authRequest) GetScopes() []string                 { return a.Scopes }
func (a *authRequest) GetState() string                    { return a.State }
func (a *authRequest) GetSubject() string                  { return a.subject }
func (a *authRequest) Done() bool                          { return a.done }
func (a *authRequest) GetCodeChallenge() *zoidc.CodeChallenge {
	return &zoidc.CodeChallenge{Challenge: a.Challenge, Method: zoidc.CodeChallengeMethodS256}
}

type accessToken struct {
	account  string
	subject  string
	clientID string
	scopes   []string
	expires  time.Time
}

type client struct{ Client }

func (c client) GetID() string                       { return c.ID }
func (c client) RedirectURIs() []string              { return slices.Clone(c.Client.RedirectURIs) }
func (c client) PostLogoutRedirectURIs() []string    { return nil }
func (c client) ApplicationType() op.ApplicationType { return op.ApplicationTypeWeb }
func (c client) AuthMethod() zoidc.AuthMethod        { return zoidc.AuthMethodBasic }
func (c client) ResponseTypes() []zoidc.ResponseType {
	return []zoidc.ResponseType{zoidc.ResponseTypeCode}
}
func (c client) GrantTypes() []zoidc.GrantType        { return []zoidc.GrantType{zoidc.GrantTypeCode} }
func (c client) AccessTokenType() op.AccessTokenType  { return op.AccessTokenTypeBearer }
func (c client) IDTokenLifetime() time.Duration       { return tokenTTL }
func (c client) DevMode() bool                        { return false }
func (c client) IsScopeAllowed(scope string) bool     { return scope == "groups" }
func (c client) IDTokenUserinfoClaimsAssertion() bool { return true }
func (c client) ClockSkew() time.Duration             { return 0 }
func (c client) LoginURL(string) string               { return "" }
func (c client) RestrictAdditionalIdTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}
func (c client) RestrictAdditionalAccessTokenScopes() func([]string) []string {
	return func(scopes []string) []string { return scopes }
}

type loginClient struct {
	client
	login func(string) string
}

func (c loginClient) LoginURL(id string) string { return c.login(id) }

type storage struct{ p *Provider }

func (s *storage) Health(context.Context) error { return nil }

func (s *storage) GetClientByClientID(_ context.Context, id string) (op.Client, error) {
	found, ok := s.p.Clients.Get(id)
	if !ok {
		return nil, errors.New("client inconnu")
	}
	return loginClient{client: client{found}, login: s.p.LoginURL}, nil
}

func (s *storage) AuthorizeClientIDSecret(_ context.Context, id, secret string) error {
	if _, ok := s.p.Clients.Authenticate(id, secret); !ok {
		return errors.New("secret refusé")
	}
	return nil
}

func (s *storage) CreateAuthRequest(ctx context.Context, req *zoidc.AuthRequest, _ string) (op.AuthRequest, error) {
	for _, value := range []string{req.State, req.Nonce, req.Scopes.String()} {
		if !paramPattern.MatchString(value) {
			return nil, zoidc.ErrInvalidRequest()
		}
	}
	switch {
	case ctx.Value(requestURIKey) != nil:
		return nil, zoidc.ErrRequestNotSupported().WithDescription("request_uri not supported")
	case !slices.Contains(req.Scopes, zoidc.ScopeOpenID):
		return nil, zoidc.ErrInvalidScope()
	case !pkceOK(req.CodeChallenge, req.CodeChallengeMethod):
		return nil, zoidc.ErrInvalidRequest().WithDescription("PKCE S256 required")
	case req.ResponseMode != "" && req.ResponseMode != zoidc.ResponseModeQuery:
		return nil, zoidc.ErrInvalidRequest().WithDescription("response_mode not supported")
	}
	p := s.p
	now := p.now()
	a := &authRequest{ID: random(32), ClientID: req.ClientID, RedirectURI: req.RedirectURI, State: req.State, Nonce: req.Nonce,
		Challenge: req.CodeChallenge, Scopes: slices.Clone([]string(req.Scopes)), expires: now.Add(requestTTL)}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.requests == nil {
		p.requests = map[string]*authRequest{}
	}
	p.purge(now)
	if len(p.requests) >= maxPending {
		return nil, zoidc.ErrServerError().WithDescription("too many pending requests")
	}
	p.requests[a.ID] = a
	return a, nil
}

func (s *storage) AuthRequestByID(_ context.Context, id string) (op.AuthRequest, error) {
	p := s.p
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.requests[id]
	if !ok || a.coded || p.now().After(a.expires) {
		return nil, ErrRequest
	}
	return a, nil
}

func (s *storage) SaveAuthCode(_ context.Context, id, code string) error {
	p := s.p
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.requests[id]
	if !ok || !a.done || a.coded || p.now().After(a.expires) {
		return ErrRequest
	}
	if p.codes == nil {
		p.codes = map[string]string{}
	}
	if len(p.codes) >= maxPending {
		return ErrRequest
	}
	a.coded = true
	p.codes[hash(code)] = id
	return nil
}

func (s *storage) AuthRequestByCode(_ context.Context, code string) (op.AuthRequest, error) {
	p := s.p
	now := p.now()
	p.mu.Lock()
	key := hash(code)
	id, ok := p.codes[key]
	// NE PAS garder le code après sa lecture : il ne sert qu'une fois, même si l'échange échoue.
	delete(p.codes, key)
	a, found := p.requests[id]
	delete(p.requests, id)
	p.mu.Unlock()
	if !ok || !found || !a.done || !a.coded || now.After(a.expires) {
		return nil, ErrRequest
	}
	if _, active := p.activeAccount(a.account, a.ClientID, now); !active {
		return nil, ErrNotAllowed
	}
	return a, nil
}

func (s *storage) DeleteAuthRequest(_ context.Context, id string) error {
	s.p.mu.Lock()
	delete(s.p.requests, id)
	s.p.mu.Unlock()
	return nil
}

func (s *storage) CreateAccessToken(_ context.Context, request op.TokenRequest) (string, time.Time, error) {
	a, ok := request.(*authRequest)
	if !ok {
		return "", time.Time{}, errUnsupported
	}
	p := s.p
	now := p.now()
	id := random(32)
	expires := now.Add(tokenTTL)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tokens == nil {
		p.tokens = map[string]*accessToken{}
	}
	p.purge(now)
	if len(p.tokens) >= maxPending {
		return "", time.Time{}, zoidc.ErrServerError().WithDescription("too many pending requests")
	}
	p.tokens[hash(id)] = &accessToken{account: a.account, subject: a.subject, clientID: a.ClientID, scopes: slices.Clone(a.Scopes), expires: expires}
	return id, expires, nil
}

func (s *storage) CreateAccessAndRefreshTokens(context.Context, op.TokenRequest, string) (string, string, time.Time, error) {
	return "", "", time.Time{}, errUnsupported
}

func (s *storage) TokenRequestByRefreshToken(context.Context, string) (op.RefreshTokenRequest, error) {
	return nil, op.ErrInvalidRefreshToken
}

func (s *storage) TerminateSession(context.Context, string, string) error { return errUnsupported }

func (s *storage) RevokeToken(context.Context, string, string, string) *zoidc.Error {
	return zoidc.ErrInvalidRequest().WithDescription("revocation not supported")
}

func (s *storage) GetRefreshTokenInfo(context.Context, string, string) (string, string, error) {
	return "", "", op.ErrInvalidRefreshToken
}

func (s *storage) SigningKey(context.Context) (op.SigningKey, error) {
	return signingKey{s.p.Signer}, nil
}

func (s *storage) SignatureAlgorithms(context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.ES256}, nil
}

func (s *storage) KeySet(context.Context) ([]op.Key, error) {
	return []op.Key{publicKey{s.p.Signer}}, nil
}

func (s *storage) SetUserinfoFromScopes(_ context.Context, info *zoidc.UserInfo, subject, _ string, _ []string) error {
	info.Subject = subject
	return nil
}

func (s *storage) SetUserinfoFromRequest(_ context.Context, info *zoidc.UserInfo, request op.IDTokenRequest, scopes []string) error {
	a, ok := request.(*authRequest)
	if !ok {
		return errUnsupported
	}
	account, active := s.p.activeAccount(a.account, a.ClientID, s.p.now())
	if !active {
		return ErrNotAllowed
	}
	fill(info, account, scopes)
	return nil
}

func (s *storage) SetUserinfoFromToken(_ context.Context, info *zoidc.UserInfo, tokenID, subject, _ string) error {
	p := s.p
	now := p.now()
	p.mu.Lock()
	t, ok := p.tokens[hash(tokenID)]
	p.mu.Unlock()
	if !ok || tokenID == "" || t.subject != subject || now.After(t.expires) {
		return ErrRequest
	}
	account, active := p.activeAccount(t.account, t.clientID, now)
	if !active || Subject(account) != t.subject {
		return ErrNotAllowed
	}
	fill(info, account, t.scopes)
	return nil
}

func (s *storage) SetIntrospectionFromToken(context.Context, *zoidc.IntrospectionResponse, string, string, string) error {
	return errUnsupported
}

func (s *storage) GetPrivateClaimsFromScopes(context.Context, string, string, []string) (map[string]any, error) {
	return nil, nil
}

func (s *storage) GetKeyByIDAndClientID(context.Context, string, string) (*jose.JSONWebKey, error) {
	return nil, errUnsupported
}

func (s *storage) ValidateJWTProfileScopes(context.Context, string, []string) ([]string, error) {
	return nil, errUnsupported
}
