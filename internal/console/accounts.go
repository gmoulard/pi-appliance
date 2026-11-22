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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
)

// Comptes, double authentification et authentification déléguée (spécification
// 8.1, 8.3) : le fournisseur d'identité intégré à la console.

// forwardCookie : cookie de la session déléguée, posé par chaque service
// protégé sur sa propre adresse (loginCallback). Il n'ouvre pas la console.
// stateCookie : posé par le service avant la connexion à la console, il lie le
// code de la console à ce navigateur (loginParam : le code dans l'adresse).
const (
	forwardCookie = "pi_forward"
	stateCookie   = "pi_login_state"
	loginParam    = "pi_login"
	stateLifetime = 600
)

var statePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

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
// State : le paramètre state de l'adresse de connexion (forwardLogin).
type ForwardRequest struct {
	Return string `json:"return"`
	State  string `json:"state"`
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
	mux.HandleFunc("POST "+BasePath+"/api/password/forgot", c.forgotPassword)
	mux.HandleFunc("POST "+BasePath+"/api/password/check", c.checkResetLink)
	mux.HandleFunc("POST "+BasePath+"/api/password/reset", c.useResetLink)
	mux.HandleFunc("POST "+BasePath+"/api/account/totp", c.ownAccount(c.beginTOTP))
	mux.HandleFunc("POST "+BasePath+"/api/account/totp/confirm", c.ownAccount(c.confirmTOTP))
	mux.HandleFunc("POST "+BasePath+"/api/account/totp/disable", c.ownAccount(c.disableTOTP))
	mux.HandleFunc("POST "+BasePath+"/api/account/password", c.ownAccount(c.changePassword))
	mux.HandleFunc("POST "+BasePath+"/api/auth/return", c.signedIn(c.forwardReturn))
	mux.HandleFunc("GET "+BasePath+"/api/auth/forward", c.forward)
	mux.HandleFunc("GET "+BasePath+"/api/auth/forward-admin", c.forwardAdmin)
	mux.HandleFunc("GET "+BasePath+"/api/auth/forward-service-admin", c.forwardServiceAdmin)
	mux.HandleFunc("GET "+BasePath+"/api/auth/forward-pad", c.forwardPad)
	mux.HandleFunc("GET "+BasePath+"/api/services/{name}/shares", c.signedIn(c.listShares))
	mux.HandleFunc("POST "+BasePath+"/api/services/{name}/shares", c.signedIn(c.createShare))
	mux.HandleFunc("DELETE "+BasePath+"/api/services/{name}/shares/{id}", c.signedIn(c.revokeShare))
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
	link := scheme + "://" + strings.ToLower(r.Host) + BasePath + "/#invite=" + token
	reply := InvitationReply{Account: account.Public()}
	if c.Mailer != nil {
		if err := c.Mailer.Invite(account.Public(), link, requestLanguage(r, "")); err == nil {
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
// protégé. Les services n'ont pas tous une adresse sous celle de la console
// (<service>-<nom>.local et <nom>.local sont voisins, et aucun cookie ne peut
// valoir pour tout .local) : la console donne donc un code à usage unique, ajouté
// à l'adresse du service, que ce service échange contre son propre cookie
// délégué (loginCallback). Seulement vers la console ou un service de
// l'appliance : jamais de redirection vers un autre site.
func (c *console) forwardReturn(w http.ResponseWriter, r *http.Request) {
	var request ForwardRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	target, protected, ok := returnURL(request.Return, requestHost(r), secure(r), c.serviceHosts())
	if !ok {
		writeError(w, http.StatusBadRequest, "Cette adresse n'est pas celle d'un service de l'appliance.")
		return
	}
	cookie, err := r.Cookie(cookieName)
	if protected && err == nil && statePattern.MatchString(request.State) {
		if code, ok := c.Accounts.StartLoginCode(cookie.Value, hostOf(target), request.State); ok {
			if target.RawQuery != "" {
				target.RawQuery += "&"
			}
			target.RawQuery += loginParam + "=" + code
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": target.String()})
}

// forward : authentification déléguée de Traefik (middleware forwardAuth
// pi-appliance-auth). 200 avec Remote-User, Remote-Email et Remote-Groups pour une
// session déléguée valide ; sinon redirection vers la connexion de la console
// (navigation) ou 401. Seul Traefik (bouclage ou réseau Docker) peut l'appeler.
func (c *console) forward(w http.ResponseWriter, r *http.Request) {
	c.forwardFor(w, r, false, false)
}

// forwardAdmin : comme forward, réservé aux administrateurs (middleware
// pi-appliance-admin, administration du système par Cockpit, servi sous
// l'adresse de la console) ; 403 pour une autre personne connectée.
func (c *console) forwardAdmin(w http.ResponseWriter, r *http.Request) {
	c.forwardFor(w, r, true, true)
}

// forwardServiceAdmin : comme forwardAdmin, pour un service à sa propre
// adresse réservé aux administrateurs (middleware pi-appliance-admins,
// accès « admins » d'un manifeste : Portainer).
func (c *console) forwardServiceAdmin(w http.ResponseWriter, r *http.Request) {
	c.forwardFor(w, r, true, false)
}

func (c *console) forwardFor(w http.ResponseWriter, r *http.Request, adminOnly, onConsoleHost bool) {
	if !trustedProxy(r) {
		writeError(w, http.StatusForbidden, "Réservé à Traefik.")
		return
	}
	if c.loginCallback(w, r) {
		return
	}
	if account, ok := c.forwardAccount(r); ok {
		if adminOnly && account.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "Réservé aux administrateurs.")
			return
		}
		remoteHeaders(w, account)
		w.WriteHeader(http.StatusOK)
		return
	}
	c.forwardLogin(w, r, onConsoleHost)
}

// forwardAccount : le compte d'une session déléguée valide pour l'adresse
// demandée. Tous les cookies pi_forward sont essayés : un ancien cookie posé
// pour tout <nom>.local (versions d'avant TODO-2.61) passe avant celui de
// l'adresse.
func (c *console) forwardAccount(r *http.Request) (auth.Account, bool) {
	host := requestHostOf(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		return auth.Account{}, false
	}
	for _, cookie := range r.CookiesNamed(forwardCookie) {
		if account, ok := c.Accounts.ForwardSession(cookie.Value, host); ok {
			return account, true
		}
	}
	return auth.Account{}, false
}

// loginCallback : retour de la console avec ?pi_login=<code> sur l'adresse du
// service. Le code, à usage unique, valable deux minutes, lié à cette adresse et
// au cookie pi_login_state posé par forwardLogin sur ce navigateur, devient le
// cookie délégué de cette adresse ; le code quitte aussitôt l'adresse. Faux
// sans code : la vérification continue.
func (c *console) loginCallback(w http.ResponseWriter, r *http.Request) bool {
	method := r.Header.Get("X-Forwarded-Method")
	if method != "" && method != http.MethodGet && method != http.MethodHead {
		return false
	}
	path, rawQuery, _ := strings.Cut(r.Header.Get("X-Forwarded-Uri"), "?")
	code, rest, found := takeParam(rawQuery, loginParam)
	forwardedHost := r.Header.Get("X-Forwarded-Host")
	host := requestHostOf(forwardedHost)
	if !found || host == "" {
		return false
	}
	scheme := "https"
	if r.Header.Get("X-Forwarded-Proto") == "http" {
		scheme = "http"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/"
	}
	target := scheme + "://" + forwardedHost + path
	if rest != "" {
		target += "?" + rest
	}
	state := ""
	if cookie, err := r.Cookie(stateCookie); err == nil {
		state = cookie.Value
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if token, ok := c.Accounts.UseLoginCode(code, host, state); ok {
		http.SetCookie(w, &http.Cookie{
			Name:     forwardCookie,
			Value:    token,
			Path:     "/",
			MaxAge:   int(auth.SessionLifetime / time.Second),
			HttpOnly: true,
			Secure:   scheme == "https",
			// Lax : le cookie suit un lien venu d'un autre site vers le service.
			SameSite: http.SameSiteLaxMode,
		})
		http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: scheme == "https", SameSite: http.SameSiteLaxMode})
		// NE PAS renvoyer une adresse relative : Traefik la lirait par rapport à l'adresse de la console.
		http.Redirect(w, r, target, http.StatusFound)
		return true
	}
	if _, ok := c.forwardAccount(r); ok {
		http.Redirect(w, r, target, http.StatusFound)
		return true
	}
	// NE PAS renvoyer vers la console ici : un navigateur qui refuse les cookies
	// tournerait sans fin entre la console et le service.
	messagePage(w, http.StatusForbidden, "pi-appliance", "Cette connexion n'est plus valable : elle ne sert qu'une fois, pendant deux minutes, dans le navigateur qui l'a demandée.", target, "Ouvrir de nouveau la page")
	return true
}

// takeParam retire le paramètre name d'une requête, sans toucher aux autres.
func takeParam(rawQuery, name string) (value, rest string, found bool) {
	var kept []string
	for _, part := range strings.Split(rawQuery, "&") {
		key, raw, _ := strings.Cut(part, "=")
		if key == name && !found {
			value, found = raw, true
			continue
		}
		if part != "" {
			kept = append(kept, part)
		}
	}
	return value, strings.Join(kept, "&"), found
}

func remoteHeaders(w http.ResponseWriter, account auth.Account) {
	w.Header().Set("Remote-User", account.Name)
	w.Header().Set("Remote-Email", account.Email)
	w.Header().Set("Remote-Groups", string(account.Role))
}

// forwardLogin : sans session déléguée, une page mène à la connexion de la
// console, après un cookie pi_login_state propre à ce navigateur et à cette
// adresse (seul ce navigateur pourra échanger le code de la console) ; le reste
// reçoit 401.
func (c *console) forwardLogin(w http.ResponseWriter, r *http.Request, onConsoleHost bool) {
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
	// Cockpit (onConsoleHost) est servi sous l'adresse même de la console.
	consoleHost := host
	if !onConsoleHost {
		consoleHost = c.consoleHost(host)
	}
	state := randomState()
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: state, Path: "/", MaxAge: stateLifetime, HttpOnly: true, Secure: scheme == "https", SameSite: http.SameSiteLaxMode})
	login := scheme + "://" + consoleHost + BasePath + "/?return=" + url.QueryEscape(original) + "&state=" + state
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, login, http.StatusFound)
}

