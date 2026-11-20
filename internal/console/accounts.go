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
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// Comptes, double authentification et authentification déléguée (spécification
// 8.1, 8.3) : le fournisseur d'identité intégré à la console.

// forwardCookie : cookie de la session déléguée, posé sur l'adresse de
// l'appliance et vu par ses sous-domaines (les services). Il n'ouvre pas la console.
const forwardCookie = "pi_forward"

// AccountRequest : nouveau compte, ou changement d'un compte existant (le nom et
// l'e-mail ne changent pas). Expires : fin d'un compte invité.
type AccountRequest struct {
	Name         string     `json:"name"`
	Email        string     `json:"email"`
	Role         auth.Role  `json:"role"`
	Expires      *time.Time `json:"expires"`
	TOTPRequired bool       `json:"totp_required"`
}

// InvitationReply : le lien à transmettre quand l'appliance ne peut pas encore
// envoyer d'e-mail (Sent faux).
type InvitationReply struct {
	Account auth.Public `json:"account"`
	Link    string      `json:"link,omitempty"`
	Sent    bool        `json:"sent"`
}

type InvitationRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type CodeRequest struct {
	Code string `json:"code"`
}

type PasswordRequest struct {
	Password string `json:"password"`
}

// PasswordChangeRequest : son propre mot de passe, l'actuel puis le nouveau.
type PasswordChangeRequest struct {
	Current  string `json:"current"`
	Password string `json:"password"`
}

// PasswordChangeReply : Backups vrai si ce mot de passe protège aussi les
// sauvegardes : la console rappelle que celles déjà faites gardent l'ancien.
type PasswordChangeReply struct {
	Account auth.Public `json:"account"`
	Backups bool        `json:"backups"`
}

// ForwardRequest : l'adresse du service où retourner une fois connecté.
type ForwardRequest struct {
	Return string `json:"return"`
}

func (c *console) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+BasePath+"/api/accounts", c.adminOnly(c.listAccounts))
	mux.HandleFunc("POST "+BasePath+"/api/accounts", c.adminOnly(c.createAccount))
	mux.HandleFunc("PUT "+BasePath+"/api/accounts/{name}", c.adminOnly(c.updateAccount))
	mux.HandleFunc("DELETE "+BasePath+"/api/accounts/{name}", c.adminOnly(c.deleteAccount))
	mux.HandleFunc("POST "+BasePath+"/api/accounts/{name}/invitation", c.adminOnly(c.reinvite))
	mux.HandleFunc("DELETE "+BasePath+"/api/accounts/{name}/totp", c.adminOnly(c.resetTOTP))
	mux.HandleFunc("POST "+BasePath+"/api/invitation/check", c.checkInvitation)
	mux.HandleFunc("POST "+BasePath+"/api/invitation", c.acceptInvitation)
	mux.HandleFunc("POST "+BasePath+"/api/account/totp", c.ownAccount(c.beginTOTP))
	mux.HandleFunc("POST "+BasePath+"/api/account/totp/confirm", c.ownAccount(c.confirmTOTP))
	mux.HandleFunc("POST "+BasePath+"/api/account/totp/disable", c.ownAccount(c.disableTOTP))
	mux.HandleFunc("POST "+BasePath+"/api/account/password", c.ownAccount(c.changePassword))
	mux.HandleFunc("POST "+BasePath+"/api/auth/return", c.signedIn(c.forwardReturn))
	mux.HandleFunc("GET "+BasePath+"/api/auth/forward", c.forward)
	mux.HandleFunc("GET "+BasePath+"/api/auth/forward-admin", c.forwardAdmin)
}

// ownAccount : réglages de son propre compte, permis même quand la double
// authentification exigée n'est pas encore activée (c'est là qu'on l'active).
func (c *console) ownAccount(next func(http.ResponseWriter, *http.Request, auth.Account)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, ok := c.account(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Connectez-vous pour continuer.")
			return
		}
		next(w, r, account)
	}
}

func (c *console) listAccounts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"accounts": c.Accounts.List()})
}

func (c *console) createAccount(w http.ResponseWriter, r *http.Request) {
	var request AccountRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, token, err := c.Accounts.Create(request.Name, request.Email, request.Role, request.Expires)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if request.TOTPRequired {
		if account, err = c.Accounts.Update(account.Name, account.Role, account.ExpiresAt, true); err != nil {
			writeAuthError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, c.invitationReply(r, account, token))
}

func (c *console) reinvite(w http.ResponseWriter, r *http.Request) {
	account, token, err := c.Accounts.Invite(r.PathValue("name"))
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c.invitationReply(r, account, token))
}

