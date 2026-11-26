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

// Package mail : envoi des e-mails du Pi par le serveur SMTP choisi par
// l'administrateur (smtp.go, spécification 10.2). Le texte des messages est
// fixé ici (message.go) : seuls le type de message, l'adresse, le nom du
// compte, le lien ou le code et la langue changent.
package mail

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
)

// Kind : type de message, dont le texte est écrit ici (message.go).
type Kind string

const (
	// KindReset : lien « mot de passe oublié » (spécification 8.5), valable une heure.
	KindReset Kind = "reset"
	// KindInvite : lien d'invitation d'un nouveau compte (spécification 8.1), valable 7 jours.
	KindInvite Kind = "invite"
	// Code de connexion (double authentification par e-mail), code de
	// vérification de l'adresse, e-mail d'essai.
	KindLoginCode  Kind = "login-code"
	KindVerifyCode Kind = "verify-code"
	KindTest       Kind = "test"
	// KindAlert : alerte aux administrateurs (spécification 8.6), dont le sujet
	// et le texte sont écrits par le paquet alerts (Message.Subject, Message.Body).
	KindAlert Kind = "alert"
	// KindService : e-mail d'un service du Pi passé par le relais
	// (internal/relay, TODO-2.74), sujet et texte écrits par le service.
	KindService Kind = "service"
)

// Request : e-mail d'un lien (invitation, mot de passe oublié) à vérifier
// avant l'envoi.
type Request struct {
	Kind Kind
	To   string
	Name string
	Link string
	Lang string
}

// ErrInvalid : demande refusée avant tout envoi.
var ErrInvalid = errors.New("demande d'e-mail invalide")

var (
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)
	// linkPattern : lien de la console, https ou http (réseau de la maison
	// seulement), jeton de 256 bits après # : il ne part pas dans les journaux.
	linkPattern = regexp.MustCompile(`^https?://([a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?)*|\d{1,3}(\.\d{1,3}){3}|\[[0-9a-f:]+\])(:\d{1,5})?/pi-appliance/#(reset|invite)=[A-Za-z0-9_-]{43}$`)
)

// Check vérifie une demande et la renvoie
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
