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
	"log"
	"net/http"
)

// Statistiques anonymes (spécification 8.4), réservées à l'administrateur :
// GET renvoie le réglage et l'envoi exact qui partirait, PUT {"enabled"} le change.

func (c *console) statsReady(w http.ResponseWriter) bool {
	if c.Stats == nil {
		writeError(w, http.StatusServiceUnavailable, "Les statistiques anonymes ne se règlent pas depuis cette console.")
		return false
	}
	return true
}

func (c *console) statsStatus(w http.ResponseWriter, _ *http.Request) {
	if c.statsReady(w) {
		writeJSON(w, http.StatusOK, c.Stats.Status())
	}
}

func (c *console) statsSave(w http.ResponseWriter, r *http.Request) {
	if !c.statsReady(w) {
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Enabled == nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	if err := c.Stats.SetEnabled(*request.Enabled); err != nil {
		log.Printf("statistiques : %v", err)
		writeError(w, http.StatusInternalServerError, "Impossible d'enregistrer le choix des statistiques.")
		return
	}
	writeJSON(w, http.StatusOK, c.Stats.Status())
}