// invitationReply envoie le lien par e-mail si l'appliance le peut, sinon le
// renvoie à l'administrateur. Le jeton est après # : il ne part ni dans les
// journaux de Traefik ni dans l'en-tête Referer.
func (c *console) invitationReply(r *http.Request, account auth.Account, token string) InvitationReply {
	scheme := "http"
	if secure(r) {
		scheme = "https"
	}
	link := scheme + "://" + r.Host + BasePath + "/#invite=" + token
	reply := InvitationReply{Account: account.Public()}
	if c.Mailer != nil {
		if err := c.Mailer.Invite(account.Public(), link); err == nil {
			reply.Sent = true
			return reply
		} else {
			log.Printf("invitation de %s : %v", account.Name, err)
		}
	}
	reply.Link = link
	return reply
}

func (c *console) updateAccount(w http.ResponseWriter, r *http.Request) {
	var request AccountRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.Update(r.PathValue("name"), request.Role, request.Expires, request.TOTPRequired)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) deleteAccount(w http.ResponseWriter, r *http.Request) {
	if me, _ := c.account(r); me.Name == r.PathValue("name") {
		writeError(w, http.StatusConflict, "Vous ne pouvez pas supprimer votre propre compte.")
		return
	}
	if err := c.Accounts.Delete(r.PathValue("name")); err != nil {
		writeAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *console) resetTOTP(w http.ResponseWriter, r *http.Request) {
	account, err := c.Accounts.ResetTOTP(r.PathValue("name"))
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) checkInvitation(w http.ResponseWriter, r *http.Request) {
	var request InvitationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.Invitation(request.Token)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var request InvitationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.AcceptInvitation(request.Token, request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if account.TOTPSecret != "" {
		// Nouveau mot de passe d'un compte déjà protégé : connexion normale, avec le code.
		writeJSON(w, http.StatusOK, account.Public())
		return
	}
	openSession(w, r, c.Accounts.StartSession(account.Name))
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) beginTOTP(w http.ResponseWriter, r *http.Request, account auth.Account) {
	setup, err := c.Accounts.BeginTOTP(account.Name)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

func (c *console) confirmTOTP(w http.ResponseWriter, r *http.Request, account auth.Account) {
	var request CodeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	codes, err := c.Accounts.ConfirmTOTP(account.Name, request.Code)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (c *console) disableTOTP(w http.ResponseWriter, r *http.Request, account auth.Account) {
	var request PasswordRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := c.Accounts.DisableTOTP(account.Name, request.Password); err != nil {
		writeAuthError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *console) changePassword(w http.ResponseWriter, r *http.Request, account auth.Account) {
	var request PasswordChangeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	token, account, err := c.Accounts.ChangePassword(account.Name, request.Current, request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	backups := c.Backups != nil && account.Role == auth.RoleAdmin && c.Accounts.FirstAdmin(account.Name)
	if backups {
		c.protectBackups(request.Password, false)
	}
	openSession(w, r, token)
	writeJSON(w, http.StatusOK, PasswordChangeReply{Account: account.Public(), Backups: backups})
}

// forwardReturn : la personne, connectée à la console, voulait ouvrir un service
// protégé. La console pose le cookie de la session déléguée sur sa propre adresse
// (donc aussi pour ses sous-domaines) et renvoie l'adresse du service, si elle est
// bien une adresse de l'appliance : jamais de redirection vers un autre site.
func (c *console) forwardReturn(w http.ResponseWriter, r *http.Request) {
	var request ForwardRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	host := requestHost(r)
	target, ok := returnURL(request.Return, host, secure(r))
	if !ok {
		writeError(w, http.StatusBadRequest, "Cette adresse n'est pas celle d'un service de l'appliance.")
		return
	}
	account, _ := c.account(r)
	http.SetCookie(w, &http.Cookie{
		Name:     forwardCookie,
		Value:    c.Accounts.StartForwardSession(account.Name),
		Domain:   cookieDomain(host),
		Path:     "/",
		MaxAge:   int(auth.SessionLifetime / time.Second),
		HttpOnly: true,
		Secure:   secure(r),
		// Lax : le cookie suit un lien venu d'un autre site vers un service.
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"url": target})
}

// forward : authentification déléguée de Traefik (middleware forwardAuth
// pi-appliance-auth). 200 avec Remote-User, Remote-Email et Remote-Groups pour une
// session déléguée valide ; sinon redirection vers la connexion de la console
// (navigation) ou 401. Seul Traefik (bouclage ou réseau Docker) peut l'appeler.
func (c *console) forward(w http.ResponseWriter, r *http.Request) {
	c.forwardFor(w, r, false)
}

// forwardAdmin : comme forward, réservé aux administrateurs (middleware
// pi-appliance-admin, administration du système par Cockpit) ; 403 pour une
// autre personne connectée.
func (c *console) forwardAdmin(w http.ResponseWriter, r *http.Request) {
	c.forwardFor(w, r, true)
}

func (c *console) forwardFor(w http.ResponseWriter, r *http.Request, adminOnly bool) {
	if !trustedProxy(r) {
		writeError(w, http.StatusForbidden, "Réservé à Traefik.")
		return
	}
	if cookie, err := r.Cookie(forwardCookie); err == nil {
		if account, ok := c.Accounts.ForwardSession(cookie.Value); ok {
			if adminOnly && account.Role != auth.RoleAdmin {
				writeError(w, http.StatusForbidden, "Réservé aux administrateurs.")
				return
			}
			w.Header().Set("Remote-User", account.Name)
			w.Header().Set("Remote-Email", account.Email)
			w.Header().Set("Remote-Groups", string(account.Role))
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	method := r.Header.Get("X-Forwarded-Method")
	host := r.Header.Get("X-Forwarded-Host")
	if (method != "" && method != http.MethodGet && method != http.MethodHead) || host == "" || strings.ContainsAny(host, "/\\@ ") {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	scheme := "https"
	if r.Header.Get("X-Forwarded-Proto") == "http" {
		scheme = "http"
	}
	uri := r.Header.Get("X-Forwarded-Uri")
	if !strings.HasPrefix(uri, "/") {
		uri = "/"
	}
	original := scheme + "://" + host + uri
	// Un service a son sous-domaine, la console est sur l'adresse parente ;
	// Cockpit (adminOnly) est servi sous l'adresse même de la console.
	consoleHost := parentHost(host)
	if adminOnly {
		consoleHost = host
	}
	login := scheme + "://" + consoleHost + BasePath + "/?return=" + url.QueryEscape(original)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, login, http.StatusFound)
}

// requestHost : l'adresse demandée par le navigateur, sans le port.
func requestHost(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// returnURL accepte l'adresse de la console ou d'un de ses sous-domaines, dans le
// même schéma que la console (https derrière Traefik).
func returnURL(raw, host string, secure bool) (string, bool) {
	target, err := url.Parse(raw)
	if err != nil || target.User != nil || target.Opaque != "" {
		return "", false
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	targetHost := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if target.Scheme != scheme || host == "" || targetHost == "" {
		return "", false
	}
	if targetHost != host && (net.ParseIP(host) != nil || !strings.HasSuffix(targetHost, "."+host)) {
		return "", false
	}
	return target.String(), true
}

// cookieDomain : le cookie délégué vaut pour l'adresse et ses sous-domaines,
// sauf pour une adresse IP ou un nom sans point (cookie limité à cette adresse).
func cookieDomain(host string) string {
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return ""
	}
	return host
}

// parentHost : cloud.maison.example.org → maison.example.org, où répond la console.
func parentHost(host string) string {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name, port = host, ""
	}
	if net.ParseIP(name) == nil {
		if _, parent, ok := strings.Cut(name, "."); ok && strings.Contains(parent, ".") {
			name = parent
		}
	}
	if port != "" {
		return net.JoinHostPort(name, port)
	}
	return name
}

// clearForwardCookie efface le cookie délégué à la déconnexion et ferme sa session.
func (c *console) clearForwardCookie(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(forwardCookie); err == nil {
		c.Accounts.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: forwardCookie, Value: "", Domain: cookieDomain(requestHost(r)), Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteLaxMode})
}

// accountError : messages des erreurs propres aux comptes ; faux si l'erreur n'en est pas une.
func accountError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, auth.ErrCodeNeeded):
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Entrez le code de vérification affiché par votre application d'authentification.", "code_needed": true})
	case errors.Is(err, auth.ErrCode):
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "Code de vérification incorrect : entrez le code affiché en ce moment, ou un code de secours.", "code_needed": true})
	case errors.Is(err, auth.ErrExpired):
		writeError(w, http.StatusForbidden, "Ce compte invité a expiré : demandez à l'administrateur de le prolonger.")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, "Ce compte n'existe pas.")
	case errors.Is(err, auth.ErrExists):
		writeError(w, http.StatusConflict, "Un compte porte déjà cet identifiant.")
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusConflict, "C'est le dernier administrateur : nommez d'abord un autre administrateur.")
	case errors.Is(err, auth.ErrNoInvitation):
		writeError(w, http.StatusGone, "Ce lien ne marche plus : demandez-en un nouveau à l'administrateur.")
	case errors.Is(err, auth.ErrTOTPRequired):
		writeError(w, http.StatusConflict, "L'administrateur exige la double authentification pour ce compte.")
	default:
		return false
	}
	return true
}
