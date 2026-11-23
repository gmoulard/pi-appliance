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

// Package cockpit installe, à la demande de l'administrateur, Cockpit, qui
// administre le système du Pi depuis le navigateur (spécification 6.4), par
// le script cockpit.sh de l'image. Cockpit est servi par Traefik sous
// /cockpit/, maison seulement, réservé aux administrateurs. Connexion
// unique : chaque administrateur de la console a son compte système
// (Logins), dont la console donne elle-même le mot de passe à Cockpit.
package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// DefaultScript est le script installé par l'image.
const DefaultScript = "/usr/local/lib/pi-appliance/cockpit.sh"

// Runner lance cockpit.sh avec ses arguments (network.Script convient).
type Runner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

// Failure est un refus de cockpit.sh : code pour la console, message en langage courant.
type Failure struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (f Failure) Error() string {
	return f.Message
}

// Job : dernière installation ou dernier retrait. Action vaut install ou
// remove ; State vaut starting, running, done ou failed.
type Job struct {
	Action  string `json:"action"`
	State   string `json:"state"`
	Error   string `json:"error"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// Status : réponse de « cockpit.sh status ».
type Status struct {
	Installed bool     `json:"installed"`
	Active    bool     `json:"active"`
	Accounts  []string `json:"accounts"`
	Path      string   `json:"path"`
	Job       *Job     `json:"job"`
	// User : compte système de l'administrateur qui demande, rempli par la
	// console (vide tant qu'il n'est pas créé).
	User string `json:"user"`
}

// Busy dit si une installation ou un retrait est en cours.
func (s Status) Busy() bool {
	return s.Job != nil && (s.Job.State == "starting" || s.Job.State == "running")
}

// Client pilote cockpit.sh.
type Client struct {
	Runner Runner
	Logf   func(format string, args ...any)
}

const timeout = time.Minute

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *Client) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := c.Runner.Run(ctx, stdin, args...)
	if err != nil {
		var failure Failure
		if json.Unmarshal(out, &failure) == nil && failure.Code != "" {
			return nil, failure
		}
		// Jamais un mot de passe dans l'erreur : seulement la commande.
		return nil, fmt.Errorf("cockpit.sh %s : %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// Status lit l'état de Cockpit.
func (c *Client) Status(ctx context.Context) (Status, error) {
	out, err := c.run(ctx, nil, "status")
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(out, &status); err != nil {
		return Status{}, fmt.Errorf("cockpit.sh status : réponse illisible : %w", err)
	}
	return status, nil
}

// Install lance l'installation, qui se poursuit après la réponse.
func (c *Client) Install(ctx context.Context) error {
	_, err := c.run(ctx, nil, "install")
	return err
}

// SyncResult : réponse de « cockpit.sh sync ».
type SyncResult struct {
	// Accounts : compte système de chaque administrateur de la console.
	Accounts map[string]string `json:"accounts"`
	// Conflicts : administrateurs sans compte système possible (nom déjà pris).
	Conflicts []string `json:"conflicts"`
}

// Sync crée, met à jour ou supprime les comptes système de Cockpit : un par
// administrateur (nom → mot de passe).
func (c *Client) Sync(ctx context.Context, passwords map[string]string) (SyncResult, error) {
	var stdin strings.Builder
	for name, password := range passwords {
		fmt.Fprintf(&stdin, "%s %s\n", name, password)
	}
	out, err := c.run(ctx, []byte(stdin.String()), "sync")
	if err != nil {
		return SyncResult{}, err
	}
	var result SyncResult
	if err := json.Unmarshal(out, &result); err != nil {
		return SyncResult{}, fmt.Errorf("cockpit.sh sync : réponse illisible : %w", err)
	}
	return result, nil
}

// Remove lance le retrait de Cockpit et de ses comptes système.
func (c *Client) Remove(ctx context.Context) error {
	_, err := c.run(ctx, nil, "remove")
	return err
}

// Routes branche l'API sous prefix (…/api/cockpit) ; wrap réserve chaque
// route à l'administrateur ; user donne le compte système de l'administrateur
// qui demande (vide s'il n'existe pas encore).
//
//	GET    prefix           état (Status)
//	POST   prefix/install   installation
//	DELETE prefix           retrait de Cockpit et de ses comptes système
func (c *Client) Routes(mux *http.ServeMux, prefix string, wrap func(http.HandlerFunc) http.HandlerFunc, user func(*http.Request) string) {
	mux.HandleFunc("GET "+prefix, wrap(func(w http.ResponseWriter, r *http.Request) {
		status, err := c.Status(r.Context())
		if err != nil {
			c.logf("Cockpit : %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "L'état de Cockpit est illisible pour l'instant."})
			return
		}
		if user != nil {
			status.User = user(r)
		}
		writeJSON(w, http.StatusOK, status)
	}))
	mux.HandleFunc("POST "+prefix+"/install", wrap(c.serveInstall))
	mux.HandleFunc("DELETE "+prefix, wrap(c.serveRemove))
}

func (c *Client) serveInstall(w http.ResponseWriter, r *http.Request) {
	c.reply(w, c.Install(r.Context()), http.StatusAccepted, "L'installation n'a pas pu démarrer.")
}

func (c *Client) serveRemove(w http.ResponseWriter, r *http.Request) {
	c.reply(w, c.Remove(r.Context()), http.StatusAccepted, "Le retrait n'a pas pu démarrer.")
}

func (c *Client) reply(w http.ResponseWriter, err error, success int, message string) {
	var failure Failure
	switch {
	case err == nil:
		writeJSON(w, success, map[string]bool{"ok": true})
	case errors.As(err, &failure):
		status := http.StatusConflict
		if failure.Code == "invalid" {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": failure.Message, "code": failure.Code})
	default:
		c.logf("Cockpit : %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": message})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("réponse JSON : %v", err)
	}
}
