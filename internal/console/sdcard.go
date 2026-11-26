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

	"github.com/gmoulard/pi-appliance/internal/sdcard"
)

type SDCardStatus struct {
	sdcard.Status
	Internet bool `json:"internet"`
}

type SDCardWriteRequest struct {
	Device string `json:"device"`
	Model  string `json:"model"`
	Size   int64  `json:"size"`
	Erase  bool   `json:"erase"`
}

var sdcardMessages = map[string]string{
	sdcard.ErrNoImage:        "Aucune version de pi-appliance avec une image n'a été trouvée.",
	sdcard.ErrDownload:       "Le téléchargement de l'image a échoué : vérifiez que le Pi est relié à internet.",
	sdcard.ErrAlreadyRunning: "Une copie est déjà en cours.",
	sdcard.ErrCardChanged:    "La carte a changé depuis l'affichage de la liste : cherchez à nouveau.",
	"refused":                "Cette carte ne peut pas être préparée.",
}

func (c *console) sdcardStatus(w http.ResponseWriter, r *http.Request) {
	if c.SDCard == nil {
		writeError(w, http.StatusServiceUnavailable, "Les cartes SD ne se préparent pas depuis cette console.")
		return
	}
	status, err := c.SDCard.Status(r.Context())
	if err != nil {
		log.Printf("carte SD : %v", err)
		writeError(w, http.StatusInternalServerError, "Les disques du Pi ne répondent pas.")
		return
	}
	writeJSON(w, http.StatusOK, SDCardStatus{Status: status, Internet: fromInternet(r)})
}

func (c *console) sdcardWrite(w http.ResponseWriter, r *http.Request) {
	if c.SDCard == nil {
		writeError(w, http.StatusServiceUnavailable, "Les cartes SD ne se préparent pas depuis cette console.")
		return
	}
	// NE PAS ouvrir depuis internet : une session volée effacerait le disque branché au Pi.
	if fromInternet(r) {
		writeError(w, http.StatusForbidden, "Depuis internet, ce n'est pas possible : faites-le depuis la maison.")
		return
	}
	var request SDCardWriteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !request.Erase {
		writeError(w, http.StatusBadRequest, "Confirmez que tout le contenu de la carte sera effacé.")
		return
	}
	err := c.SDCard.Start(r.Context(), request.Device, request.Model, request.Size)
	var failure sdcard.Failure
	switch {
	case err == nil:
		writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
	case errors.As(err, &failure):
		message := sdcardMessages[failure.Code]
		if message == "" {
			message = "La copie n'a pas pu démarrer."
		}
		writeJSON(w, http.StatusConflict, map[string]string{"error": message, "code": failure.Code, "refused": failure.Refused})
	default:
		log.Printf("carte SD : %v", err)
		writeError(w, http.StatusInternalServerError, "Les disques du Pi ne répondent pas.")
	}
}

func (c *console) sdcardCancel(w http.ResponseWriter, r *http.Request) {
	if c.SDCard == nil || !c.SDCard.Cancel() {
		writeError(w, http.StatusConflict, "Aucune copie n'est en cours.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
