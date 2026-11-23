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

import "net/http"

// setAccess change qui ouvre un service (spécification 8.2) : public,
// personnes connectées (users) ou maison seulement (home).
func (c *console) setAccess(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Access string `json:"access"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	instance, err := c.Services.SetAccess(r.Context(), r.PathValue("name"), request.Access)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	// Publication sur internet selon le nouvel accès (services_internet.go).
	c.syncTraefikNow()
	writeJSON(w, http.StatusOK, instance)
}

// setTags : étiquettes d'un service hors catalogue, choisies par l'administrateur
// dans sa fiche (TODO-5.22) ; {"tags": []} les retire.
func (c *console) setTags(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Tags []string `json:"tags"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	name := r.PathValue("name")
	instances, err := c.Services.List()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	for _, instance := range instances {
		if instance.Name != name {
			continue
		}
		if _, ok := c.Catalog.Get(instance.App); ok {
			writeError(w, http.StatusBadRequest, "Ce service prend les étiquettes de son application dans la marketplace.")
			return
		}
		updated, err := c.Services.SetTags(name, request.Tags)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
		return
	}
	writeError(w, http.StatusNotFound, "Ce service n'est pas installé.")
}
