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

// Accès à Cockpit (TODO-3.4, décision de l'utilisateur du 2026-11-24) : par
// défaut les administrateurs, depuis la maison. Un administrateur peut
// l'ouvrir aussi depuis internet, sans double authentification exigée (comme
// la console sur internet, dont il dépend : la connexion se fait sur la
// console), et aussi aux utilisateurs de la console, qui ont alors tous les
// droits sur le Pi (sudo). L'écran l'avertit avant.

import (
	"log"
	"net/http"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/cockpit"
)

// CockpitAccess : état montré par GET api/cockpit/access.
type CockpitAccess struct {
	cockpit.Access
	// ConsoleInternet : la console est ouverte sur internet ; sinon Cockpit
	// ne l'est pas non plus, même avec Internet.
	ConsoleInternet bool `json:"console_internet"`
	// Allowed : ce compte, depuis l'endroit de la demande, peut ouvrir Cockpit.
	Allowed bool `json:"allowed"`
	// Installed, Active : Cockpit installé, et répond (seulement si Allowed).
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
	Path      string `json:"path,omitempty"`
}

// CockpitAccounts : comptes de la console qui ont un compte système de
// Cockpit (cockpit.Logins.Admins) : les administrateurs, et les utilisateurs
// quand l'accès le permet ; jamais les invités ni les invitations en attente.
func CockpitAccounts(accounts *auth.Store, access *cockpit.AccessFile) func() []string {
	return func() []string {
		users := access.Get().Users
		var names []string
		for _, account := range accounts.List() {
			if account.Invited {
				continue
			}
			if account.Role == auth.RoleAdmin || (users && account.Role == auth.RoleUser) {
				names = append(names, account.Name)
			}
		}
		return names
	}
}

func (c *console) cockpitAccountAllowed(account auth.Account) bool {
	return account.Role == auth.RoleAdmin || (account.Role == auth.RoleUser && c.CockpitAccess.Get().Users)
}

// cockpitOnInternet : Cockpit publié sur les adresses internet du Pi.
func (c *console) cockpitOnInternet() bool {
	return c.Cockpit != nil && c.internetEnabled() && c.CockpitAccess.Get().Internet
}

func (c *console) cockpitAccessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+BasePath+"/api/cockpit/access", c.signedIn(c.cockpitAccessStatus))
	mux.HandleFunc("PUT "+BasePath+"/api/cockpit/access", c.adminOnly(c.cockpitAccessSave))
}

func (c *console) cockpitAccessState(r *http.Request, account auth.Account) CockpitAccess {
	state := CockpitAccess{Access: c.CockpitAccess.Get(), ConsoleInternet: c.internetEnabled()}
	state.ChangedBy, state.ChangedAt = "", time.Time{}
	state.Allowed = c.cockpitAccountAllowed(account) && (!fromInternet(r) || c.cockpitOnInternet())
	if state.Allowed {
		if status, err := c.Cockpit.Status(r.Context()); err == nil {
			state.Installed, state.Active, state.Path = status.Installed, status.Active, status.Path
		}
	}
	return state
}

func (c *console) cockpitAccessStatus(w http.ResponseWriter, r *http.Request) {
	account, _ := c.account(r)
	writeJSON(w, http.StatusOK, c.cockpitAccessState(r, account))
}

// cockpitAccessSave : PUT api/cockpit/access {"internet", "users"}. Ouvrir
// plus largement depuis internet est confirmé ; refermer est permis partout.
func (c *console) cockpitAccessSave(w http.ResponseWriter, r *http.Request) {
	account, _ := c.account(r)
	var body struct {
		Internet bool `json:"internet"`
		Users    bool `json:"users"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if c.CockpitAccess == nil {
		writeError(w, http.StatusNotImplemented, "Ce réglage n'existe pas ici.")
		return
	}
	current := c.CockpitAccess.Get()
	// Ouvrir plus largement depuis internet : confirmé (mot de passe et code).
	if (body.Internet && !current.Internet) || (body.Users && !current.Users) {
		if !c.confirmFromInternet(w, r, MailConfirm{}) {
			return
		}
	}
	access := cockpit.Access{Internet: body.Internet, Users: body.Users, ChangedBy: account.Name, ChangedAt: time.Now().UTC()}
	if err := c.CockpitAccess.Set(access); err != nil {
		log.Printf("Cockpit, accès : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage n'a pas pu être enregistré.")
		return
	}
	log.Printf("Cockpit, accès : internet %s, utilisateurs %s, par %s", onOff(access.Internet), onOff(access.Users), account.Name)
	if err := c.syncTraefik(); err != nil {
		log.Printf("Cockpit sur internet, Traefik : %v", err)
		writeError(w, http.StatusInternalServerError, "Le réglage est enregistré, mais Traefik n'a pas pu être mis à jour.")
		return
	}
	writeJSON(w, http.StatusOK, c.cockpitAccessState(r, account))
}

func onOff(on bool) string {
	return map[bool]string{true: "oui", false: "non"}[on]
}
