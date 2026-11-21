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

// Appairage de l'application mobile (spécification 8.1) : un téléphone
// appairé ouvre la console sans mot de passe. L'application envoie un
// formulaire par une navigation (nouvel onglet), pas par fetch ; la console
// répond par une courte page qui passe à la console par une navigation du
// même site, pour que le cookie SameSite=Strict de la session soit envoyé.

import (
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"slices"
	"strings"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// mobileOrigins : sites d'où viennent les formulaires de l'application mobile
// (adresse définitive, et adresse provisoire de GitHub Pages).
var mobileOrigins = []string{MobileApp, "https://gmoulard.github.io"}

// Raisons d'un refus, passées à la console (?appareil=…) qui les dit en
// langage courant dans la langue choisie ; « appaire » : appairage réussi.
const (
	devicePaired  = "appaire"
	deviceCode    = "code"
	deviceUnknown = "inconnu"
	deviceClock   = "heure"
	deviceTooMany = "essais"
	deviceRefused = "refus"
)

func (c *console) deviceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST "+BasePath+"/appareil/appairer", c.devicePair)
	mux.HandleFunc("POST "+BasePath+"/appareil/connexion", c.deviceLogin)
	mux.HandleFunc("GET "+BasePath+"/api/account/devices", c.ownAccount(c.listAccountDevices))
	mux.HandleFunc("PUT "+BasePath+"/api/account/devices/{id}", c.ownAccount(c.renameAccountDevice))
	mux.HandleFunc("DELETE "+BasePath+"/api/account/devices/{id}", c.ownAccount(c.removeAccountDevice))
}

// deviceForm : champs du formulaire de l'application, refusé s'il ne vient pas
// d'un site de l'application (contrôle d'origine) ou s'il est illisible.
func deviceForm(w http.ResponseWriter, r *http.Request) bool {
	if !slices.Contains(mobileOrigins, r.Header.Get("Origin")) {
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	return r.ParseForm() == nil
}

// formMessage : le message signé. Un formulaire HTML envoie les retours à la
// ligne de ses champs en « \r\n » (règle du HTML) ; le téléphone a signé le
// message avec des « \n ».
func formMessage(r *http.Request) string {
	return strings.ReplaceAll(r.PostForm.Get("message"), "\r\n", "\n")
}

func deviceReason(err error) (int, string) {
	var tooMany auth.TooManyError
	switch {
	case errors.As(err, &tooMany):
		return http.StatusTooManyRequests, deviceTooMany
	case errors.Is(err, auth.ErrPairingCode):
		return http.StatusForbidden, deviceCode
	case errors.Is(err, auth.ErrDeviceUnknown), errors.Is(err, auth.ErrNotFound):
		return http.StatusForbidden, deviceUnknown
	case errors.Is(err, auth.ErrDeviceClock):
		return http.StatusForbidden, deviceClock
	case errors.Is(err, auth.ErrDeviceSignature), errors.Is(err, auth.ErrTooManyDevices):
		return http.StatusForbidden, deviceRefused
	default:
		log.Printf("appareil : %v", err)
		return http.StatusInternalServerError, deviceRefused
	}
}

// devicePair : POST appareil/appairer (code, cle, appareil, message, signature).
func (c *console) devicePair(w http.ResponseWriter, r *http.Request) {
	if !deviceForm(w, r) {
		devicePage(w, http.StatusForbidden, deviceRefused)
		return
	}
	form := r.PostForm
	token, _, err := c.Accounts.Pair(form.Get("code"), form.Get("cle"), form.Get("appareil"), formMessage(r), form.Get("signature"), DeviceID(c.DevicePublicKey), clientIP(r))
	if err != nil {
		status, reason := deviceReason(err)
		devicePage(w, status, reason)
		return
	}
	openSession(w, r, token)
	devicePage(w, http.StatusOK, devicePaired)
}

// deviceLogin : POST appareil/connexion (cle : empreinte, message, signature).
func (c *console) deviceLogin(w http.ResponseWriter, r *http.Request) {
	if !deviceForm(w, r) {
		devicePage(w, http.StatusForbidden, deviceRefused)
		return
	}
	form := r.PostForm
	token, _, err := c.Accounts.DeviceLogin(form.Get("cle"), formMessage(r), form.Get("signature"), DeviceID(c.DevicePublicKey), clientIP(r))
	if err != nil {
		status, reason := deviceReason(err)
		devicePage(w, status, reason)
		return
	}
	openSession(w, r, token)
	devicePage(w, http.StatusOK, "")
}

// devicePage : page de passage vers la console, par une navigation du même
// site (rafraîchissement immédiat, et un lien sans rafraîchissement) ; reason :
// raison à dire à la console, vide pour une connexion réussie.
func devicePage(w http.ResponseWriter, status int, reason string) {
	next := BasePath + "/"
	if reason != "" {
		next += "?appareil=" + reason
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	link := html.EscapeString(next)
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta http-equiv="refresh" content="0;url=%s"><title>pi-appliance</title></head><body style="font-family: sans-serif; margin: 3rem auto; text-align: center"><p><a href="%s">pi-appliance</a></p></body></html>`, link, link)
}

func (c *console) listAccountDevices(w http.ResponseWriter, _ *http.Request, account auth.Account) {
	writeJSON(w, http.StatusOK, map[string]any{"devices": c.Accounts.Devices(account.Name)})
}

func (c *console) renameAccountDevice(w http.ResponseWriter, r *http.Request, account auth.Account) {
	var request struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	device, err := c.Accounts.RenameDevice(account.Name, r.PathValue("id"), request.Name)
	switch {
	case errors.Is(err, auth.ErrDeviceUnknown):
		writeError(w, http.StatusNotFound, "Cet appareil n'est pas appairé à votre compte.")
	case errors.Is(err, auth.ErrInvalid):
		writeError(w, http.StatusBadRequest, "Donnez un nom à l'appareil.")
	case err != nil:
		log.Printf("appareil : %v", err)
		writeError(w, http.StatusInternalServerError, "Le nom de l'appareil n'a pas pu être enregistré.")
	default:
		writeJSON(w, http.StatusOK, device)
	}
}

func (c *console) removeAccountDevice(w http.ResponseWriter, r *http.Request, account auth.Account) {
	err := c.Accounts.RemoveDevice(account.Name, r.PathValue("id"))
	switch {
	case errors.Is(err, auth.ErrDeviceUnknown):
		writeError(w, http.StatusNotFound, "Cet appareil n'est pas appairé à votre compte.")
	case err != nil:
		log.Printf("appareil : %v", err)
		writeError(w, http.StatusInternalServerError, "L'appareil n'a pas pu être retiré.")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
