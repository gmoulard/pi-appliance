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

// Package cockpit installe, à la demande de l'administrateur, Cockpit, qui
// administre le système du Pi depuis le navigateur (spécification 6.4), par
// le script cockpit.sh de l'image. Cockpit est servi par Traefik sous
// /cockpit/, maison seulement, réservé aux administrateurs ; on s'y connecte
// avec le compte système pi-admin, dont le mot de passe est choisi dans la
// console et jamais gardé par elle.
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
	"unicode"
)

// DefaultScript est le script installé par l'image.
const DefaultScript = "/usr/local/lib/pi-appliance/cockpit.sh"

// Longueur du mot de passe du compte pi-admin : celle du mot de passe
// administrateur de la console (auth.AdminPasswordMin).
const (
	PasswordMin = 12
	PasswordMax = 128
)

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
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
	Account   bool   `json:"account"`
	User      string `json:"user"`
	Path      string `json:"path"`
	Job       *Job   `json:"job"`
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

// ErrPassword : mot de passe refusé avant même d'appeler le script.
var ErrPassword = errors.New("mot de passe refusé")

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
		// Jamais le mot de passe dans l'erreur : seulement la commande.
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

// CheckPassword : 12 à 128 caractères, sur une seule ligne (cockpit.sh lit une ligne).
func CheckPassword(password string) error {
	n := len([]rune(password))
	if n < PasswordMin || len(password) > PasswordMax {
		return ErrPassword
	}
	if strings.ContainsFunc(password, unicode.IsControl) {
		return ErrPassword
	}
	return nil
}

// Install crée le compte pi-admin avec ce mot de passe et lance
// l'installation, qui se poursuit après la réponse.
func (c *Client) Install(ctx context.Context, password string) error {
	if err := CheckPassword(password); err != nil {
		return err
	}
	_, err := c.run(ctx, []byte(password+"\n"), "install")
	return err
}

// SetPassword change le mot de passe du compte pi-admin.
func (c *Client) SetPassword(ctx context.Context, password string) error {
	if err := CheckPassword(password); err != nil {
		return err
	}
	_, err := c.run(ctx, []byte(password+"\n"), "password")
	return err
}

// Remove lance le retrait de Cockpit et du compte pi-admin.
func (c *Client) Remove(ctx context.Context) error {
	_, err := c.run(ctx, nil, "remove")
	return err
}

// PasswordRequest : corps de POST …/install et POST …/password.
type PasswordRequest struct {
	Password string `json:"password"`
}

// Routes branche l'API sous prefix (…/api/cockpit) ; wrap réserve chaque
// route à l'administrateur.
//
//	GET    prefix           état (Status)
//	POST   prefix/install   {"password"} : compte pi-admin, puis installation
//	POST   prefix/password  {"password"} : nouveau mot de passe de pi-admin
//	DELETE prefix           retrait de Cockpit et du compte pi-admin
func (c *Client) Routes(mux *http.ServeMux, prefix string, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET "+prefix, wrap(c.serveStatus))
	mux.HandleFunc("POST "+prefix+"/install", wrap(c.serveInstall))
	mux.HandleFunc("POST "+prefix+"/password", wrap(c.servePassword))
	mux.HandleFunc("DELETE "+prefix, wrap(c.serveRemove))
}

func (c *Client) serveStatus(w http.ResponseWriter, r *http.Request) {
	status, err := c.Status(r.Context())
	if err != nil {
		c.logf("administration du système : %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "L'état de l'administration du système est illisible pour l'instant."})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func readPassword(w http.ResponseWriter, r *http.Request) (string, bool) {
	var request PasswordRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Demande illisible."})
		return "", false
	}
	return request.Password, true
}

func (c *Client) serveInstall(w http.ResponseWriter, r *http.Request) {
	password, ok := readPassword(w, r)
	if !ok {
		return
	}
	c.reply(w, c.Install(r.Context(), password), http.StatusAccepted, "L'installation n'a pas pu démarrer.")
}

func (c *Client) servePassword(w http.ResponseWriter, r *http.Request) {
	password, ok := readPassword(w, r)
	if !ok {
		return
	}
	c.reply(w, c.SetPassword(r.Context(), password), http.StatusOK, "Le mot de passe n'a pas pu être changé.")
}

func (c *Client) serveRemove(w http.ResponseWriter, r *http.Request) {
	c.reply(w, c.Remove(r.Context()), http.StatusAccepted, "Le retrait n'a pas pu démarrer.")
}

func (c *Client) reply(w http.ResponseWriter, err error, success int, message string) {
	var failure Failure
	switch {
	case err == nil:
		writeJSON(w, success, map[string]bool{"ok": true})
	case errors.Is(err, ErrPassword):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Le mot de passe doit faire entre %d et %d caractères.", PasswordMin, PasswordMax), "code": "invalid"})
	case errors.As(err, &failure):
		status := http.StatusConflict
		if failure.Code == "invalid" {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": failure.Message, "code": failure.Code})
	default:
		c.logf("administration du système : %v", err)
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
