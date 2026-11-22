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

// Console ouverte sur internet (spécification 8.3, TODO-3.12). Fermée par
// défaut ; un administrateur qui a activé la double authentification peut
// l'ouvrir. Deux barrières :
//   - Traefik : le routeur de la console n'accepte que les adresses de la
//     maison ; ouverte, la console écrit console-internet.yml (configuration
//     dynamique de Traefik), un routeur limité aux adresses internet du Pi,
//     avec un certificat Let's Encrypt ; fermée, elle retire ce fichier ;
//   - la console elle-même : une demande venue d'internet est refusée si la
//     console est fermée ; ouverte, seuls les comptes avec double
//     authentification s'y connectent, et la création du compte
//     administrateur ou la réinitialisation se font à la maison seulement.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// InternetSettings : réglage gardé sur le Pi (<données>/internet-console.json).
type InternetSettings struct {
	Enabled bool `json:"enabled"`
	// Domain : adresse personnelle de la console sur internet (par exemple
	// vdl.moulard.org), choisie par l'administrateur ; vide sinon.
	Domain    string    `json:"domain,omitempty"`
	ChangedBy string    `json:"changed_by,omitempty"`
	ChangedAt time.Time `json:"changed_at,omitzero"`
}

// InternetStatus : état montré à l'administrateur (GET api/internet-console).
type InternetStatus struct {
	Enabled bool `json:"enabled"`
	// Domain : adresse personnelle choisie, vide sinon.
	Domain string `json:"domain"`
	// DomainTarget : cible conseillée de l'alias (CNAME) de l'adresse
	// personnelle : <nom>.pi-appliance.com réservée, sinon le nom DuckDNS ;
	// vide sans l'un ni l'autre (entrée A vers l'adresse publique de la box).
	DomainTarget string `json:"domain_target"`
	// Hosts : adresses internet du Pi ; vide tant qu'il n'en a pas.
	Hosts []string `json:"hosts"`
	// URL : adresse de la console sur internet, vide sans adresse.
	URL string `json:"url"`
	// TOTP : le compte connecté a activé la double authentification.
	TOTP bool `json:"totp"`
	// Internet : cette demande vient d'internet.
	Internet bool `json:"internet"`
}

