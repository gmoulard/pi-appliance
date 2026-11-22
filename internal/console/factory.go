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
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// FactoryResetRequest : remise à zéro de l'installation (spécification 8.5,
// TODO-2.52). Password et Code : ceux de l'administrateur connecté, redemandés ;
// inutiles pendant une réinitialisation par le fichier reset.
type FactoryResetRequest struct {
	Password       string `json:"password"`
	Code           string `json:"code"`
	Confirm        bool   `json:"confirm"`
	DeleteServices bool   `json:"delete_services"`
}

type FactoryResetResult struct {
	Problems []string `json:"problems,omitempty"`
}

// NE PAS ouvrir à une simple session : n'importe qui sur le réseau de la maison
// effacerait le Pi. Il faut un administrateur qui redonne son mot de passe, ou
// une réinitialisation par le fichier reset (accès à la carte SD).
func (c *console) factoryReset(w http.ResponseWriter, r *http.Request) {
	var request FactoryResetRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !request.Confirm {
		writeError(w, http.StatusBadRequest, "Confirmez la remise à zéro : cochez la case qui dit ce qui sera effacé.")
		return
	}
	if !c.Accounts.ResetPending() {
		account, ok := c.account(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Connectez-vous avec un compte administrateur pour remettre le Pi à zéro.")
			return
		}
		if account.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "Seul un administrateur peut remettre le Pi à zéro.")
			return
		}
		token, _, err := c.Accounts.Login(account.Name, request.Password, request.Code, clientIP(r))
		if err != nil {
			writeAuthError(w, err)
			return
		}
		c.Accounts.Logout(token)
	}
	var result FactoryResetResult
	if request.DeleteServices {
		instances, err := c.Services.List()
		if err != nil {
			result.Problems = append(result.Problems, "La liste des services n'a pas pu être lue : ils sont gardés.")
		}
		for _, instance := range instances {
			if err := c.Services.Remove(r.Context(), instance.Name, false); err != nil {
				log.Printf("remise à zéro, service %s : %v", instance.Name, err)
				result.Problems = append(result.Problems, fmt.Sprintf("Le service « %s » n'a pas pu être effacé : %v", instance.Name, err))
			}
		}
	}
	if c.Stats != nil {
		if err := c.Stats.SetEnabled(false); err != nil {
			result.Problems = append(result.Problems, "Le réglage des statistiques anonymes n'a pas pu être effacé.")
		}
	}
	if c.DuckDNS != nil {
		if err := c.DuckDNS.Forget(); err != nil {
			result.Problems = append(result.Problems, "Le réglage DuckDNS n'a pas pu être effacé.")
		}
	}
	for _, file := range []string{c.WizardFile, c.ACMEEmailFile} {
		if file == "" {
			continue
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("remise à zéro, %s : %v", file, err)
			result.Problems = append(result.Problems, "Un réglage de la mise en route n'a pas pu être effacé.")
		}
	}
	if err := c.Accounts.FactoryReset(); err != nil {
		log.Printf("remise à zéro, comptes : %v", err)
		writeError(w, http.StatusInternalServerError, "Les comptes n'ont pas pu être effacés : le Pi n'est pas remis à zéro.")
		return
	}
	clearForwardCookie(w, r)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: BasePath, MaxAge: -1, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, result)
}
