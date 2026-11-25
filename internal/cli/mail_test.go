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

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const mailJSON = `{"configured": true, "host": "smtp.free.fr", "port": 465, "security": "tls", "username": "jean.dupont", "from": "jean.dupont@free.fr", "password_set": true, "verified": true, "verified_at": "2026-11-25T10:00:00Z", "updated_at": "2026-11-25T09:00:00Z", "updated_by": "marie", "email_code_accounts": 1}`

func mailPi(t *testing.T, input string, configured bool) *fakePi {
	pi := newFakePi(t, input)
	status := `{"configured": false, "port": 587, "security": "starttls"}`
	if configured {
		status = mailJSON
	}
	pi.routes = map[string]func(w http.ResponseWriter, body []byte){
		"GET mail":    func(w http.ResponseWriter, _ []byte) { io.WriteString(w, status) },
		"PUT mail":    func(w http.ResponseWriter, _ []byte) { io.WriteString(w, mailJSON) },
		"DELETE mail": func(w http.ResponseWriter, _ []byte) { io.WriteString(w, `{"configured": false}`) },
		"POST mail/test": func(w http.ResponseWriter, body []byte) {
			var request map[string]any
			json.Unmarshal(body, &request)
			if request["to"] == "refus@example.org" {
				w.WriteHeader(http.StatusBadGateway)
				io.WriteString(w, `{"error": "L'e-mail n'est pas parti. Le serveur d'envoi refuse l'identifiant ou le mot de passe."}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"sent": true, "email": request["to"], "status": json.RawMessage(mailJSON)})
		},
	}
	return pi
}

func TestMailShow(t *testing.T) {
	pi := mailPi(t, "", true)
	if code := pi.run("mail"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	for _, want := range []string{"smtp.free.fr, port 465, TLS", "jean.dupont ; mot de passe : enregistré", "jean.dupont@free.fr", "Vérifié le"} {
		if !strings.Contains(pi.out.String(), want) {
			t.Errorf("sans %q : %s", want, pi.out.String())
		}
	}
	empty := mailPi(t, "", false)
	if code := empty.run("email"); code != 0 || !strings.Contains(empty.out.String(), "pas réglé") {
		t.Fatalf("pas réglé : code %d : %s", code, empty.out.String())
	}
}

// Le mot de passe est lu sur l'entrée standard, masqué par stty, jamais sur la
// ligne de commande ; --essai n'enregistre que si l'e-mail part.
func TestMailSet(t *testing.T) {
	pi := mailPi(t, "secret du compte\n", false)
	if code := pi.run("mail", "regler", "--serveur", "smtp.free.fr", "--securite", "tls", "--identifiant", "jean.dupont", "--expediteur", "jean.dupont@free.fr"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	last := pi.lastRequest()
	if !strings.HasPrefix(last, "PUT /pi-appliance/api/mail") || !strings.Contains(last, `"password":"secret du compte"`) || !strings.Contains(last, `"security":"tls"`) || !strings.Contains(last, `"from":"jean.dupont@free.fr"`) {
		t.Fatalf("demande %q", last)
	}
	if strings.Contains(pi.out.String(), "secret du compte") || !strings.Contains(strings.Join(pi.commands, "\n"), "stty -F /dev/tty -echo") {
		t.Fatalf("mot de passe affiché ou non masqué : %s %v", pi.out.String(), pi.commands)
	}
	// Réglage enregistré, Entrée seule : le mot de passe est gardé (vide envoyé).
	keep := mailPi(t, "\n", true)
	if code := keep.run("mail", "regler", "--expediteur", "jean.dupont@free.fr", "--essai", "marie@example.org"); code != 0 {
		t.Fatalf("essai : code %d : %s", code, keep.err.String())
	}
	last = keep.lastRequest()
	if !strings.HasPrefix(last, "POST /pi-appliance/api/mail/test") || !strings.Contains(last, `"to":"marie@example.org"`) || !strings.Contains(last, `"host":"smtp.free.fr"`) || !strings.Contains(last, `"port":465`) || !strings.Contains(last, `"password":""`) {
		t.Fatalf("demande %q", last)
	}
	if !strings.Contains(keep.out.String(), "réglage vérifié et enregistré") {
		t.Fatalf("sortie : %s", keep.out.String())
	}
	if code := pi.run("mail", "regler"); code != 2 {
		t.Fatalf("sans serveur : code %d", code)
	}
	if code := pi.run("mail", "regler", "--serveur", "smtp.free.fr", "--securite", "aucune"); code != 2 {
		t.Fatalf("sécurité inconnue : code %d", code)
	}
}

func TestMailTestAndForget(t *testing.T) {
	pi := mailPi(t, "non\n", true)
	if code := pi.run("mail", "essai", "marie@example.org"); code != 0 || !strings.Contains(pi.out.String(), "envoyé à marie@example.org") {
		t.Fatalf("essai : code %d : %s %s", code, pi.out.String(), pi.err.String())
	}
	if code := pi.run("mail", "essai", "refus@example.org"); code != 1 || !strings.Contains(pi.err.String(), "refuse l'identifiant") {
		t.Fatalf("refusé : code %d : %s", code, pi.err.String())
	}
	if code := pi.run("mail", "essai"); code != 2 {
		t.Fatalf("sans adresse : code %d", code)
	}
	count := len(pi.requests)
	if code := pi.run("mail", "effacer"); code != 0 || len(pi.requests) != count+1 || !strings.Contains(pi.out.String(), "Rien n'a changé") {
		t.Fatalf("refusé à la question : code %d : %s", code, pi.out.String())
	}
	if code := pi.run("mail", "effacer", "--oui"); code != 0 || pi.lastRequest() != "DELETE /pi-appliance/api/mail " {
		t.Fatalf("effacé : code %d : %s", code, pi.err.String())
	}
}
