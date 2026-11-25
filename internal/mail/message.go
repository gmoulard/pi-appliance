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

package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"regexp"
	"strings"
	"time"
)

// Message : un e-mail du Pi, dont le texte est fixé ici (texts) ; seuls
// l'identifiant du compte, le lien ou le code changent.
type Message struct {
	Kind Kind
	To   string
	Name string
	Link string
	Code string
	Lang string
	// Subject, Body : sujet et texte d'une alerte (KindAlert) seulement,
	// écrits par le paquet alerts, jamais par une personne.
	Subject string
	Body    string
}

var codePattern = regexp.MustCompile(`^[0-9]{6}$`)

// texts : textes fixes des messages, par type et par langue (sujet, corps).
// {name}, {link} et {code} sont remplacés par l'identifiant du compte, le
// lien de la console et le code à 6 chiffres.
var texts = map[Kind]map[string][2]string{
	KindReset: {
		"fr": {
			"Votre mot de passe pi-appliance",
			"Bonjour,\n\nQuelqu'un a demandé à changer le mot de passe du compte « {name} » de votre appliance pi-appliance.\n\nPour choisir un nouveau mot de passe, ouvrez ce lien dans l'heure :\n\n{link}\n\nLe lien ne sert qu'une fois. Votre identifiant est « {name} » : c'est lui qu'il faut taper pour vous connecter, pas votre adresse e-mail.\n\nSi vous n'avez rien demandé, ignorez ce message : votre mot de passe actuel reste valable et personne ne peut l'utiliser sans ce lien.\n\n-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n",
		},
		"en": {
			"Your pi-appliance password",
			"Hello,\n\nSomeone asked to change the password of the account \"{name}\" on your pi-appliance.\n\nTo choose a new password, open this link within the hour:\n\n{link}\n\nThe link works only once. Your username is \"{name}\": type it to sign in, not your email address.\n\nIf you did not ask for this, ignore this message: your current password still works and nobody can use this link without access to your mailbox.\n\n-- \npi-appliance\nAutomatic message, please do not reply.\n",
		},
	},
	KindInvite: {
		"fr": {
			"Invitation sur une appliance pi-appliance",
			"Bonjour,\n\nUn compte « {name} » vous a été ouvert sur une appliance pi-appliance.\n\nPour choisir votre mot de passe, ouvrez ce lien dans les 7 jours :\n\n{link}\n\nLe lien ne sert qu'une fois. Votre identifiant est « {name} ».\n\nSi vous ne vous attendiez pas à cette invitation, ignorez ce message.\n\n-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n",
		},
		"en": {
			"Invitation to a pi-appliance",
			"Hello,\n\nAn account \"{name}\" has been opened for you on a pi-appliance.\n\nTo choose your password, open this link within 7 days:\n\n{link}\n\nThe link works only once. Your username is \"{name}\".\n\nIf you were not expecting this invitation, ignore this message.\n\n-- \npi-appliance\nAutomatic message, please do not reply.\n",
		},
	},
	KindLoginCode: {
		"fr": {
			"Votre code de connexion pi-appliance",
			"Bonjour,\n\nVoici le code pour vous connecter au compte « {name} » de votre appliance pi-appliance :\n\n{code}\n\nIl est valable 10 minutes et ne sert qu'une fois.\n\nSi vous n'essayez pas de vous connecter en ce moment, ne donnez ce code à personne : quelqu'un connaît peut-être votre mot de passe. Changez-le dans « Mon compte ».\n\n-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n",
		},
		"en": {
			"Your pi-appliance sign-in code",
			"Hello,\n\nHere is the code to sign in to the account \"{name}\" on your pi-appliance:\n\n{code}\n\nIt is valid for 10 minutes and works only once.\n\nIf you are not trying to sign in right now, do not give this code to anyone: someone may know your password. Change it in \"My account\".\n\n-- \npi-appliance\nAutomatic message, please do not reply.\n",
		},
	},
	KindVerifyCode: {
		"fr": {
			"Vérifiez votre adresse e-mail pi-appliance",
			"Bonjour,\n\nPour confirmer que cette adresse est bien celle du compte « {name} » de votre appliance pi-appliance, entrez ce code dans la console :\n\n{code}\n\nIl est valable 10 minutes et ne sert qu'une fois.\n\nSi vous n'avez rien demandé, ignorez ce message.\n\n-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n",
		},
		"en": {
			"Confirm your pi-appliance email address",
			"Hello,\n\nTo confirm that this address belongs to the account \"{name}\" on your pi-appliance, enter this code in the console:\n\n{code}\n\nIt is valid for 10 minutes and works only once.\n\nIf you did not ask for this, ignore this message.\n\n-- \npi-appliance\nAutomatic message, please do not reply.\n",
		},
	},
	KindTest: {
		"fr": {
			"E-mail d'essai de pi-appliance",
			"Bonjour,\n\nCet e-mail d'essai a été envoyé par votre appliance pi-appliance, à la demande du compte « {name} ».\n\nS'il est bien arrivé, l'envoi des e-mails fonctionne : le Pi pourra envoyer les invitations, les liens « mot de passe oublié » et les codes de connexion.\n\n-- \npi-appliance\nMessage envoyé automatiquement, merci de ne pas y répondre.\n",
		},
		"en": {
			"Test email from pi-appliance",
			"Hello,\n\nThis test email was sent by your pi-appliance, at the request of the account \"{name}\".\n\nIf it arrived, sending emails works: the Pi can send invitations, \"forgot password\" links and sign-in codes.\n\n-- \npi-appliance\nAutomatic message, please do not reply.\n",
		},
	},
}

// Compose écrit le message complet (en-têtes et corps). from : expéditeur
// déjà mis en forme (« pi-appliance <adresse> »).
func Compose(from string, m Message, now time.Time) ([]byte, error) {
	text, ok := texts[m.Kind][Language(m.Lang)]
	if m.Kind == KindAlert || m.Kind == KindService {
		// Le texte de l'alerte peut tenir sur plusieurs lignes, jamais le sujet.
		if m.Subject == "" || m.Body == "" || strings.ContainsAny(m.Subject, "\r\n") || strings.Contains(m.Body, "\r") || len(m.Subject) > 200 || len(m.Body) > 20000 {
			return nil, ErrInvalid
		}
		text, ok = [2]string{m.Subject, m.Body}, true
	}
	if !ok {
		return nil, ErrInvalid
	}
	if strings.ContainsAny(from+m.To+m.Name+m.Link+m.Code, "\r\n") {
		return nil, ErrInvalid
	}
	switch m.Kind {
	case KindLoginCode, KindVerifyCode:
		if !codePattern.MatchString(m.Code) {
			return nil, ErrInvalid
		}
	}
	body := strings.NewReplacer("{name}", m.Name, "{link}", m.Link, "{code}", m.Code).Replace(text[1])
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
	fmt.Fprintf(&b, "To: <%s>\r\n", m.To)
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
