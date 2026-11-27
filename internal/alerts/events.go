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

package alerts

import "strings"

// Way : comment un mot de passe a été changé, quand ce n'est pas par un compte connecté.
type Way string

const (
	ByAccount   Way = ""
	ByResetLink Way = "link"
	ByResetFile Way = "file"
)

// Event : compte créé ou supprimé (Account, Created), mot de passe changé
// (Password). Name, Email et Lang : le compte concerné ; By : le compte qui
// a fait le changement, vide sans compte connecté (lien reçu par e-mail,
// fichier reset).
type Event struct {
	Type    Type
	Created bool
	Name    string
	Email   string
	Lang    string
	By      string
	Way     Way
}

func (m *Monitor) Notify(e Event) {
	if m.Sender == nil || !m.Sender.Configured() {
		return
	}
	m.mu.Lock()
	m.load()
	disabled := false
	for _, d := range m.state.Disabled {
		disabled = disabled || d == e.Type
	}
	m.mu.Unlock()
	if disabled {
		return
	}
	link := ""
	if m.Link != nil {
		link = m.Link()
	}
	var admins []Recipient
	if m.Recipients != nil {
		admins = m.Recipients()
	}
	for _, r := range admins {
		if r.Name == e.By || r.Name == e.Name {
			continue
		}
		m.sendEvent(e, r, false, link)
	}
	// NE PAS sauter le titulaire qui change lui-même son mot de passe : un voleur
	// du compte le ferait sans que le titulaire en sache rien (décision du 2026-11-26).
	if e.Email != "" && (e.Name != e.By || e.Type == Password) {
		m.sendEvent(e, Recipient{Name: e.Name, Email: e.Email, Lang: e.Lang}, true, link)
	}
}

func (m *Monitor) sendEvent(e Event, r Recipient, concerned bool, link string) {
	subject, body := ComposeEvent(e, concerned, r.Name, link, r.Lang)
	if err := m.Sender.SendAlert(r.Email, r.Name, r.Lang, subject, body); err != nil {
		// NE PAS journaliser l'adresse : elle irait dans le diagnostic.
		m.logf("alertes : e-mail « %s » non envoyé à %s : %v", e.Type, r.Name, err)
		return
	}
	m.logf("alertes : « %s » (%s) envoyé à %s", e.Type, e.Name, r.Name)
}

type eventTexts struct {
	created, deleted, password [2]string
	subjects                   [3]string
	byAccount, byLink, byFile  string
	notMe, footer              string
}

// Textes : [0] pour les administrateurs, [1] pour la personne concernée ;
// {account} et {by} remplacés.
var eventLanguages = map[string]eventTexts{
	"fr": {
		subjects:  [3]string{"pi-appliance : compte créé", "pi-appliance : compte supprimé", "pi-appliance : mot de passe changé"},
		created:   [2]string{"Le compte « {account} » a été créé sur votre Pi{by}.", "Un compte « {account} » a été créé pour vous sur ce Pi{by}."},
		deleted:   [2]string{"Le compte « {account} » a été supprimé de votre Pi{by}.", "Votre compte « {account} » a été supprimé de ce Pi{by} : vous ne pouvez plus vous y connecter."},
		password:  [2]string{"Le mot de passe du compte « {account} » de votre Pi a été changé{by}.", "Le mot de passe de votre compte « {account} » sur ce Pi a été changé{by}."},
		byAccount: " par « {by} »",
		byLink:    " par le lien « Mot de passe oublié » reçu par e-mail",
		byFile:    " par le fichier reset déposé sur la carte SD",
		notMe:     "Si ce changement est inattendu, ouvrez la console ou prévenez un administrateur du Pi.",
		footer:    "Vous recevez cet e-mail parce qu'il concerne votre compte (« {name} ») sur ce Pi.",
	},
	"en": {
		subjects:  [3]string{"pi-appliance: account created", "pi-appliance: account deleted", "pi-appliance: password changed"},
		created:   [2]string{"The account \"{account}\" was created on your Pi{by}.", "An account \"{account}\" was created for you on this Pi{by}."},
		deleted:   [2]string{"The account \"{account}\" was deleted from your Pi{by}.", "Your account \"{account}\" was deleted from this Pi{by}: you can no longer sign in to it."},
		password:  [2]string{"The password of the account \"{account}\" on your Pi was changed{by}.", "The password of your account \"{account}\" on this Pi was changed{by}."},
		byAccount: " by \"{by}\"",
		byLink:    " with the \"Forgot password\" link received by email",
		byFile:    " with the reset file placed on the SD card",
		notMe:     "If this change is unexpected, open the console or tell an administrator of the Pi.",
		footer:    "You receive this email because it is about your account (\"{name}\") on this Pi.",
	},
}

// ComposeEvent écrit le sujet et le texte d'un événement ; concerned : pour
// la personne dont c'est le compte.
func ComposeEvent(e Event, concerned bool, name, link, lang string) (string, string) {
	if strings.ToLower(strings.TrimSpace(lang)) != "fr" {
		lang = "en"
	} else {
		lang = "fr"
	}
	text := eventLanguages[lang]
	words := common[lang]
	who := 0
	if concerned {
		who = 1
	}
	subject, sentence := text.subjects[2], text.password[who]
	if e.Type == Account {
		subject, sentence = text.subjects[1], text.deleted[who]
		if e.Created {
			subject, sentence = text.subjects[0], text.created[who]
		}
	}
	by := ""
	switch {
	case e.By != "":
		by = strings.ReplaceAll(text.byAccount, "{by}", clean(e.By))
	case e.Way == ByResetLink:
		by = text.byLink
	case e.Way == ByResetFile:
		by = text.byFile
	}
	sentence = strings.ReplaceAll(strings.ReplaceAll(sentence, "{account}", clean(e.Name)), "{by}", by)
	var b strings.Builder
	b.WriteString(words[0] + "\n\n" + sentence + "\n" + text.notMe + "\n")
	if link != "" {
		b.WriteString("\n" + words[1] + clean(link) + "\n")
	}
	footer := words[2]
	if concerned {
		footer = text.footer
	}
	b.WriteString("\n" + strings.ReplaceAll(footer, "{name}", clean(name)) + "\n\n")
	b.WriteString(words[3])
	return subject, b.String()
}
