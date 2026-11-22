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

package service

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/mail"
)

// texts : textes fixes des messages, par type et par langue. {name} et {link}
// sont remplacés par l'identifiant du compte et le lien de la console.
var texts = map[mail.Kind]map[string][2]string{
	mail.KindReset: {
		"fr": {
			"Votre mot de passe pi-appliance",
			"Bonjour,\n\nQuelqu'un a demandé à changer le mot de passe du compte « {name} » de votre appliance pi-appliance.\n\nPour choisir un nouveau mot de passe, ouvrez ce lien dans l'heure :\n\n{link}\n\nLe lien ne sert qu'une fois. Votre identifiant est « {name} » : c'est lui qu'il faut taper pour vous connecter, pas votre adresse e-mail.\n\nSi vous n'avez rien demandé, ignorez ce message : votre mot de passe actuel reste valable et personne ne peut l'utiliser sans ce lien.\n\n-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n",
		},
		"en": {
			"Your pi-appliance password",
			"Hello,\n\nSomeone asked to change the password of the account \"{name}\" on your pi-appliance.\n\nTo choose a new password, open this link within the hour:\n\n{link}\n\nThe link works only once. Your username is \"{name}\": type it to sign in, not your email address.\n\nIf you did not ask for this, ignore this message: your current password still works and nobody can use this link without access to your mailbox.\n\n-- \npi-appliance\nAutomatic message, please do not reply.\n",
		},
	},
	mail.KindInvite: {
		"fr": {
			"Invitation sur une appliance pi-appliance",
			"Bonjour,\n\nUn compte « {name} » vous a été ouvert sur une appliance pi-appliance.\n\nPour choisir votre mot de passe, ouvrez ce lien dans les 7 jours :\n\n{link}\n\nLe lien ne sert qu'une fois. Votre identifiant est « {name} ».\n\nSi vous ne vous attendiez pas à cette invitation, ignorez ce message.\n\n-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n",
		},
		"en": {
			"Invitation to a pi-appliance",
			"Hello,\n\nAn account \"{name}\" has been opened for you on a pi-appliance.\n\nTo choose your password, open this link within 7 days:\n\n{link}\n\nThe link works only once. Your username is \"{name}\".\n\nIf you were not expecting this invitation, ignore this message.\n\n-- \npi-appliance\nAutomatic message, please do not reply.\n",
		},
	},
}

// Compose écrit le message complet (en-têtes et corps) d'une demande vérifiée.
func Compose(from string, request mail.Request, now time.Time) ([]byte, error) {
	text, ok := texts[request.Kind][mail.Language(request.Lang)]
	if !ok {
		return nil, mail.ErrInvalid
	}
	if strings.ContainsAny(from+request.To, "\r\n") {
		return nil, mail.ErrInvalid
	}
	body := strings.NewReplacer("{name}", request.Name, "{link}", request.Link).Replace(text[1])
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	domain := "pi-appliance.com"
	if at := strings.LastIndex(from, "@"); at >= 0 {
		domain = strings.Trim(from[at+1:], "<> ")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: <%s>\r\n", request.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", text[0]))
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), domain)
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	writer := quotedprintable.NewWriter(&b)
	if _, err := writer.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