// hostPattern : nom d'hôte sûr à écrire dans la configuration de Traefik.
var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?)+$`)

var internetMu sync.Mutex

// fromInternet : la demande vient d'une adresse qui n'est pas de la maison
// (ni réseau privé, ni boucle locale, ni lien local). Derrière Traefik, c'est
// l'adresse qu'il a vue (clientIP) ; Traefik retire les X-Forwarded-For posés
// par le visiteur.
func fromInternet(r *http.Request) bool {
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return true
	}
	return !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
}

func (c *console) internetSettings() InternetSettings {
	var settings InternetSettings
	if c.InternetFile == "" {
		return settings
	}
	data, err := os.ReadFile(c.InternetFile)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("console sur internet : %v", err)
		}
		return settings
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		log.Printf("console sur internet : %v", err)
		return InternetSettings{}
	}
	return settings
}

func (c *console) internetEnabled() bool {
	return c.internetSettings().Enabled
}

// internetHosts : adresses internet du Pi, sans doublon : adresse personnelle
// de la console (choisie exprès, donc la première), <nom>.pi-appliance.com
// réservée, nom DuckDNS, domaine personnel des services.
func (c *console) internetHosts() []string {
	var hosts []string
	add := func(host string) {
		host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
		if hostPattern.MatchString(host) && !strings.HasSuffix(host, ".local") && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	add(c.internetSettings().Domain)
	if c.Address != nil {
		add(c.Address.Status().Hostname)
	}
	if c.DuckDNS != nil {
		add(c.DuckDNS.Status().Hostname)
	}
	if c.Services != nil {
		add(c.Services.CurrentDomain())
	}
	return hosts
}

// traefikInternet : configuration dynamique de Traefik pour la console sur
// internet ; vide quand elle est fermée ou que le Pi n'a pas d'adresse.
// Les adresses de l'authentification déléguée (api/auth/forward…) ne sont
// appelées que par Traefik lui-même : jamais publiées sur internet.
func traefikInternet(enabled bool, hosts []string) string {
	if !enabled || len(hosts) == 0 {
		return ""
	}
	rules := make([]string, len(hosts))
	for i, host := range hosts {
		rules[i] = "Host(`" + host + "`)"
	}
	var b strings.Builder
	b.WriteString("# Écrit par la console pi-appliance (console ouverte sur internet, spécification 8.3).\n")
	b.WriteString("# Ne pas modifier à la main : refermer la console dans « Adresse sur internet » retire ce fichier.\n")
	b.WriteString("http:\n  routers:\n    pi-appliance-console-internet:\n")
	b.WriteString("      entryPoints: [websecure]\n")
	fmt.Fprintf(&b, "      rule: \"(%s) && PathPrefix(`%s`) && !PathPrefix(`%s/api/auth/forward`)\"\n", strings.Join(rules, " || "), BasePath, BasePath)
	b.WriteString("      priority: 2\n")
	b.WriteString("      service: pi-appliance-console@file\n")
	b.WriteString("      tls:\n        certResolver: letsencrypt\n")
	return b.String()
}

// syncTraefik écrit ou retire console-internet.yml selon le réglage et les
// adresses du moment ; rien n'est réécrit si le contenu ne change pas.
func (c *console) syncTraefik() error {
	if c.TraefikInternetFile == "" {
		return nil
	}
	internetMu.Lock()
	defer internetMu.Unlock()
	content := traefikInternet(c.internetEnabled(), c.internetHosts())
	if content == "" {
		if err := os.Remove(c.TraefikInternetFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if current, err := os.ReadFile(c.TraefikInternetFile); err == nil && string(current) == content {
		return nil
	}
	temporary := c.TraefikInternetFile + ".tmp"
	if err := os.WriteFile(temporary, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, c.TraefikInternetFile)
}

// RunInternet tient console-internet.yml à jour (adresse DuckDNS ou
// pi-appliance.com réglée après l'ouverture, réglage changé) jusqu'à la fin de ctx.
func RunInternet(ctx context.Context, config Config, every time.Duration) {
	c := &console{Config: config}
	for {
		if err := c.syncTraefik(); err != nil {
			log.Printf("console sur internet : %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// internetPaths : ce qu'une demande venue d'internet atteint même console
// fermée : api/hello (application mobile), et l'authentification déléguée,
// appelée par Traefik avec l'adresse du visiteur d'un service.
func internetOpenPath(path string) bool {
	return path == BasePath+"/api/hello" || strings.HasPrefix(path, BasePath+"/api/auth/forward") || strings.HasPrefix(path, BasePath+"/erreur/")
}

// homeOnlyPaths : à faire depuis la maison, même console ouverte.
var homeOnlyPaths = []string{
	"POST " + BasePath + "/api/setup",
	"POST " + BasePath + "/api/reset",
	"POST " + BasePath + "/api/factory-reset",
	"POST " + BasePath + "/appareil/appairer",
	"POST " + BasePath + "/appareil/connexion",
	"POST " + BasePath + "/appareil/retirer",
	"PUT " + BasePath + "/api/internet-console",
	"PUT " + BasePath + "/api/internet-console/domain",
}

// NE PAS relâcher : console fermée sur internet par défaut (spécification 8.3).
func (c *console) internetGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fromInternet(r) || internetOpenPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if !c.internetEnabled() {
			http.NotFound(w, r)
			return
		}
		if slices.Contains(homeOnlyPaths, r.Method+" "+r.URL.Path) && !c.closingFromInternet(r) {
			writeError(w, http.StatusForbidden, "Depuis internet, ce n'est pas possible : faites-le depuis la maison.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// closingFromInternet : fermer la console est permis depuis internet (pas l'ouvrir).
func (c *console) closingFromInternet(r *http.Request) bool {
	return r.Method == http.MethodPut && r.URL.Path == BasePath+"/api/internet-console" && r.URL.Query().Get("enabled") == "false"
}

func (c *console) internetStatus(w http.ResponseWriter, r *http.Request) {
	account, _ := c.account(r)
	writeJSON(w, http.StatusOK, c.internetState(r, account.TOTPSecret != ""))
}

func (c *console) internetState(r *http.Request, totp bool) InternetStatus {
	hosts := c.internetHosts()
	settings := c.internetSettings()
	status := InternetStatus{Enabled: settings.Enabled, Domain: settings.Domain, DomainTarget: c.domainTarget(), Hosts: hosts, TOTP: totp, Internet: fromInternet(r)}
	if status.Hosts == nil {
		status.Hosts = []string{}
	}
	if len(hosts) > 0 {
		status.URL = "https://" + hosts[0] + BasePath + "/"
	}
	return status
}

// domainTarget : nom vers lequel faire pointer l'adresse personnelle (alias
// CNAME), qui suit l'adresse de la box sans identifiant du fournisseur de
// domaine sur le Pi.
func (c *console) domainTarget() string {
	if c.Address != nil {
		if host := c.Address.Status().Hostname; host != "" {
			return host
		}
	}
	if c.DuckDNS != nil {
		return c.DuckDNS.Status().Hostname
	}
	return ""
}

// internetSave : PUT api/internet-console?enabled=true|false. Ouvrir demande
// la double authentification du compte connecté et se fait depuis la maison ;
// fermer est toujours permis.
func (c *console) internetSave(w http.ResponseWriter, r *http.Request) {
	account, _ := c.account(r)
	enabled := r.URL.Query().Get("enabled") == "true"
	if !enabled && r.URL.Query().Get("enabled") != "false" {
		writeError(w, http.StatusBadRequest, "Demande illisible.")
		return
	}
	if c.InternetFile == "" {
		writeError(w, http.StatusNotImplemented, "La console ne s'ouvre pas sur internet ici.")
		return
	}
	settings := c.internetSettings()
	settings.Enabled, settings.ChangedBy, settings.ChangedAt = enabled, account.Name, time.Now().UTC()
	if err := c.saveInternetSettings(settings); err != nil {
		log.Printf("console sur internet : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage n'a pas pu être enregistré.")
		return
	}
	log.Printf("console sur internet : %s par %s", map[bool]string{true: "ouverte", false: "fermée"}[enabled], account.Name)
	if err := c.syncTraefik(); err != nil {
		log.Printf("console sur internet, Traefik : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage est enregistré, mais Traefik n'a pas pu être mis à jour.")
		return
	}
	writeJSON(w, http.StatusOK, c.internetState(r, account.TOTPSecret != ""))
}

// saveInternetSettings écrit le réglage (droits 600), en remplaçant le fichier d'un coup.
func (c *console) saveInternetSettings(settings InternetSettings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(c.InternetFile), 0o700)
	}
	if err == nil {
		temporary := c.InternetFile + ".tmp"
		if err = os.WriteFile(temporary, append(data, '\n'), 0o600); err == nil {
			err = os.Rename(temporary, c.InternetFile)
		}
	}
	return err
}

// lookupHost : résolution DNS, remplacée dans les tests.
var lookupHost = net.DefaultResolver.LookupHost

// cleanDomain : nom tapé par l'administrateur, ramené à un nom d'hôte
// (« https://vdl.moulard.org/pi-appliance/ » donne « vdl.moulard.org »).
func cleanDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(strings.TrimPrefix(value, "https://"), "http://")
	if i := strings.IndexAny(value, "/?#"); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSuffix(value, ".")
}

// internetDomainSave : PUT api/internet-console/domain {"domain"}, à la
// maison seulement. Un nom vide retire l'adresse personnelle. Le nom doit
// déjà exister dans le DNS (alias ou adresse vers la box), sinon Let's
// Encrypt ne pourrait pas donner de certificat.
func (c *console) internetDomainSave(w http.ResponseWriter, r *http.Request) {
	account, _ := c.account(r)
	var body struct {
		Domain string `json:"domain"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	domain := cleanDomain(body.Domain)
	if domain != "" {
		if !hostPattern.MatchString(domain) || len(domain) > 253 || net.ParseIP(domain) != nil || strings.HasSuffix(domain, ".local") {
			writeError(w, http.StatusBadRequest, "Cette adresse n'est pas valable : tapez un nom comme maison.exemple.fr, sans espace.")
			return
		}
		if domain == "pi-appliance.com" || strings.HasSuffix(domain, ".pi-appliance.com") || strings.HasSuffix(domain, ".duckdns.org") {
			writeError(w, http.StatusBadRequest, "Cette adresse se règle plus haut, avec DuckDNS ou votre adresse pi-appliance.com.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		addresses, err := lookupHost(ctx, domain)
		cancel()
		if err != nil || len(addresses) == 0 {
			writeError(w, http.StatusConflict, fmt.Sprintf("L'adresse %s n'existe pas encore sur internet : chez votre fournisseur de nom de domaine, faites-la pointer vers votre box, puis réessayez (cela peut prendre quelques minutes).", domain))
			return
		}
	}
	if c.InternetFile == "" {
		writeError(w, http.StatusNotImplemented, "La console ne s'ouvre pas sur internet ici.")
		return
	}
	settings := c.internetSettings()
	settings.Domain, settings.ChangedBy, settings.ChangedAt = domain, account.Name, time.Now().UTC()
	if err := c.saveInternetSettings(settings); err != nil {
		log.Printf("console sur internet : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage n'a pas pu être enregistré.")
		return
	}
	log.Printf("console sur internet : adresse personnelle %q choisie par %s", domain, account.Name)
	if err := c.syncTraefik(); err != nil {
		log.Printf("console sur internet, Traefik : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage est enregistré, mais Traefik n'a pas pu être mis à jour.")
		return
	}
	writeJSON(w, http.StatusOK, c.internetState(r, account.TOTPSecret != ""))
}
