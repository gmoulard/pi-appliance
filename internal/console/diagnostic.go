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

package console

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/diagnostic"
	"github.com/gmoulard/pi-appliance/internal/system"
)

// DiagnosticReport : réponse de GET api/diagnostic (spécification 8.5,
// TODO-2.55). StatusError : l'état des services n'a pas pu être lu.
// ByFile : page ouverte par le fichier diagnostic (ou reset), jusqu'à OpenUntil.
type DiagnosticReport struct {
	*diagnostic.Status
	StatusError string            `json:"status_error,omitempty"`
	Version     string            `json:"version"`
	Resources   *system.Resources `json:"resources,omitempty"`
	ByFile      bool              `json:"by_file"`
	OpenUntil   string            `json:"open_until,omitempty"`
}

// NE PAS ouvrir à une simple session ni à tout le réseau de la maison : les
// journaux disent quels appareils, réseaux et comptes le Pi connaît. Il faut
// un administrateur, ou l'accès à la carte SD (fichier diagnostic ou reset).
func (c *console) diagnosticAllowed(w http.ResponseWriter, r *http.Request) (byFile bool, ok bool) {
	if c.Accounts.ResetPending() {
		return true, true
	}
	if _, open := c.DiagnosticWindow.Until(); open {
		return true, true
	}
	account, signed := c.account(r)
	switch {
	case !signed:
		writeError(w, http.StatusUnauthorized, "Connectez-vous avec un compte administrateur, ou déposez le fichier diagnostic sur la carte SD, pour voir le diagnostic du Pi.")
	case account.Role != auth.RoleAdmin:
		writeError(w, http.StatusForbidden, "Seul un administrateur peut voir le diagnostic du Pi.")
	case account.NeedsTOTPSetup():
		writeError(w, http.StatusForbidden, "Activez d'abord la double authentification, exigée pour ce compte.")
	default:
		return false, true
	}
	return false, false
}

func (c *console) diagnosticStatus(w http.ResponseWriter, r *http.Request) {
	byFile, ok := c.diagnosticAllowed(w, r)
	if !ok {
		return
	}
	report := DiagnosticReport{Version: c.Version, ByFile: byFile}
	if until, open := c.DiagnosticWindow.Until(); open && byFile {
		report.OpenUntil = until.Format(time.RFC3339)
	}
	if c.Resources != nil {
		resources := c.Resources()
		report.Resources = &resources
	}
	status, err := c.Diagnostic.Status(r.Context())
	if err != nil {
		log.Printf("diagnostic : %v", err)
		report.StatusError = "L'état des services n'a pas pu être lu."
	} else {
		report.Status = &status
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, report)
}

func (c *console) diagnosticLogs(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.diagnosticAllowed(w, r); !ok {
		return
	}
	logs, err := c.Diagnostic.Logs(r.Context())
	if err != nil {
		log.Printf("diagnostic : %v", err)
		writeError(w, http.StatusInternalServerError, "Les journaux du Pi n'ont pas pu être lus.")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="pi-appliance-diagnostic-%s.txt"`, time.Now().UTC().Format("20060102-1504")))
	w.Write(logs)
}

// La page ouverte par le fichier se referme avant la fin des 24 heures.
func (c *console) diagnosticClose(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.diagnosticAllowed(w, r); !ok {
		return
	}
	if err := c.DiagnosticWindow.Close(); err != nil {
		log.Printf("diagnostic : %v", err)
		writeError(w, http.StatusInternalServerError, "L'accès par le fichier diagnostic n'a pas pu être fermé.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var rescueFiles = map[string]bool{
	"/app.js": true, "/i18n.js": true, "/styles.css": true, "/favicon.svg": true, "/symbole.svg": true,
}

// RescueHandler : la console de secours (diagnostic.Rescue), quand Traefik ne
// répond plus. Seulement depuis la maison (mêmes plages que la console dans
// Traefik), seulement la page de diagnostic et la connexion.
// NE PAS laisser passer X-Forwarded-For : sans Traefik devant, le client
// l'écrirait lui-même et échapperait à la limitation des tentatives de connexion.
func RescueHandler(console http.Handler) http.Handler {
	allowed := map[string]bool{
		"GET " + BasePath + "/diagnostic":          true,
		"GET " + BasePath + "/api/diagnostic":      true,
		"GET " + BasePath + "/api/diagnostic/logs": true,
		"DELETE " + BasePath + "/api/diagnostic":   true,
		"GET " + BasePath + "/api/health":          true,
		"GET " + BasePath + "/api/session":         true,
		"POST " + BasePath + "/api/login":          true,
		"POST " + BasePath + "/api/logout":         true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		for _, header := range []string{"X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Real-Ip", "Forwarded"} {
			r.Header.Del(header)
		}
		path := r.URL.Path
		file := strings.TrimPrefix(path, BasePath)
		static := r.Method == http.MethodGet && strings.HasPrefix(path, BasePath+"/") &&
			(rescueFiles[file] || (strings.HasPrefix(file, "/i18n/") && strings.HasSuffix(file, ".js") && !strings.Contains(file[len("/i18n/"):], "/")))
		switch {
		case allowed[r.Method+" "+path] || static:
			console.ServeHTTP(w, r)
		case r.Method == http.MethodGet && !strings.HasPrefix(path, BasePath+"/api/"):
			http.Redirect(w, r, BasePath+"/diagnostic", http.StatusFound)
		default:
			writeError(w, http.StatusNotFound, "La console de secours ne sert que le diagnostic du Pi.")
		}
	})
}
