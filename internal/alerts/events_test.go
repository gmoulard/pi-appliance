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

import (
	"path/filepath"
	"strings"
	"testing"
)

func recipientsOf(sent []sentMail) string {
	var to []string
	for _, mail := range sent {
		to = append(to, mail.to)
	}
	return strings.Join(to, ",")
}

// Aux autres administrateurs et à la personne concernée, jamais à l'auteur,
// tout de suite et à chaque fois.
func TestNotifyEveryTimeButNotTheAuthor(t *testing.T) {
	f := newFixture(t, filepath.Join(t.TempDir(), "alerts.json"))
	event := Event{Type: Account, Created: true, Name: "lea", Email: "lea@example.org", Lang: "fr", By: "marie"}
	for range 2 {
		f.monitor.Notify(event)
		sent := f.sender.take()
		if got := recipientsOf(sent); got != "john@example.org,lea@example.org" {
			t.Fatalf("destinataires %s", got)
		}
		if sent[0].subject != "pi-appliance: account created" || !strings.Contains(sent[0].body, `The account "lea" was created on your Pi by "marie".`) {
			t.Fatalf("administrateur : %s\n%s", sent[0].subject, sent[0].body)
		}
		if sent[1].subject != "pi-appliance : compte créé" || !strings.Contains(sent[1].body, "Un compte « lea » a été créé pour vous sur ce Pi par « marie ».") || !strings.Contains(sent[1].body, "concerne votre compte (« lea »)") {
			t.Fatalf("personne concernée : %s\n%s", sent[1].subject, sent[1].body)
		}
	}
	f.monitor.Notify(Event{Type: Password, Name: "john", Email: "john@example.org", Lang: "en", By: "john"})
	if got := recipientsOf(f.sender.take()); got != "marie@example.org" {
		t.Fatalf("mot de passe changé par la personne elle-même : %s", got)
	}
	f.monitor.Notify(Event{Type: Password, Name: "marie", Email: "marie@example.org", Lang: "fr", Way: ByResetLink})
	sent := f.sender.take()
	if got := recipientsOf(sent); got != "john@example.org,marie@example.org" {
		t.Fatalf("lien reçu par e-mail : %s", got)
	}
	if !strings.Contains(sent[1].body, "Le mot de passe de votre compte « marie » sur ce Pi a été changé par le lien « Mot de passe oublié » reçu par e-mail.") {
		t.Fatal(sent[1].body)
	}
	f.monitor.Notify(Event{Type: Account, Name: "lea", Email: "lea@example.org", By: "john"})
	sent = f.sender.take()
	if got := recipientsOf(sent); got != "marie@example.org,lea@example.org" || !strings.Contains(sent[1].body, `Your account "lea" was deleted from this Pi by "john"`) {
		t.Fatalf("compte supprimé : %s %v", got, sent)
	}
}

func TestNotifyDisabledOrWithoutMail(t *testing.T) {
	f := newFixture(t, filepath.Join(t.TempDir(), "alerts.json"))
	if err := f.monitor.SetEnabled(Password, false); err != nil {
		t.Fatal(err)
	}
	f.monitor.Notify(Event{Type: Password, Name: "lea", Email: "lea@example.org", By: "marie"})
	if sent := f.sender.take(); len(sent) != 0 {
		t.Fatalf("type coupé : %v", sent)
	}
	f.monitor.Notify(Event{Type: Account, Created: true, Name: "lea", By: "marie"})
	if got := recipientsOf(f.sender.take()); got != "john@example.org" {
		t.Fatalf("sans adresse : %s", got)
	}
	f.sender.configured = false
	f.monitor.Notify(Event{Type: Account, Created: true, Name: "lea", Email: "lea@example.org", By: "marie"})
	if sent := f.sender.take(); len(sent) != 0 {
		t.Fatalf("sans serveur d'e-mails : %v", sent)
	}
}
