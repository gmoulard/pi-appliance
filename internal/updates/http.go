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

package updates

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// Routes branche l'API des mises à jour sous prefix (…/api/updates) ; wrap
// réserve chaque route à l'administrateur.
//
//	GET  prefix          état des mises à jour (Status)
//	POST prefix/check    nouvelle recherche, sans l'attendre
//	POST prefix/system   installation des correctifs du système
//	POST prefix/appliance installation de la dernière version de pi-appliance,
//	                      ou de {"version": "v0.1.6"} (plus récente ou plus ancienne)
//	GET  prefix/versions versions publiées sur le canal (Version), la plus récente en premier
//	PUT  prefix/channel  canal de version, {"channel": "stable"|"beta"}
func (c *Checker) Routes(mux *http.ServeMux, prefix string, wrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET "+prefix, wrap(c.serveStatus))
	mux.HandleFunc("PUT "+prefix+"/channel", wrap(c.serveChannel))
	mux.HandleFunc("POST "+prefix+"/check", wrap(c.serveCheck))
	mux.HandleFunc("POST "+prefix+"/system", wrap(c.serveApply))
	mux.HandleFunc("POST "+prefix+"/appliance", wrap(c.serveInstall))
	mux.HandleFunc("GET "+prefix+"/versions", wrap(c.serveVersions))
}

func (c *Checker) serveVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := c.Versions(r.Context())
	var failure Failure
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"current": c.Version, "channel": c.Channel(), "versions": versions})
	case errors.As(err, &failure):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": failure.Message, "code": failure.Code})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "La liste des versions n'a pas pu être lue."})
	}
}

func (c *Checker) serveInstall(w http.ResponseWriter, r *http.Request) {
	// Sans corps (écran de la console) : la dernière version.
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Demande illisible."})
		return
	}
	version, err := c.InstallVersion(r.Context(), body.Version)
	var failure Failure
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]string{"version": version})
	case errors.As(err, &failure) && (failure.Code == "version" || failure.Code == "unknown"):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": failure.Message, "code": failure.Code})
	case errors.As(err, &failure):
		writeJSON(w, http.StatusConflict, map[string]string{"error": failure.Message, "code": failure.Code})
	default:
		c.logf("nouvelle version de pi-appliance : %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "L'installation de la nouvelle version n'a pas pu démarrer."})
	}
}

func (c *Checker) serveChannel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Demande illisible."})
		return
	}
	err := c.SetChannel(body.Channel)
	var failure Failure
	switch {
	case err == nil:
		c.CheckNow()
		writeJSON(w, http.StatusOK, map[string]string{"channel": c.Channel()})
	case errors.As(err, &failure) && failure.Code == "channel":
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": failure.Message, "code": failure.Code})
	case errors.As(err, &failure):
		writeJSON(w, http.StatusConflict, map[string]string{"error": failure.Message, "code": failure.Code})
	default:
		c.logf("canal de version : %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Le canal n'a pas pu être enregistré."})
	}
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
