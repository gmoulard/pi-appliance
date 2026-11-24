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

// Package mail : envoi d'e-mails par le service du projet (spécification 10.2).
// Le Pi n'envoie jamais d'e-mail lui-même (son adresse résidentielle serait
// bloquée) : la console demande au service du projet, par une demande signée
// avec la clé de l'appliance, d'envoyer un message dont le texte est fixé par
// le service. Le Pi ne fournit que le type de message, l'adresse, le nom du
// compte, le lien et la langue : le service ne peut pas servir à envoyer un
// texte libre.
package mail

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

// Échanges entre la console et le service d'envoi :
//
//	POST /api/send  {kind, to, name, link, lang}, signé par la clé de l'appliance
//
// Réponse 202 sans corps ; en erreur, {"error": "…"} en langage courant.

// DefaultServiceURL : service d'envoi d'e-mails du projet, sur le Pi du projet.
const DefaultServiceURL = "https://envoi.pi-appliance.com"

// Kind : type de message, dont le texte est écrit par le service.
type Kind string

const (
	// KindReset : lien « mot de passe oublié » (spécification 8.5), valable une heure.
	KindReset Kind = "reset"
	// KindInvite : lien d'invitation d'un nouveau compte (spécification 8.1), valable 7 jours.
	KindInvite Kind = "invite"
	// Messages envoyés seulement par le SMTP du Pi (smtp.go), pas par le service
	// du projet : code de connexion (double authentification par e-mail),
	// code de vérification de l'adresse, e-mail d'essai.
	KindLoginCode  Kind = "login-code"
	KindVerifyCode Kind = "verify-code"
	KindTest       Kind = "test"
	// KindAlert : alerte aux administrateurs (spécification 8.6), dont le sujet
	// et le texte sont écrits par le paquet alerts (Message.Subject, Message.Body).
	KindAlert Kind = "alert"
)

// Request : corps de POST /api/send.
type Request struct {
	Kind Kind   `json:"kind"`
	To   string `json:"to"`
	Name string `json:"name"`
	Link string `json:"link"`
	Lang string `json:"lang,omitempty"`
}

// ErrorBody : corps d'une réponse en erreur.
type ErrorBody struct {
	Error string `json:"error"`
}

// ErrInvalid : demande refusée avant tout envoi.
var ErrInvalid = errors.New("demande d'e-mail invalide")

var (
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
	// linkPattern : lien de la console, https ou http (réseau de la maison
	// seulement), jeton de 256 bits après # : il ne part pas dans les journaux.
	linkPattern = regexp.MustCompile(`^https?://([a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?)*|\d{1,3}(\.\d{1,3}){3}|\[[0-9a-f:]+\])(:\d{1,5})?/pi-appliance/#(reset|invite)=[A-Za-z0-9_-]{43}$`)
)

// Check vérifie une demande, côté console comme côté service, et la renvoie
// nettoyée. Le lien doit être celui du type de message (#reset= ou #invite=).
func (r Request) Check() (Request, error) {
	r.To = strings.TrimSpace(r.To)
	address, err := mail.ParseAddress(r.To)
	if err != nil || address.Address != r.To || address.Name != "" || len(r.To) > 254 || strings.ContainsAny(r.To, "\r\n") {
		return Request{}, fmt.Errorf("%w : adresse e-mail", ErrInvalid)
	}
	if !namePattern.MatchString(r.Name) {
		return Request{}, fmt.Errorf("%w : identifiant", ErrInvalid)
	}
	switch r.Kind {
	case KindReset, KindInvite:
	default:
		return Request{}, fmt.Errorf("%w : type de message", ErrInvalid)
	}
	r.Link = strings.TrimSpace(r.Link)
	if len(r.Link) > 400 || !linkPattern.MatchString(r.Link) || !strings.Contains(r.Link, "/#"+string(r.Kind)+"=") {
		return Request{}, fmt.Errorf("%w : lien", ErrInvalid)
	}
	r.Lang = Language(r.Lang)
	return r, nil
}

// Language : langue du message ; le français, qui fait foi, et l'anglais pour
// toutes les autres langues de la console.
func Language(lang string) string {
	if strings.ToLower(strings.TrimSpace(lang)) == "fr" {
		return "fr"
	}
	return "en"
}
