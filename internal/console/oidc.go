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
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/oidc"
	"github.com/gmoulard/pi-appliance/internal/services"
)

// La console fournisseur OpenID Connect pour les services (spécification 8.1,
// TODO-4.1) : adresses publiques sous /pi-appliance/oidc (paquet oidc), reprise
// de la demande par la page de la console une fois connecté (?oidc=<demande>),
// clients enregistrés par l'administrateur pour un service installé, et
// services autorisés par compte.

// OIDCPath : adresses du fournisseur ; l'émetteur est https://<adresse>/pi-appliance/oidc.
const OIDCPath = BasePath + "/oidc"

// OIDCApproveRequest : POST api/oidc/authorize.
type OIDCApproveRequest struct {
	Request string `json:"request"`
}

// OIDCClientRequest : POST api/oidc/clients.
type OIDCClientRequest struct {
	Service      string   `json:"service"`
	RedirectURIs []string `json:"redirect_uris"`
}

// OIDCClientReply : le client enregistré et son secret, montré une seule fois.
type OIDCClientReply struct {
	Client oidc.PublicClient `json:"client"`
	Secret string            `json:"secret"`
	Issuer string            `json:"issuer"`
}

// ServicesRequest : PUT api/accounts/{name}/services.
type ServicesRequest struct {
	Restrict bool     `json:"restrict"`
	Services []string `json:"services"`
}

func (c *console) oidcRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PUT "+BasePath+"/api/accounts/{name}/services", c.adminOnly(c.setAccountServices))
	if c.OIDC == nil {
		return
	}
	p := c.OIDC
	if p.Issuer == nil {
		p.Issuer = c.oidcIssuer
	}
	if p.Accounts == nil {
		p.Accounts = c.Accounts
	}
	if p.Allowed == nil {
		p.Allowed = func(account auth.Account, service string) bool { return account.MayOpen(service) }
	}
	if p.LoginURL == nil {
		p.LoginURL = func(id string) string {
			return "https://" + strings.ToLower(c.Services.CurrentDomain()) + BasePath + "/?oidc=" + url.QueryEscape(id)
		}
	}
	p.Routes(mux, OIDCPath)
	mux.HandleFunc("POST "+BasePath+"/api/oidc/authorize", c.signedIn(c.oidcApprove))
	mux.HandleFunc("GET "+BasePath+"/api/oidc/clients", c.adminOnly(c.oidcClients))
	mux.HandleFunc("POST "+BasePath+"/api/oidc/clients", c.adminOnly(c.oidcRegister))
	mux.HandleFunc("DELETE "+BasePath+"/api/oidc/clients/{service}", c.adminOnly(c.oidcRemove))
}

// oidcIssuer : https://<adresse de la console>/pi-appliance/oidc, adresse de
// la maison (<nom>.local) : les services s'y connectent depuis la maison.
func (c *console) oidcIssuer() string {
	return "https://" + strings.ToLower(c.Services.CurrentDomain()) + OIDCPath
}

func (c *console) oidcApprove(w http.ResponseWriter, r *http.Request) {
	var request OIDCApproveRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, _ := c.account(r)
	target, err := c.OIDC.Approve(request.Request, account)
	switch {
	case errors.Is(err, oidc.ErrNotAllowed):
		writeError(w, http.StatusForbidden, "Votre compte n'a pas accès à ce service : demandez-le à l'administrateur du Pi.")
	case err != nil:
		writeError(w, http.StatusBadRequest, "Cette demande de connexion a expiré : rouvrez le service.")
	default:
		writeJSON(w, http.StatusOK, map[string]string{"url": target})
	}
}

func (c *console) oidcClients(w http.ResponseWriter, _ *http.Request) {
	clients, err := c.OIDC.Clients.List()
	if err != nil {
		log.Printf("OpenID Connect : %v", err)
		writeError(w, http.StatusInternalServerError, "La liste des services reliés est illisible.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issuer": c.oidcIssuer(), "clients": clients})
}

func (c *console) installedService(name string) bool {
	instances, err := c.Services.List()
	if err != nil {
		return false
	}
	return slices.ContainsFunc(instances, func(i services.Instance) bool { return i.Name == name })
}

func (c *console) oidcRegister(w http.ResponseWriter, r *http.Request) {
	var request OIDCClientRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if !c.installedService(request.Service) {
		writeError(w, http.StatusNotFound, "Ce service n'est pas installé.")
		return
	}
	client, secret, err := c.OIDC.Clients.Register(request.Service, request.RedirectURIs, time.Now())
	if err != nil {
		if errors.Is(err, oidc.ErrInvalidClient) {
			writeError(w, http.StatusBadRequest, "Adresse de retour refusée : il faut une adresse complète en https.")
			return
		}
		log.Printf("OpenID Connect : %v", err)
		writeError(w, http.StatusInternalServerError, "Le service n'a pas pu être relié.")
		return
	}
	account, _ := c.account(r)
	log.Printf("OpenID Connect : %s relié par %s", request.Service, account.Name)
	writeJSON(w, http.StatusCreated, OIDCClientReply{Client: client, Secret: secret, Issuer: c.oidcIssuer()})
}

func (c *console) oidcRemove(w http.ResponseWriter, r *http.Request) {
	if err := c.OIDC.Clients.RemoveService(r.PathValue("service")); err != nil {
		log.Printf("OpenID Connect : %v", err)
		writeError(w, http.StatusInternalServerError, "Le service n'a pas pu être détaché.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *console) setAccountServices(w http.ResponseWriter, r *http.Request) {
	var request ServicesRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	account, err := c.Accounts.SetServices(r.PathValue("name"), request.Restrict, request.Services)
	if err != nil {
		writeAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account.Public())
}

// forgetServiceIdentity : un service désinstallé n'est plus relié ni autorisé.
func (c *console) forgetServiceIdentity(name string) {
	if c.OIDC != nil {
		if err := c.OIDC.Clients.RemoveService(name); err != nil {
			log.Printf("OpenID Connect, %s : %v", name, err)
		}
	}
	if err := c.Accounts.ForgetService(name); err != nil {
		log.Printf("services autorisés, %s : %v", name, err)
	}
}

// serviceOf : nom du service d'une adresse demandée à l'authentification
// déléguée (<service>-<nom>.local, domaine des services, ou chemin sous
// l'adresse internet du Pi) ; vide si ce n'est pas un service installé.
func (c *console) serviceOf(host, uri string) string {
	if _, _, published, _ := c.forwardTarget(host, uri); published {
		path, _, _ := strings.Cut(uri, "?")
		first, _, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
		return first
	}
	host = requestHostOf(host)
	instances, err := c.Services.List()
	if err != nil {
		return ""
	}
	for _, instance := range instances {
		if instance.Address != "" && strings.EqualFold(instance.Address, host) {
			return instance.Name
		}
	}
	return ""
}
