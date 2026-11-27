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
