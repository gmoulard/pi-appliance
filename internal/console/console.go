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
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

const (
	BasePath     = "/pi-appliance"
	maxBodyBytes = 64 << 10
	cookieName   = "pi_session"
)

//go:embed web
var webFiles embed.FS

type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type InstallRequest struct {
	App        string            `json:"app"`
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters"`
}

type SetupRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

type ResetRequest struct {
	Password string `json:"password"`
}

// SessionState dit à l'interface quoi afficher : création du compte administrateur,
// réinitialisation par le fichier reset, connexion ou console.
type SessionState struct {
	Setup   bool         `json:"setup"`
	Reset   bool         `json:"reset"`
	Account *auth.Public `json:"account"`
}

type console struct {
	version  string
	catalog  marketplace.Catalog
	services *services.Manager
	accounts *auth.Store
}

func Handler(version string, catalog marketplace.Catalog, manager *services.Manager, accounts *auth.Store) http.Handler {
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	c := &console{version: version, catalog: catalog, services: manager, accounts: accounts}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+BasePath+"/api/health", c.health)
	mux.HandleFunc("GET "+BasePath+"/api/session", c.session)
	mux.HandleFunc("POST "+BasePath+"/api/setup", c.setup)
	mux.HandleFunc("POST "+BasePath+"/api/reset", c.reset)
	mux.HandleFunc("POST "+BasePath+"/api/login", c.login)
	mux.HandleFunc("POST "+BasePath+"/api/logout", c.logout)
	mux.HandleFunc("GET "+BasePath+"/api/catalog", c.signedIn(c.listCatalog))
	mux.HandleFunc("GET "+BasePath+"/api/services", c.signedIn(c.listServices))
	mux.HandleFunc("POST "+BasePath+"/api/services", c.adminOnly(c.install))
	mux.HandleFunc("DELETE "+BasePath+"/api/services/{name}", c.adminOnly(c.remove))
	mux.Handle("GET "+BasePath+"/", http.StripPrefix(BasePath, http.FileServerFS(web)))
	mux.Handle("GET "+BasePath, http.RedirectHandler(BasePath+"/", http.StatusMovedPermanently))
	return mux
}

func (c *console) account(r *http.Request) (auth.Account, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return auth.Account{}, false
	}
	return c.accounts.Session(cookie.Value)
}

func (c *console) signedIn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := c.account(r); !ok {
			writeError(w, http.StatusUnauthorized, "Connectez-vous pour continuer.")
			return
		}
		next(w, r)
	}
}

// NE PAS relâcher : les actions d'administration (installer, désinstaller) exigent une session d'administrateur.
func (c *console) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, ok := c.account(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Connectez-vous pour continuer.")
			return
		}
		if account.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "Seul un administrateur peut faire cela.")
			return
		}
		next(w, r)
	}
}

func (c *console) session(w http.ResponseWriter, r *http.Request) {
	state := SessionState{Setup: c.accounts.SetupNeeded(), Reset: c.accounts.ResetPending()}
	if account, ok := c.account(r); ok {
		public := account.Public()
		state.Account = &public
	}
	writeJSON(w, http.StatusOK, state)
}

func (c *console) setup(w http.ResponseWriter, r *http.Request) {
	var request SetupRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.accounts.CreateAdmin(request.Name, request.Email, request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	openSession(w, r, c.accounts.StartSession(account.Name))
	writeJSON(w, http.StatusCreated, account.Public())
}

func (c *console) reset(w http.ResponseWriter, r *http.Request) {
	var request ResetRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.accounts.ResetAdmin(request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	openSession(w, r, c.accounts.StartSession(account.Name))
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	token, account, err := c.accounts.Login(request.Name, request.Password, clientIP(r))
	if err != nil {
		writeAuthError(w, err)
		return
	}
	openSession(w, r, token)
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		c.accounts.Logout(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: BasePath, MaxAge: -1, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func openSession(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     BasePath,
		MaxAge:   int(auth.SessionLifetime / time.Second),
		HttpOnly: true,
		Secure:   secure(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// secure : le cookie n'est envoyé qu'en HTTPS, servi par Traefik devant la console (spécification 8.4).
func secure(r *http.Request) bool {
	return r.TLS != nil || (trustedProxy(r) && r.Header.Get("X-Forwarded-Proto") == "https")
}

// clientIP sert à limiter les tentatives de connexion par client : derrière Traefik
// (adresse de bouclage ou réseau Docker), la vraie adresse est la dernière de X-Forwarded-For.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if trustedProxy(r) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			hops := strings.Split(forwarded, ",")
			if ip := net.ParseIP(strings.TrimSpace(hops[len(hops)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	return host
}

var dockerNetworks = func() *net.IPNet {
	_, network, err := net.ParseCIDR("172.16.0.0/12")
	if err != nil {
		panic(err)
	}
	return network
}()

func trustedProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || dockerNetworks.Contains(ip))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Requête attendue en JSON.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return false
	}
	return true
}

func writeAuthError(w http.ResponseWriter, err error) {
	var tooMany auth.TooManyError
	switch {
	case errors.As(err, &tooMany):
		w.Header().Set("Retry-After", fmt.Sprint(int((tooMany.Wait+time.Second-1)/time.Second)))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("Trop de tentatives : réessayez dans %s.", waitText(tooMany.Wait)))
	case errors.Is(err, auth.ErrCredentials):
		writeError(w, http.StatusUnauthorized, "Identifiant ou mot de passe incorrect.")
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), auth.ErrInvalid.Error()+" : "))
	case errors.Is(err, auth.ErrSetupDone):
		writeError(w, http.StatusConflict, "Le compte administrateur existe déjà : connectez-vous.")
	case errors.Is(err, auth.ErrNoReset):
		writeError(w, http.StatusConflict, "Aucune réinitialisation n'est en cours.")
	default:
		log.Printf("comptes : %v", err)
		writeError(w, http.StatusInternalServerError, "Les comptes sont illisibles.")
	}
}

func waitText(wait time.Duration) string {
	if minutes := int((wait + time.Minute - 1) / time.Minute); minutes > 1 {
		return fmt.Sprintf("%d minutes", minutes)
	}
	return "une minute"
}

func (c *console) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: "ok", Version: c.version})
}

func (c *console) listCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"apps": c.catalog.Apps()})
}

func (c *console) listServices(w http.ResponseWriter, _ *http.Request) {
	instances, err := c.services.List()
	if err != nil {
		log.Printf("liste des services : %v", err)
		writeError(w, http.StatusInternalServerError, "La liste des services est illisible.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": instances})
}

func (c *console) install(w http.ResponseWriter, r *http.Request) {
	var request InstallRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	app, ok := c.catalog.Get(request.App)
	if !ok {
		writeError(w, http.StatusNotFound, "Cette application n'est pas dans la marketplace.")
		return
	}
	if request.Name == "" {
		request.Name = app.Subdomain
	}
	result, err := c.services.Install(r.Context(), app, request.Name, request.Parameters)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (c *console) remove(w http.ResponseWriter, r *http.Request) {
	var keepData bool
	switch r.URL.Query().Get("data") {
	case "keep":
		keepData = true
	case "delete":
	default:
		writeError(w, http.StatusBadRequest, "Dites s'il faut garder (data=keep) ou supprimer (data=delete) les données du service.")
		return
	}
	if err := c.services.Remove(r.Context(), r.PathValue("name"), keepData); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		log.Printf("service : %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("réponse JSON : %v", err)
	}
}
