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
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/catalog"
	"github.com/gmoulard/pi-appliance/internal/cockpit"
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/diagnostic"
	"github.com/gmoulard/pi-appliance/internal/metrics"
	"github.com/gmoulard/pi-appliance/internal/network"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/stats"
	"github.com/gmoulard/pi-appliance/internal/storage"
	"github.com/gmoulard/pi-appliance/internal/system"
	"github.com/gmoulard/pi-appliance/internal/updates"
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
	// Access : qui ouvre le service (public, users, home) ; vide : valeur du manifeste.
	Access string `json:"access,omitempty"`
	// ConfirmRisks : l'administrateur a confirmé qu'une application qui a la
	// main sur tout le Pi (Portainer) s'installe.
	ConfirmRisks bool `json:"confirm_risks"`
}

type SetupRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest : Code, code de vérification ou code de secours quand la double
// authentification est activée.
type LoginRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

type ResetRequest struct {
	Password string `json:"password"`
}

type CountryRequest struct {
	Country string `json:"country"`
}

// NameRequest : nouveau nom du Pi sur le réseau (<nom>.local, Wi-Fi de configuration).
type NameRequest struct {
	Name string `json:"name"`
}

// AdvancedRequest : nouveaux réglages avancés d'un service (spécification 6.4) ;
// ConfirmRisks confirme les options dangereuses signalées.
type AdvancedRequest struct {
	Settings     services.Advanced `json:"settings"`
	ConfirmRisks bool              `json:"confirm_risks"`
}

// ComposeRequest : un fichier Docker Compose libre à installer (spécification 6.4).
type ComposeRequest struct {
	Name         string `json:"name"`
	Compose      string `json:"compose"`
	Service      string `json:"service"`
	Port         int    `json:"port"`
	ConfirmRisks bool   `json:"confirm_risks"`
}

// StorageUseRequest : placer les données des services sur cette partition ext4.
type StorageUseRequest struct {
	Partition string `json:"partition"`
}

// StorageFormatRequest : effacer ce disque USB ; Erase confirme que son contenu sera perdu.
type StorageFormatRequest struct {
	Disk  string `json:"disk"`
	Erase bool   `json:"erase"`
}

// SessionState dit à l'interface quoi afficher : création du compte administrateur,
// réinitialisation par le fichier reset, connexion, assistant de premier démarrage
// (administrateur seulement) ou console.
type SessionState struct {
	Setup   bool         `json:"setup"`
	Reset   bool         `json:"reset"`
	Wizard  bool         `json:"wizard"`
	Account *auth.Public `json:"account"`
}

// Config rassemble ce dont la console a besoin.
// Catalog : le catalogue de la marketplace, intégré (marketplace.Catalog) ou
// tenu à jour en ligne (catalog.Source).
type Catalog interface {
	Apps() []marketplace.App
	Get(id string) (marketplace.App, bool)
}

type Config struct {
	Version  string
	Catalog  Catalog
	Services *services.Manager
	Accounts *auth.Store
	// Network peut être nil (poste de développement) : les écrans du réseau
	// disent alors que le réseau ne se règle pas ici.
	Network *network.Client
	// Storage peut être nil (poste de développement) : pas de disque USB.
	Storage *storage.Client
	// Resources lit l'état du Pi ; nil : jauges absentes.
	Resources func() system.Resources
	// History : historique des graphiques sur 30 jours ; nil : pas de graphiques.
	History *metrics.History
	// DuckDNS tient à jour l'adresse sur internet (spécification 7.1) ; nil : non réglable ici.
	DuckDNS *ddns.Updater
	// Address : adresse <nom>.pi-appliance.com auprès du service d'adresses (spécification 7.1) ;
	// nil : seules les règles des noms sont vérifiées.
	Address *addresses.Client
	// Backups : sauvegardes (spécification 9.1) ; nil : pas de sauvegardes ici.
	Backups *backup.Manager
	// Updates propose les mises à jour (spécification 9.2) ; nil : pas d'écran des mises à jour.
	Updates *updates.Checker
	// Cockpit : administration du système (spécification 6.4) ; nil : non installable ici.
	Cockpit *cockpit.Client
	// Stats : statistiques anonymes (spécification 8.4) ; nil : non réglables ici.
	Stats *stats.Reporter
	// Diagnostic : état du système et journaux (spécification 8.5) ; nil : pas de diagnostic ici.
	Diagnostic *diagnostic.Client
	// DiagnosticWindow : page de diagnostic ouverte par le fichier diagnostic ; nil : pas de fichier.
	DiagnosticWindow *diagnostic.Window
	// WizardFile marque la fin de l'assistant de premier démarrage ; vide : pas d'assistant.
	WizardFile    string
	ACMEEmailFile string
	// Mailer envoie les invitations ; nil : le lien est montré à l'administrateur.
	Mailer auth.Mailer
	// DevicePublicKey : clé publique du Pi (device.pub), d'où vient son
	// identifiant pour l'application mobile ; vide : pas d'identifiant.
	DevicePublicKey string
	// Hostname : nom du Pi sur le réseau ; nil : nom du système (os.Hostname).
	Hostname func() (string, error)
}

