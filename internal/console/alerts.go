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
	"errors"
	"log"
	"net/http"

	"github.com/gmoulard/pi-appliance/internal/alerts"
	"github.com/gmoulard/pi-appliance/internal/auth"
)

// Alertes par e-mail (spécification 8.6, TODO-4.2) : réglage des types
// d'alerte, pour l'administrateur, dans « Mon Pi ».

// AlertsStatus : réponse de GET api/alerts.
type AlertsStatus struct {
	Types []alerts.Setting `json:"types"`
	// Mail : le Pi sait envoyer des e-mails (SMTP réglé).
	Mail bool `json:"mail"`
	// Recipients : administrateurs dont l'adresse est vérifiée, qui reçoivent les alertes.
	Recipients int `json:"recipients"`
	// Verified : l'adresse du compte connecté est vérifiée.
	Verified bool `json:"verified"`
}

// AlertRequest : PUT api/alerts.
type AlertRequest struct {
	Type    alerts.Type `json:"type"`
	Enabled bool        `json:"enabled"`
}

func (c *console) alertRoutes(mux *http.ServeMux) {
	if c.Alerts == nil {
		return
	}
	mux.HandleFunc("GET "+BasePath+"/api/alerts", c.adminOnly(c.alertsStatus))
	mux.HandleFunc("PUT "+BasePath+"/api/alerts", c.adminOnly(c.alertsSave))
}

// AlertRecipients : administrateurs qui ont choisi leur mot de passe et dont
// l'adresse e-mail est vérifiée.
func AlertRecipients(accounts *auth.Store) []alerts.Recipient {
	var recipients []alerts.Recipient
	for _, public := range accounts.List() {
		if public.Role != auth.RoleAdmin || !public.EmailVerified || public.Invited || public.Email == "" {
			continue
		}
		account, err := accounts.Get(public.Name)
		if err != nil {
			continue
		}
		recipients = append(recipients, alerts.Recipient{Name: account.Name, Email: account.Email, Lang: account.Lang})
	}
	return recipients
}

// notify : alerte « compte créé ou supprimé » ou « mot de passe changé »,
// envoyée à part pour ne pas retenir la réponse (serveur SMTP lent).
func (c *console) notify(event alerts.Event) {
	if c.Alerts != nil {
		go c.Alerts.Notify(event)
	}
}

// InternetHosts : adresses internet du Pi (certificats surveillés par les alertes).
func InternetHosts(config Config) []string {
	return (&console{Config: config}).internetHosts()
}

func (c *console) alertsState(r *http.Request) AlertsStatus {
	status := AlertsStatus{Types: c.Alerts.Settings(), Mail: c.SMTP.Configured(), Recipients: len(AlertRecipients(c.Accounts))}
	if account, ok := c.account(r); ok {
		status.Verified = account.EmailVerified
	}
	return status
}

func (c *console) alertsStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, c.alertsState(r))
}

func (c *console) alertsSave(w http.ResponseWriter, r *http.Request) {
	var request AlertRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := c.Alerts.SetEnabled(request.Type, request.Enabled); err != nil {
		if errors.Is(err, alerts.ErrUnknownType) {
			writeError(w, http.StatusBadRequest, "Ce type d'alerte n'existe pas.")
			return
		}
		writeError(w, http.StatusInternalServerError, "Le réglage n'a pas pu être enregistré.")
		return
	}
	account, _ := c.account(r)
	log.Printf("alertes : « %s » %s par %s", request.Type, map[bool]string{true: "activée", false: "coupée"}[request.Enabled], account.Name)
	writeJSON(w, http.StatusOK, c.alertsState(r))
}