func randomState() string {
	state := make([]byte, 32)
	if _, err := rand.Read(state); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(state)
}

// requestHost : l'adresse demandée par le navigateur, sans le port.
func requestHost(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(host, "."))
}

// hostOf : le nom d'une adresse, sans le port, en minuscules.
func hostOf(target *url.URL) string {
	return strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
}

// serviceHosts : adresse de chaque service installé qui a une page web, et
// s'il demande une connexion (utilisateurs connectés, administrateurs).
func (c *console) serviceHosts() map[string]bool {
	hosts := map[string]bool{}
	instances, err := c.Services.List()
	if err != nil {
		log.Printf("services illisibles : %v", err)
		return hosts
	}
	for _, instance := range instances {
		if !instance.NoWebPage && instance.Address != "" {
			hosts[strings.ToLower(instance.Address)] = instance.Access == services.AccessUsers || instance.Access == services.AccessAdmins
		}
	}
	return hosts
}

// consoleHost : adresse de la console pour l'adresse d'un service, sans changer
// le port : www-maison.local → maison.local, cloud.maison.example.org →
// maison.example.org ; l'adresse parente pour une adresse inconnue.
func (c *console) consoleHost(host string) string {
	name, port, err := net.SplitHostPort(host)
	if err != nil {
		name, port = host, ""
	}
	console := parentHost(name)
	if instances, err := c.Services.List(); err == nil {
		for _, instance := range instances {
			if strings.EqualFold(instance.Address, name) && len(name) > len(instance.Name)+1 {
				console = strings.ToLower(name[len(instance.Name)+1:])
			}
		}
	}
	if port != "" {
		return net.JoinHostPort(console, port)
	}
	return console
}