type console struct {
	Config
}

func Handler(config Config) http.Handler {
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	c := &console{Config: config}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+BasePath+"/api/health", c.health)
	mux.HandleFunc("GET "+BasePath+"/api/session", c.session)
	mux.HandleFunc("GET "+BasePath+"/api/hello", c.helloGet)
	mux.HandleFunc("OPTIONS "+BasePath+"/api/hello", c.helloPreflight)
	mux.HandleFunc("GET "+BasePath+"/api/mobile", c.adminOnly(c.mobile))
	mux.HandleFunc("POST "+BasePath+"/api/setup", c.setup)
	mux.HandleFunc("POST "+BasePath+"/api/reset", c.reset)
	mux.HandleFunc("POST "+BasePath+"/api/factory-reset", c.factoryReset)
	mux.HandleFunc("POST "+BasePath+"/api/login", c.login)
	mux.HandleFunc("POST "+BasePath+"/api/logout", c.logout)
	mux.HandleFunc("GET "+BasePath+"/api/catalog", c.signedIn(c.listCatalog))
	mux.HandleFunc("GET "+BasePath+"/api/services", c.signedIn(c.listServices))
	mux.HandleFunc("POST "+BasePath+"/api/services", c.adminOnly(c.install))
	mux.HandleFunc("DELETE "+BasePath+"/api/services/{name}", c.adminOnly(c.remove))
	mux.HandleFunc("POST "+BasePath+"/api/services/{name}/{action}", c.adminOnly(c.control))
	mux.HandleFunc("GET "+BasePath+"/api/services/{name}/advanced", c.adminOnly(c.advancedSettings))
	mux.HandleFunc("GET "+BasePath+"/api/services/{name}/progress", c.adminOnly(c.installProgress))
	mux.HandleFunc("PUT "+BasePath+"/api/services/{name}/advanced", c.adminOnly(c.setAdvanced))
	mux.HandleFunc("PUT "+BasePath+"/api/services/{name}/access", c.adminOnly(c.setAccess))
	mux.HandleFunc("POST "+BasePath+"/api/compose", c.adminOnly(c.installCompose))
	mux.HandleFunc("GET "+BasePath+"/api/services/{name}/wireguard", c.adminOnly(c.wireGuard))
	mux.HandleFunc("POST "+BasePath+"/api/services/{name}/wireguard/devices", c.adminOnly(c.addDevice))
	mux.HandleFunc("DELETE "+BasePath+"/api/services/{name}/wireguard/devices/{id}", c.adminOnly(c.removeDevice))
	mux.HandleFunc("GET "+BasePath+"/api/network", c.adminOnly(c.networkStatus))
	mux.HandleFunc("GET "+BasePath+"/api/network/wifi", c.adminOnly(c.wifiList))
	mux.HandleFunc("POST "+BasePath+"/api/network/wifi/scan", c.adminOnly(c.wifiScan))
	mux.HandleFunc("POST "+BasePath+"/api/network/wifi", c.adminOnly(c.wifiJoin))
	mux.HandleFunc("POST "+BasePath+"/api/network/wired", c.adminOnly(c.wired))
	mux.HandleFunc("POST "+BasePath+"/api/network/country", c.adminOnly(c.country))
	mux.HandleFunc("POST "+BasePath+"/api/network/name", c.adminOnly(c.setName))
	mux.HandleFunc("POST "+BasePath+"/api/network/test", c.adminOnly(c.networkTest))
	mux.HandleFunc("POST "+BasePath+"/api/system/reboot", c.adminOnly(c.reboot))
	mux.HandleFunc("GET "+BasePath+"/api/system", c.adminOnly(c.resources))
	mux.HandleFunc("GET "+BasePath+"/api/system/history", c.adminOnly(c.history))
	mux.HandleFunc("GET "+BasePath+"/api/duckdns", c.adminOnly(c.duckdnsStatus))
	mux.HandleFunc("PUT "+BasePath+"/api/duckdns", c.adminOnly(c.duckdnsSave))
	mux.HandleFunc("DELETE "+BasePath+"/api/duckdns", c.adminOnly(c.duckdnsForget))
	mux.HandleFunc("GET "+BasePath+"/api/address", c.adminOnly(c.addressStatus))
	mux.HandleFunc("POST "+BasePath+"/api/address", c.adminOnly(c.addressReserve))
	mux.HandleFunc("DELETE "+BasePath+"/api/address", c.adminOnly(c.addressRelease))
	mux.HandleFunc("GET "+BasePath+"/api/address/check", c.adminOnly(c.addressCheck))
	mux.HandleFunc("GET "+BasePath+"/api/stats", c.adminOnly(c.statsStatus))
	mux.HandleFunc("PUT "+BasePath+"/api/stats", c.adminOnly(c.statsSave))
	mux.HandleFunc("POST "+BasePath+"/api/wizard", c.adminOnly(c.wizardDone))
	mux.HandleFunc("GET "+BasePath+"/api/storage", c.adminOnly(c.storageStatus))
	mux.HandleFunc("POST "+BasePath+"/api/storage/use", c.adminOnly(c.storageUse))
	mux.HandleFunc("POST "+BasePath+"/api/storage/format", c.adminOnly(c.storageFormat))
	c.accountRoutes(mux)
	c.deviceRoutes(mux)
	if c.Backups != nil {
		c.Backups.Routes(mux, BasePath, c.adminOnly)
	}
	if c.Updates != nil {
		c.Updates.Routes(mux, BasePath+"/api/updates", c.adminOnly)
	}
	if c.Cockpit != nil {
		c.Cockpit.Routes(mux, BasePath+"/api/cockpit", c.adminOnly)
	}
	if c.Diagnostic != nil {
		mux.HandleFunc("GET "+BasePath+"/api/diagnostic", c.diagnosticStatus)
		mux.HandleFunc("GET "+BasePath+"/api/diagnostic/logs", c.diagnosticLogs)
		mux.HandleFunc("DELETE "+BasePath+"/api/diagnostic", c.diagnosticClose)
	}
	for _, page := range []string{"/reset-to-factory", "/diagnostic"} {
		mux.HandleFunc("GET "+BasePath+page, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, web, "index.html")
		})
	}
	mux.Handle("GET "+BasePath+"/", http.StripPrefix(BasePath, http.FileServerFS(web)))
	mux.Handle("GET "+BasePath, http.RedirectHandler(BasePath+"/", http.StatusMovedPermanently))
	return mux
}

