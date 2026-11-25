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

// Hébergement d'une application hors catalogue (services/hosting.go,
// services/database.go) : réglages de la fiche d'un service, réservés aux
// administrateurs.
//   - PUT api/services/<nom>/subpath {"sub_path": true} : un Docker Compose
//     libre marche sous /<nom>/ (TODO-2.72) ;
//   - PUT api/services/<nom>/mail {"mail": true} : relais d'e-mails (TODO-2.74) ;
//   - PUT api/services/<nom>/redirects {"hosts": [...]} : domaines renvoyés
//     vers l'adresse internet du service (TODO-3.18) ;
//   - POST api/services/<nom>/database : base dans le PostgreSQL partagé (TODO-2.73).

import (
	"context"
	"net/http"
	"time"
)

func (c *console) setSubPath(w http.ResponseWriter, r *http.Request) {
	var request struct {
		SubPath bool `json:"sub_path"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	instance, err := c.Services.SetSubPath(r.PathValue("name"), request.SubPath)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	c.syncTraefikNow()
	writeJSON(w, http.StatusOK, instance)
}

func (c *console) setMail(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Mail bool `json:"mail"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	instance, err := c.Services.SetMail(ctx, r.PathValue("name"), request.Mail)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

func (c *console) setRedirects(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Hosts []string `json:"hosts"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	for _, host := range request.Hosts {
		for _, own := range c.internetHosts() {
			if host == own {
				writeError(w, http.StatusBadRequest, "« "+host+" » est déjà l'adresse internet du Pi.")
				return
			}
		}
	}
	instance, err := c.Services.SetRedirectHosts(r.PathValue("name"), request.Hosts)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	c.syncTraefikNow()
	writeJSON(w, http.StatusOK, instance)
}

func (c *console) createDatabase(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	instance, err := c.Services.CreateDatabase(ctx, r.PathValue("name"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}