// returnURL accepte, dans le même schéma que la console (https derrière Traefik),
// l'adresse de la console, d'un de ses sous-domaines ou d'un service installé
// (services : adresse → connexion demandée). protected : la page demande une
// connexion (service protégé, ou Cockpit sous l'adresse de la console).
func returnURL(raw, host string, secure bool, services map[string]bool) (target *url.URL, protected, ok bool) {
	target, err := url.Parse(raw)
	if err != nil || target.User != nil || target.Opaque != "" {
		return nil, false, false
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	targetHost := hostOf(target)
	if target.Scheme != scheme || host == "" || targetHost == "" {
		return nil, false, false
	}
	if targetHost == host {
		return target, true, true
	}
	if protected, ok := services[targetHost]; ok {
		return target, protected, true
	}
	if net.ParseIP(host) != nil || !strings.HasSuffix(targetHost, "."+host) {
		return nil, false, false
	}
	return target, false, true
}

// parentHost : cloud.maison.example.org → maison.example.org.
func parentHost(name string) string {
	if net.ParseIP(name) == nil {
		if _, parent, ok := strings.Cut(name, "."); ok && strings.Contains(parent, ".") {
			return parent
		}
	}
	return name
}

// clearForwardCookie efface le cookie délégué de l'adresse de la console
// (Cockpit) à la déconnexion ; Logout a déjà fermé les sessions déléguées
// ouvertes depuis cette session, sur toutes les adresses.
func clearForwardCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: forwardCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteLaxMode})
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
	case errors.Is(err, auth.ErrNoResetLink):
		writeError(w, http.StatusGone, "Ce lien ne marche plus : il a déjà servi ou il a plus d'une heure. Demandez-en un nouveau avec « Mot de passe oublié ? ».")
	case errors.Is(err, auth.ErrNoInvitation):
		writeError(w, http.StatusGone, "Ce lien ne marche plus : demandez-en un nouveau à l'administrateur.")
	case errors.Is(err, auth.ErrTOTPRequired):
		writeError(w, http.StatusConflict, "L'administrateur exige la double authentification pour ce compte.")
	default:
		return false
	}
	return true
}
