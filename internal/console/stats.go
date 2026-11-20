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