func (c *console) account(r *http.Request) (auth.Account, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return auth.Account{}, false
	}
	return c.Accounts.Session(cookie.Value)
}

// NE PAS relâcher : tant que la double authentification exigée par l'administrateur
// n'est pas activée, le compte ne peut que l'activer (ownAccount).
func (c *console) signedIn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		account, ok := c.account(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Connectez-vous pour continuer.")
			return
		}
		if account.NeedsTOTPSetup() {
			writeError(w, http.StatusForbidden, "Activez d'abord la double authentification, exigée pour ce compte.")
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
		if account.NeedsTOTPSetup() {
			writeError(w, http.StatusForbidden, "Activez d'abord la double authentification, exigée pour ce compte.")
			return
		}
		next(w, r)
	}
}

func (c *console) session(w http.ResponseWriter, r *http.Request) {
	state := SessionState{Setup: c.Accounts.SetupNeeded(), Reset: c.Accounts.ResetPending()}
	if account, ok := c.account(r); ok {
		public := account.Public()
		state.Account = &public
		state.Wizard = account.Role == auth.RoleAdmin && c.wizardPending()
	}
	writeJSON(w, http.StatusOK, state)
}

func (c *console) setup(w http.ResponseWriter, r *http.Request) {
	var request SetupRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.CreateAdmin(request.Name, request.Email, request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if err := c.saveACMEEmail(account.Email); err != nil {
		log.Printf("adresse de contact Let's Encrypt : %v", err)
		writeError(w, http.StatusInternalServerError, "Le compte est créé, mais l'adresse de contact ACME n'a pas pu être enregistrée. Corrigez le problème puis reconnectez-vous.")
		return
	}
	c.protectBackups(request.Password, false)
	openSession(w, r, c.Accounts.StartSession(account.Name))
	writeJSON(w, http.StatusCreated, account.Public())
}

func (c *console) reset(w http.ResponseWriter, r *http.Request) {
	var request ResetRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.ResetAdmin(request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	c.protectBackups(request.Password, false)
	openSession(w, r, c.Accounts.StartSession(account.Name))
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	token, account, err := c.Accounts.Login(request.Name, request.Password, request.Code, clientIP(r))
	if err != nil {
		writeAuthError(w, err)
		return
	}
	if account.Role == auth.RoleAdmin {
		if err := c.saveACMEEmail(account.Email); err != nil {
			log.Printf("adresse de contact Let's Encrypt : %v", err)
			writeError(w, http.StatusInternalServerError, "L'adresse de contact ACME n'a pas pu être enregistrée.")
			return
		}
		if c.Accounts.FirstAdmin(account.Name) {
			c.protectBackups(request.Password, true)
		}
	}
	openSession(w, r, token)
	writeJSON(w, http.StatusOK, account.Public())
}

// protectBackups (re)chiffre la clé des sauvegardes par le mot de passe du
// premier administrateur (spécification 9.1) : à la création du compte, à la
// réinitialisation et au changement du mot de passe ; à la connexion, seulement
// si ce n'est pas encore fait.
func (c *console) protectBackups(password string, ifMissing bool) {
	if c.Backups == nil {
		return
	}
	protect := c.Backups.Protect
	if ifMissing {
		protect = c.Backups.ProtectIfMissing
	}
	if err := protect(password); err != nil {
		log.Printf("clé des sauvegardes : %v", err)
	}
}

func (c *console) saveACMEEmail(email string) error {
	if c.ACMEEmailFile == "" {
		return nil
	}
	if strings.ContainsAny(email, "\r\n") {
		return errors.New("adresse e-mail invalide pour la configuration ACME")
	}
	if err := os.MkdirAll(filepath.Dir(c.ACMEEmailFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(c.ACMEEmailFile, []byte(email+"\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(c.ACMEEmailFile, 0o600)
}

func (c *console) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		c.Accounts.Logout(cookie.Value)
	}
	clearForwardCookie(w, r)
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
	if accountError(w, err) {
		return
	}
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

// wizardPending : l'assistant de premier démarrage n'est pas encore terminé (spécification 5.1).
func (c *console) wizardPending() bool {
	if c.WizardFile == "" {
		return false
	}
	_, err := os.Stat(c.WizardFile)
	return errors.Is(err, fs.ErrNotExist)
}

func (c *console) wizardDone(w http.ResponseWriter, _ *http.Request) {
	if c.WizardFile != "" {
		if err := os.WriteFile(c.WizardFile, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600); err != nil {
			log.Printf("assistant : %v", err)
			writeError(w, http.StatusInternalServerError, "Impossible d'enregistrer la fin de l'assistant.")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// resources : jauges de la mémoire et du disque du Pi (spécification 6.2).
func (c *console) resources(w http.ResponseWriter, _ *http.Request) {
	var resources system.Resources
	if c.Resources != nil {
		resources = c.Resources()
	}
	writeJSON(w, http.StatusOK, resources)
}

// history : points horaires des 30 derniers jours, pour les graphiques du tableau de bord (spécification 5.2).
func (c *console) history(w http.ResponseWriter, _ *http.Request) {
	points := []metrics.Point{}
	if c.History != nil {
		points = c.History.Points()
	}
	writeJSON(w, http.StatusOK, map[string]any{"step_seconds": int(metrics.Step / time.Second), "points": points})
}

func (c *console) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: "ok", Version: c.Version})
}

func (c *console) listCatalog(w http.ResponseWriter, _ *http.Request) {
	body := map[string]any{"apps": c.Catalog.Apps()}
	if source, ok := c.Catalog.(interface{ Status() catalog.Status }); ok {
		body["source"] = source.Status()
	}
	writeJSON(w, http.StatusOK, body)
}

// ServiceView est un service installé avec son état vert, orange ou rouge.
type ServiceView struct {
	services.Instance
	Status          services.Status `json:"status"`
	UpdateAvailable bool            `json:"update_available"`
}

func (c *console) listServices(w http.ResponseWriter, r *http.Request) {
	instances, err := c.Services.List()
	if err != nil {
		log.Printf("liste des services : %v", err)
		writeError(w, http.StatusInternalServerError, "La liste des services est illisible.")
		return
	}
	views := make([]ServiceView, 0, len(instances))
	for _, instance := range instances {
		status, err := c.Services.Status(r.Context(), instance.Name)
		if err != nil {
			status = services.Status{Color: services.Orange, Message: "État inconnu."}
		}
		view := ServiceView{Instance: instance, Status: status}
		if app, ok := c.Catalog.Get(instance.App); ok {
			view.UpdateAvailable = c.Services.UpdateAvailable(app, instance.Name)
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": views})
}

func (c *console) control(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if r.PathValue("action") == "update" {
		c.update(w, r, name)
		return
	}
	if err := c.Services.Control(r.Context(), name, services.Action(r.PathValue("action"))); err != nil {
		writeServiceError(w, err)
		return
	}
	status, err := c.Services.Status(r.Context(), name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (c *console) install(w http.ResponseWriter, r *http.Request) {
	var request InstallRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	app, ok := c.Catalog.Get(request.App)
	if !ok {
		writeError(w, http.StatusNotFound, "Cette application n'est pas dans la marketplace.")
		return
	}
	if request.Name == "" {
		request.Name = app.Subdomain
	}
	if request.Access != "" && request.Access != app.Access {
		if app.Access == services.AccessAdmins {
			writeError(w, http.StatusBadRequest, app.Name+" a la main sur tout le Pi : il reste réservé aux administrateurs, à la maison.")
			return
		}
		if !services.ValidAccess(request.Access) {
			writeError(w, http.StatusBadRequest, "Choisissez qui ouvre le service : tout le monde, les personnes connectées ou la maison seulement.")
			return
		}
		app.Access = request.Access
	}
	risks, err := services.MarketplaceRisks(app)
	if err == nil && len(risks) > 0 && !request.ConfirmRisks {
		err = services.RisksError{Risks: risks}
	}
	if err != nil {
		writeServiceError(w, err)
		return
	}
	result, err := c.Services.Install(r.Context(), app, request.Name, request.Parameters)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// installProgress : avancement de l'installation lancée par POST api/services
// ou api/compose, lu pendant qu'elle se fait (spécification 6.2).
func (c *console) installProgress(w http.ResponseWriter, r *http.Request) {
	progress, ok := c.Services.InstallProgress(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "Aucune installation en cours pour ce service.")
		return
	}
	writeJSON(w, http.StatusOK, progress)
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
	if err := c.Services.Remove(r.Context(), r.PathValue("name"), keepData); err != nil {
		writeServiceError(w, err)
		return
	}
	if err := c.Accounts.RevokeServiceShares(r.PathValue("name")); err != nil {
		log.Printf("liens de partage de %s : %v", r.PathValue("name"), err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// update met un service à jour depuis le catalogue, avec retour arrière automatique en cas d'échec.
func (c *console) update(w http.ResponseWriter, r *http.Request, name string) {
	instances, err := c.Services.List()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	for _, instance := range instances {
		if instance.Name != name {
			continue
		}
		app, ok := c.Catalog.Get(instance.App)
		if !ok {
			if instance.App == services.CustomApp {
				writeError(w, http.StatusBadRequest, "Un service Docker Compose libre ne se met pas à jour depuis la marketplace : changez les versions des images dans son fichier et réinstallez-le.")
				return
			}
			writeError(w, http.StatusNotFound, "Cette application n'est plus dans la marketplace : pas de mise à jour possible.")
			return
		}
		result, err := c.Services.Update(r.Context(), app, name)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeServiceError(w, fmt.Errorf("%w : %s", services.ErrNotFound, name))
}

// Réglages avancés et Docker Compose libre (spécification 6.4), réservés à l'administrateur.

func (c *console) advancedSettings(w http.ResponseWriter, r *http.Request) {
	settings, names, err := c.Services.AdvancedSettings(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": settings, "services": names})
}

func (c *console) setAdvanced(w http.ResponseWriter, r *http.Request) {
	var request AdvancedRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	risks, err := c.Services.SetAdvanced(r.Context(), r.PathValue("name"), request.Settings, request.ConfirmRisks)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"risks": nonNil(risks)})
}

// VPN WireGuard (spécification 6.3), réservé à l'administrateur : la
// configuration d'un appareil (clé privée comprise) n'est rendue qu'une fois.

func (c *console) wireGuard(w http.ResponseWriter, r *http.Request) {
	status, err := c.Services.WireGuard(r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (c *console) addDevice(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	device, err := c.Services.AddDevice(r.Context(), r.PathValue("name"), request.Name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, device)
}

func (c *console) removeDevice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "Appareil inconnu.")
		return
	}
	if err := c.Services.RemoveDevice(r.Context(), r.PathValue("name"), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *console) installCompose(w http.ResponseWriter, r *http.Request) {
	var request ComposeRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result, err := c.Services.InstallCustom(r.Context(), services.CustomRequest{
		Name:         request.Name,
		Compose:      []byte(request.Compose),
		Service:      request.Service,
		Port:         request.Port,
		ConfirmRisks: request.ConfirmRisks,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func nonNil(risks []services.Risk) []services.Risk {
	if risks == nil {
		return []services.Risk{}
	}
	return risks
}

func writeServiceError(w http.ResponseWriter, err error) {
	var risks services.RisksError
	switch {
	case errors.As(err, &risks):
		// À confirmer, pas bloqué (spécification 6.4) : la console redemande avec confirm_risks.
		writeJSON(w, http.StatusConflict, map[string]any{"error": "Ces options donnent la main sur tout le Pi : confirmez pour continuer.", "risks": risks.Risks})
	case errors.Is(err, services.ErrSettingsRestored):
		writeError(w, http.StatusConflict, "Avec ces réglages, le service ne démarre pas : les réglages d'avant sont remis. "+strings.TrimPrefix(err.Error(), services.ErrSettingsRestored.Error()+" : "))
	case errors.Is(err, services.ErrNoSpace):
		writeError(w, http.StatusConflict, strings.TrimPrefix(err.Error(), services.ErrNoSpace.Error()+" : ")+".")
	case errors.Is(err, services.ErrRolledBack):
		writeError(w, http.StatusConflict, "La mise à jour n'a pas marché : le service est revenu à sa version précédente. "+strings.TrimPrefix(err.Error(), services.ErrRolledBack.Error()+" : "))
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

// Adresse <nom>.pi-appliance.com (spécification 7.1), réservée à l'administrateur.

// AddressCheck : réponse de GET api/address/check. Valid : le nom suit les
// règles (vérifiées sur le Pi) ; Available : le service d'adresses le dit libre.
type AddressCheck struct {
	Name      string `json:"name"`
	Hostname  string `json:"hostname,omitempty"`
	Valid     bool   `json:"valid"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// AddressStatus : réponse de GET api/address.
type AddressStatus struct {
	addresses.State
	// Service : false si la console ne parle pas au service d'adresses.
	Service bool `json:"service"`
	// DuckDNS : nom DuckDNS vers lequel pointera l'adresse ("" : DuckDNS pas réglé).
	DuckDNS string `json:"duckdns,omitempty"`
}

func (c *console) addressStatus(w http.ResponseWriter, _ *http.Request) {
	status := AddressStatus{Service: c.Address != nil}
	if c.Address != nil {
		status.State = c.Address.Status()
	}
	if c.DuckDNS != nil {
		status.DuckDNS = c.DuckDNS.Status().Hostname
	}
	writeJSON(w, http.StatusOK, status)
}

func (c *console) addressCheck(w http.ResponseWriter, r *http.Request) {
	name := addresses.Normalize(r.URL.Query().Get("name"))
	result := AddressCheck{Name: name}
	if err := addresses.Check(name); err != nil {
		result.Reason = strings.TrimPrefix(err.Error(), addresses.ErrInvalid.Error()+" : ")
		writeJSON(w, http.StatusOK, result)
		return
	}
	result.Valid = true
	result.Hostname = addresses.Hostname(name)
	if c.Address == nil {
		writeJSON(w, http.StatusOK, result)
		return
	}
	availability, err := c.Address.Check(r.Context(), name)
	if err != nil {
		addressError(w, err)
		return
	}
	result.Available = availability.Available
	result.Reason = availability.Reason
	writeJSON(w, http.StatusOK, result)
}

func (c *console) addressReady(w http.ResponseWriter) bool {
	if c.Address == nil {
		writeError(w, http.StatusServiceUnavailable, "Cette console ne peut pas réserver d'adresse pi-appliance.com.")
		return false
	}
	return true
}

func (c *console) addressReserve(w http.ResponseWriter, r *http.Request) {
	if !c.addressReady(w) {
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	state, err := c.Address.Reserve(r.Context(), request.Name)
	if err != nil {
		addressError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AddressStatus{State: state, Service: true, DuckDNS: state.Target})
}

func (c *console) addressRelease(w http.ResponseWriter, r *http.Request) {
	if !c.addressReady(w) {
		return
	}
	if err := c.Address.Release(r.Context()); err != nil {
		addressError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// addressError traduit une erreur du service d'adresses en réponse, phrase en langage courant.
func addressError(w http.ResponseWriter, err error) {
	var refused *addresses.ServiceError
	sentence := func(text string) string {
		text = strings.TrimPrefix(text, addresses.ErrInvalid.Error()+" : ")
		if text == "" {
			return text
		}
		return strings.ToUpper(text[:1]) + text[1:] + "."
	}
	switch {
	case errors.As(err, &refused):
		status := refused.Status
		if status == http.StatusUnauthorized {
			// Seule cause attendue : l'horloge du Pi n'est pas à l'heure.
			status = http.StatusBadGateway
		}
		writeError(w, status, sentence(refused.Message))
	case errors.Is(err, addresses.ErrInvalid):
		writeError(w, http.StatusBadRequest, sentence(err.Error()))
	case errors.Is(err, addresses.ErrNoTarget), errors.Is(err, addresses.ErrNotReserved):
		writeError(w, http.StatusConflict, sentence(err.Error()))
	case errors.Is(err, addresses.ErrUnreachable):
		writeError(w, http.StatusServiceUnavailable, sentence(err.Error()))
	default:
		log.Printf("adresse : %v", err)
		writeError(w, http.StatusInternalServerError, "Impossible d'enregistrer l'adresse sur le Pi.")
	}
}

// Adresse sur internet par DuckDNS (spécification 7.1), réservée à l'administrateur.
// NE PAS renvoyer le jeton : la réponse ne contient que ddns.Status.

func (c *console) duckdnsReady(w http.ResponseWriter) bool {
	if c.DuckDNS == nil {
		writeError(w, http.StatusServiceUnavailable, "L'adresse sur internet ne se règle pas depuis cette console.")
		return false
	}
	return true
}

func (c *console) duckdnsStatus(w http.ResponseWriter, _ *http.Request) {
	if c.duckdnsReady(w) {
		writeJSON(w, http.StatusOK, c.DuckDNS.Status())
	}
}

func (c *console) duckdnsSave(w http.ResponseWriter, r *http.Request) {
	if !c.duckdnsReady(w) {
		return
	}
	var request ddns.Settings
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := c.DuckDNS.Save(request); err != nil {
		if errors.Is(err, ddns.ErrInvalid) {
			writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), ddns.ErrInvalid.Error()+" : ")+".")
			return
		}
		log.Printf("DuckDNS : %v", err)
		writeError(w, http.StatusInternalServerError, "Impossible d'enregistrer les réglages DuckDNS.")
		return
	}
	if c.Address != nil {
		// Nouveau nom DuckDNS : l'alias pi-appliance.com le suit tout de suite.
		c.Address.WakeUp()
	}
	writeJSON(w, http.StatusOK, c.DuckDNS.Status())
}

func (c *console) duckdnsForget(w http.ResponseWriter, _ *http.Request) {
	if !c.duckdnsReady(w) {
		return
	}
	if err := c.DuckDNS.Forget(); err != nil {
		log.Printf("DuckDNS : %v", err)
		writeError(w, http.StatusInternalServerError, "Impossible d'effacer les réglages DuckDNS.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Réseau du Pi (spécification, section 4), réservé à l'administrateur : le
// portail de configuration crée d'abord son compte, puis choisit le réseau.

func (c *console) networkCall(w http.ResponseWriter, r *http.Request, call func(*network.Client, context.Context) (json.RawMessage, error)) {
	if c.Network == nil {
		writeError(w, http.StatusServiceUnavailable, "Le réseau ne se règle pas depuis cette console.")
		return
	}
	out, err := call(c.Network, r.Context())
	if err != nil {
		writeNetworkError(w, err)
		return
	}
	writeRaw(w, http.StatusOK, out)
}

func (c *console) networkStatus(w http.ResponseWriter, r *http.Request) {
	c.networkCall(w, r, (*network.Client).Status)
}

func (c *console) wifiList(w http.ResponseWriter, r *http.Request) {
	c.networkCall(w, r, (*network.Client).WifiList)
}

func (c *console) wifiScan(w http.ResponseWriter, r *http.Request) {
	c.networkCall(w, r, (*network.Client).WifiScan)
}

func (c *console) wired(w http.ResponseWriter, r *http.Request) {
	c.networkCall(w, r, (*network.Client).Wired)
}

func (c *console) networkTest(w http.ResponseWriter, r *http.Request) {
	c.networkCall(w, r, (*network.Client).Test)
}

// reboot redémarre le Pi à la fin de la mise en route (spécification 5.1),
// pour vérifier qu'il retrouve seul le réseau de la maison.
func (c *console) reboot(w http.ResponseWriter, r *http.Request) {
	c.networkCall(w, r, func(n *network.Client, ctx context.Context) (json.RawMessage, error) {
		out, err := n.Reboot(ctx)
		if err == nil {
			log.Printf("redémarrage du Pi demandé par l'administrateur")
		}
		return out, err
	})
}

func (c *console) country(w http.ResponseWriter, r *http.Request) {
	var request CountryRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	c.networkCall(w, r, func(n *network.Client, ctx context.Context) (json.RawMessage, error) {
		return n.SetCountry(ctx, request.Country)
	})
}

// setName change le nom du Pi (spécification 4) : la console devient
// https://<nom>.local/pi-appliance, Traefik redémarre après la réponse, et
// les services passent à <service>-<nom>.local (Relocate).
func (c *console) setName(w http.ResponseWriter, r *http.Request) {
	var request NameRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	c.networkCall(w, r, func(n *network.Client, ctx context.Context) (json.RawMessage, error) {
		out, err := n.SetName(ctx, request.Name)
		if err == nil {
			var renamed struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(out, &renamed) == nil && renamed.Name != "" {
				c.Services.Renamed(renamed.Name)
				go c.Services.Relocate(context.WithoutCancel(ctx), log.Printf)
			}
		}
		return out, err
	})
}

// wifiJoin répond 202 avant de lancer la connexion : le point d'accès va se
// fermer et le téléphone perdre la réponse.
func (c *console) wifiJoin(w http.ResponseWriter, r *http.Request) {
	if c.Network == nil {
		writeError(w, http.StatusServiceUnavailable, "Le réseau ne se règle pas depuis cette console.")
		return
	}
	var request network.JoinRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := c.Network.Join(request); err != nil {
		writeNetworkError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"ssid": request.SSID})
}

func writeNetworkError(w http.ResponseWriter, err error) {
	var failure network.Failure
	switch {
	case errors.As(err, &failure):
		writeJSON(w, http.StatusConflict, map[string]string{"error": failure.Message, "code": failure.Code})
	case errors.Is(err, network.ErrInvalid):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), network.ErrInvalid.Error()+" : ")+".")
	default:
		log.Printf("réseau : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réseau du Pi ne répond pas.")
	}
}

// Disque USB des données des services (spécification 4), réservé à l'administrateur.

func (c *console) storageStatus(w http.ResponseWriter, r *http.Request) {
	if c.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "Le disque USB ne se règle pas depuis cette console.")
		return
	}
	status, err := c.Storage.Status(r.Context())
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (c *console) storageUse(w http.ResponseWriter, r *http.Request) {
	var request StorageUseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	c.storageStart(w, r, func(ctx context.Context) error { return c.Storage.Use(ctx, request.Partition) })
}

func (c *console) storageFormat(w http.ResponseWriter, r *http.Request) {
	var request StorageFormatRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !request.Erase {
		writeError(w, http.StatusBadRequest, "Confirmez que le contenu du disque sera effacé.")
		return
	}
	c.storageStart(w, r, func(ctx context.Context) error { return c.Storage.Format(ctx, request.Disk) })
}

// storageStart lance le déplacement, qui se poursuit après la réponse : Docker
// s'arrête pendant la copie, donc Traefik et l'accès à la console avec lui.
func (c *console) storageStart(w http.ResponseWriter, r *http.Request, start func(context.Context) error) {
	if c.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "Le disque USB ne se règle pas depuis cette console.")
		return
	}
	if err := start(r.Context()); err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

func writeStorageError(w http.ResponseWriter, err error) {
	var failure storage.Failure
	switch {
	case errors.As(err, &failure):
		writeJSON(w, http.StatusConflict, map[string]string{"error": failure.Message, "code": failure.Code})
	case errors.Is(err, storage.ErrInvalid):
		writeError(w, http.StatusBadRequest, "Ce disque est inconnu.")
	default:
		log.Printf("disque USB : %v", err)
		writeError(w, http.StatusInternalServerError, "Les disques du Pi ne répondent pas.")
	}
}

func writeRaw(w http.ResponseWriter, status int, body json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		log.Printf("réponse JSON : %v", err)
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
