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

import (
	"errors"
	"log"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// Mot de passe oublié par e-mail (spécification 8.5), depuis l'écran de connexion :
//
//	POST api/password/forgot {"name", "lang"}  envoie un lien à chaque compte dont
//	                                           l'identifiant ou l'adresse e-mail est name
//	POST api/password/check  {"token"}         compte d'un lien encore valable
//	POST api/password/reset  {"token", "password"}  nouveau mot de passe
//
// Ouvert sans compte, à la maison comme sur internet (console ouverte) : la
// réponse de forgot est la même que le compte existe ou non.

// ForgotRequest : corps de POST api/password/forgot.
type ForgotRequest struct {
	Name string `json:"name"`
	Lang string `json:"lang"`
}

// ResetLinkRequest : corps de POST api/password/check et api/password/reset.
type ResetLinkRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// sendAsync : l'e-mail part après la réponse, qui ne dit donc pas, par sa
// durée, si un compte correspond. Remplacé dans les tests.
var sendAsync = func(send func()) { go send() }

func (c *console) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var request ForgotRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if c.Mailer == nil {
		writeError(w, http.StatusServiceUnavailable, "La récupération par e-mail n'est pas disponible sur ce Pi. Administrateur : le guide explique comment changer le mot de passe avec le fichier reset. Autre compte : demandez un nouveau lien à l'administrateur.")
		return
	}
	links, err := c.Accounts.RequestReset(request.Name, clientIP(r))
	if errors.Is(err, auth.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "Entrez votre identifiant ou votre adresse e-mail.")
		return
	}
	if err != nil {
		writeAuthError(w, err)
		return
	}
	base := c.consoleBase(r)
	lang := requestLanguage(r, request.Lang)
	for _, link := range links {
		account, url := link.Account.Public(), base+BasePath+"/#reset="+link.Token
		sendAsync(func() {
			// NE PAS journaliser le lien ni l'adresse e-mail.
			if err := c.Mailer.PasswordReset(account, url, lang); err != nil {
				log.Printf("mot de passe oublié, compte %s : e-mail non envoyé : %v", account.Name, err)
			}
		})
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"sent": true})
}

func (c *console) checkResetLink(w http.ResponseWriter, r *http.Request) {
	var request ResetLinkRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.ResetLinkAccount(request.Token)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account.Public())
}

func (c *console) useResetLink(w http.ResponseWriter, r *http.Request) {
	var request ResetLinkRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.UseResetLink(request.Token, request.Password)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	log.Printf("mot de passe oublié : nouveau mot de passe choisi pour %s", account.Name)
	if account.Role == auth.RoleAdmin && c.Accounts.FirstAdmin(account.Name) {
		c.protectBackups(request.Password, false)
	}
	// Double authentification activée : connexion normale ensuite, avec le
	// code ; pas de session ouverte ici.
	if account.TOTPSecret != "" {
		writeJSON(w, http.StatusOK, account.Public())
		return
	}
	openSession(w, r, c.Accounts.StartSession(account.Name))
	writeJSON(w, http.StatusOK, account.Public())
}

// consoleBase : début des liens envoyés par e-mail (https://adresse[:port]).
// NE PAS reprendre l'en-tête Host sans le vérifier : une demande « mot de
// passe oublié » se fait sans compte, et un Host choisi par un attaquant
// enverrait le jeton chez lui. Seules les adresses connues du Pi sont gardées
// (adresses internet, <nom>.local, adresse IP privée) ; sinon <nom>.local.
func (c *console) consoleBase(r *http.Request) string {
	host := strings.ToLower(strings.TrimSpace(r.Host))
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.TrimSuffix(strings.Trim(name, "[]"), ".")
	local := c.piName()
	switch {
	case slices.Contains(c.internetHosts(), name):
		return "https://" + host
	case name == local+".local" || name == local:
	case net.ParseIP(name) != nil:
		ip := net.ParseIP(name)
		if !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return "https://" + local + ".local"
		}
	default:
		return "https://" + local + ".local"
	}
	if secure(r) {
		return "https://" + host
	}
	return "http://" + host
}

// requestLanguage : langue du message, choisie dans la console (asked), sinon
// la première de l'en-tête Accept-Language du navigateur, sinon l'anglais.
func requestLanguage(r *http.Request, asked string) string {
	if asked = strings.ToLower(strings.TrimSpace(asked)); asked != "" {
		return asked
	}
	first, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ",")
	first, _, _ = strings.Cut(strings.TrimSpace(first), ";")
	first, _, _ = strings.Cut(first, "-")
	if first = strings.ToLower(first); first == "" {
		// Sans navigateur (pia) : l'anglais (choix de l'utilisateur du 2026-11-22).
		return "en"
	}
	return first
}
