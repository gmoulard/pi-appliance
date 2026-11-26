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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/cockpit"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/updates"
)

// FactoryResetRequest : remise à zéro de l'installation (spécification 8.5,
// TODO-2.52). Password et Code : ceux de l'administrateur connecté, redemandés ;
// inutiles pendant une réinitialisation par le fichier reset.
type FactoryResetRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
	Confirm  bool   `json:"confirm"`
}

// FactoryResetResult : Name, nom de départ repris par le Pi (<nom>.local).
type FactoryResetResult struct {
	Name     string   `json:"name,omitempty"`
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
	// NE PAS relâcher : le fichier reset efface sans compte ; depuis internet,
	// n'importe qui le ferait.
	if c.Accounts.ResetPending() && fromInternet(r) {
		writeError(w, http.StatusForbidden, "Depuis internet, ce n'est pas possible : faites-le depuis la maison.")
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
		if account.NeedsTwoFactorSetup() {
			writeError(w, http.StatusForbidden, "Activez d'abord la double authentification, exigée pour ce compte.")
			return
		}
		// Depuis internet : compte avec double authentification (confirmFromInternet).
		if fromInternet(r) && !account.HasTwoFactor() {
			writeError(w, http.StatusForbidden, "Depuis internet, cette action demande la double authentification : activez-la d'abord dans « Mon compte » (application d'authentification), ou faites-le depuis la maison.")
			return
		}
		token, _, err := c.Accounts.Login(account.Name, request.Password, request.Code, clientIP(r))
		if err != nil {
			writeAuthError(w, err)
			return
		}
		c.Accounts.Logout(token)
	}
	if c.Backups != nil && c.Backups.Running() != "" {
		writeError(w, http.StatusConflict, "Une sauvegarde ou une restauration est en cours : attendez la fin, puis recommencez.")
		return
	}
	var result FactoryResetResult
	problem := func(text string, err error) {
		log.Printf("remise à zéro : %s : %v", text, err)
		result.Problems = append(result.Problems, text)
	}
	c.factoryRemoveServices(r.Context(), &result)
	if c.Address != nil && c.Address.Status().Name != "" {
		if err := c.Address.Release(r.Context()); err != nil {
			problem("L'adresse pi-appliance.com réservée n'a pas pu être libérée : le service des adresses ne répond pas.", err)
		}
	}
	if c.PortMapper != nil {
		if _, err := c.PortMapper.Disable(r.Context(), ""); err != nil {
			problem("L'accès direct depuis internet n'a pas pu être coupé.", err)
		}
	}
	if c.CockpitAccess != nil && c.CockpitAccess.Path != "" {
		if err := c.CockpitAccess.Set(cockpit.Access{}); err != nil {
			problem("Le réglage de l'accès à Cockpit n'a pas pu être effacé.", err)
		}
	}
	if c.Updates != nil && c.Updates.SettingsFile != "" {
		if err := c.Updates.SetChannel(updates.Stable); err != nil {
			problem("Le canal des mises à jour n'a pas pu être remis à « stable ».", err)
		}
	}
	if c.Alerts != nil {
		c.Alerts.Reset()
	}
	if c.Stats != nil {
		if err := c.Stats.SetEnabled(false); err != nil {
			problem("Le réglage des statistiques anonymes n'a pas pu être effacé.", err)
		}
	}
	if c.SMTP != nil {
		if err := c.SMTP.Forget(); err != nil {
			problem("Le réglage de l'envoi des e-mails n'a pas pu être effacé.", err)
		}
	}
	for _, file := range []string{c.WizardFile, c.WizardUpdateFile, c.ACMEEmailFile, c.InternetFile} {
		if file == "" {
			continue
		}
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			problem("Un réglage de la mise en route n'a pas pu être effacé.", err)
		}
	}
	if c.Backups != nil {
		if err := c.Backups.Forget(r.Context()); err != nil {
			problem("Les réglages des sauvegardes n'ont pas pu être effacés, ou le disque de sauvegarde n'a pas pu être libéré : débranchez-le.", err)
		}
	}
	if err := c.Accounts.FactoryReset(); err != nil {
		log.Printf("remise à zéro, comptes : %v", err)
		writeError(w, http.StatusInternalServerError, "Les comptes n'ont pas pu être effacés : le Pi n'est pas remis à zéro.")
		return
	}
	c.syncTraefikNow()
	if c.Network != nil {
		out, err := c.Network.ResetName(r.Context())
		var renamed struct {
			Name string `json:"name"`
		}
		if err == nil && json.Unmarshal(out, &renamed) == nil {
			result.Name = renamed.Name
		} else {
			problem("Le nom du Pi n'a pas pu être remis à celui du départ.", err)
		}
	}
	clearForwardCookie(w, r)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: BasePath, MaxAge: -1, HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, result)
	// Nom, adresses et réglages relus par une console neuve.
	if c.Restart != nil {
		time.AfterFunc(3*time.Second, c.Restart)
	}
}

// factoryRemoveServices désinstalle chaque service avec ses données ;
// PostgreSQL partagé en dernier, une fois ses clients partis.
func (c *console) factoryRemoveServices(ctx context.Context, result *FactoryResetResult) {
	instances, err := c.Services.List()
	if err != nil {
		log.Printf("remise à zéro, services : %v", err)
		result.Problems = append(result.Problems, "La liste des services n'a pas pu être lue : ils n'ont pas été effacés.")
		return
	}
	sort.SliceStable(instances, func(i, j int) bool {
		return instances[i].App != services.PostgresApp && instances[j].App == services.PostgresApp
	})
	for _, instance := range instances {
		if err := c.Services.Remove(ctx, instance.Name, false); err != nil {
			log.Printf("remise à zéro, service %s : %v", instance.Name, err)
			result.Problems = append(result.Problems, fmt.Sprintf("Le service « %s » n'a pas pu être effacé : %v", instance.Name, err))
			continue
		}
		if c.Alerts != nil {
			c.Alerts.SetStopped(instance.Name, false)
		}
		c.forgetServiceIdentity(instance.Name)
	}
}
