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

package updates

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
)

// Routes branche l'API des mises à jour sous prefix (…/api/updates) ; wrap
// réserve chaque route à l'administrateur.
//
//	GET  prefix          état des mises à jour (Status)
//	POST prefix/check    nouvelle recherche, sans l'attendre
//	POST prefix/system   installation des correctifs du système
func (c *Checker) Routes(mux *http.ServeMux, prefix string, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET "+prefix, wrap(c.serveStatus))
	mux.HandleFunc("POST "+prefix+"/check", wrap(c.serveCheck))
	mux.HandleFunc("POST "+prefix+"/system", wrap(c.serveApply))
}

func (c *Checker) serveStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, c.Status(r.Context()))
}

func (c *Checker) serveCheck(w http.ResponseWriter, _ *http.Request) {
	c.CheckNow()
	writeJSON(w, http.StatusAccepted, map[string]bool{"checking": true})
}

func (c *Checker) serveApply(w http.ResponseWriter, r *http.Request) {
	if c.Runner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Le système ne se met pas à jour depuis cette console."})
		return
	}
	err := c.ApplySystem(r.Context())
	var failure Failure
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
	case errors.As(err, &failure):
		writeJSON(w, http.StatusConflict, map[string]string{"error": failure.Message, "code": failure.Code})
	default:
		c.logf("mises à jour du système : %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "L'installation des mises à jour n'a pas pu démarrer."})
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("réponse JSON : %v", err)
	}
}
