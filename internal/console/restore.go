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
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/gmoulard/pi-appliance/internal/backup"
)

type WholeRestoreStatus struct {
	Disk      *backup.DiskStatus `json:"disk"`
	DiskReady bool               `json:"disk_ready"`
	Running   string             `json:"running"`
	Last      *backup.Outcome    `json:"last"`
	Backups   []backup.Whole     `json:"backups"`
}

type WholeRestoreRequest struct {
	Host     string `json:"host"`
	ID       string `json:"id"`
	Password string `json:"password"`
}

// WholeRestoreReply : Name, nom du Pi sauvegardé que le Pi reprend à la fin
// (vide : le nom ne change pas).
type WholeRestoreReply struct {
	Started bool   `json:"started"`
	Name    string `json:"name,omitempty"`
}

func (c *console) restoreRoutes(mux *http.ServeMux) {
	base := BasePath + "/api/setup/restore"
	mux.HandleFunc("GET "+base, c.beforeSetup(c.wholeRestoreStatus))
	mux.HandleFunc("POST "+base+"/disk", c.beforeSetup(c.wholeRestoreDisk))
	mux.HandleFunc("POST "+base, c.beforeSetup(c.wholeRestoreStart))
}

// NE PAS relâcher : routes sans session, seulement tant qu'aucun compte n'existe et jamais depuis internet.
func (c *console) beforeSetup(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c.Backups == nil {
			writeError(w, http.StatusNotFound, "Les sauvegardes ne se restaurent pas sur cette console.")
			return
		}
		if c.fromInternet(r) {
			writeError(w, http.StatusForbidden, "Depuis internet, ce n'est pas possible : faites-le depuis la maison.")
			return
		}
		if !c.Accounts.SetupNeeded() {
			writeError(w, http.StatusForbidden, "L'appliance a déjà un compte : une sauvegarde se remet depuis l'écran « Sauvegardes » de « Mon Pi ».")
			return
		}
		next(w, r)
	}
}

func (c *console) wholeRestoreStatus(w http.ResponseWriter, r *http.Request) {
	status := WholeRestoreStatus{Running: c.Backups.Running(), Last: c.Backups.Last(), Backups: []backup.Whole{}}
	if c.Backups.Disks != nil {
		if disk, err := c.Backups.Disks.Status(r.Context()); err == nil {
			status.Disk = &disk
		} else {
			log.Printf("disque de sauvegarde : %v", err)
		}
	}
	list, err := c.Backups.ApplianceBackups(r.Context())
	switch {
	case err == nil:
		status.DiskReady = true
		status.Backups = list
	case !errors.Is(err, backup.ErrNoDisk):
		status.DiskReady = true
		log.Printf("sauvegardes illisibles : %v", err)
	}
	writeJSON(w, http.StatusOK, status)
}

func (c *console) wholeRestoreDisk(w http.ResponseWriter, r *http.Request) {
	var request backup.DiskUseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if c.Backups.Disks == nil {
		writeError(w, http.StatusServiceUnavailable, "Le disque de sauvegarde ne se règle pas depuis cette console.")
		return
	}
	if err := c.Backups.Disks.Use(r.Context(), request.Partition); err != nil {
		backup.WriteFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *console) wholeRestoreStart(w http.ResponseWriter, r *http.Request) {
	var request WholeRestoreRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	keys := []string{"restore-client:" + clientIP(r), "restore"}
	if err := c.Accounts.Throttled(keys...); err != nil {
		writeAuthError(w, err)
		return
	}
	if err := c.Backups.CheckAppliance(r.Context(), request.Host, request.ID, request.Password); err != nil {
		if errors.Is(err, backup.ErrPassword) && request.Password != "" {
			c.Accounts.Failed(keys...)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Ce mot de passe n'ouvre pas cette sauvegarde : saisissez le mot de passe administrateur en vigueur à sa date.", "code": "password"})
			return
		}
		backup.WriteFailure(w, err)
		return
	}
	if !c.restoring.CompareAndSwap(false, true) {
		backup.WriteFailure(w, backup.ErrBusy)
		return
	}
	// Le Pi reprend le nom du Pi sauvegardé (décision du 2026-11-26) : même
	// dossier de sauvegardes, même adresse <nom>.local. Le dossier d'une
	// appliance sur le disque porte son nom (backup.DiskTarget).
	rename := ""
	if request.Host != "" && request.Host != c.piName() && c.Network != nil {
		rename = request.Host
	}
	go func() {
		err := c.Backups.RestoreAppliance(context.Background(), request.Host, request.ID, request.Password)
		if err != nil {
			log.Printf("restauration de l'appliance : %v", err)
		}
		if err == nil && rename != "" {
			if _, err := c.Network.SetName(context.Background(), rename); err != nil {
				log.Printf("restauration de l'appliance, nom « %s » : %v", rename, err)
			}
		}
		if c.Restart != nil && !errors.Is(err, backup.ErrBusy) {
			c.Restart()
			return
		}
		c.restoring.Store(false)
	}()
	writeJSON(w, http.StatusAccepted, WholeRestoreReply{Started: true, Name: rename})
}
