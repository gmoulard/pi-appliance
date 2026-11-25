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
	"time"
)

const paulJSON = `{"name": "paul", "email": "paul@example.org", "role": "user", "invited": false, "totp": true, "totp_required": false, "recovery_codes": 8}`

func userPi(t *testing.T, input string) *fakePi {
	pi := newFakePi(t, input)
	pi.routes = map[string]func(w http.ResponseWriter, body []byte){
		"GET accounts": func(w http.ResponseWriter, _ []byte) {
			io.WriteString(w, `{"accounts": [{"name": "marie", "email": "marie@example.org", "role": "admin", "totp": false}, `+paulJSON+`,
				{"name": "ines", "email": "ines@example.org", "role": "guest", "invited": true, "expires_at": "2099-01-01T00:00:00Z", "totp_required": true}]}`)
		},
		"GET accounts/paul": func(w http.ResponseWriter, _ []byte) {
			io.WriteString(w, `{"account": `+paulJSON+`, "devices": [{"name": "Téléphone de Paul", "paired_at": "2026-11-20T10:00:00Z", "last_used": "2026-11-21T18:30:00Z"}],
				"shares": [{"service": "pad", "pad": "courses", "created_at": "2026-11-21T09:00:00Z", "expires_at": "2026-11-28T09:00:00Z"}]}`)
		},
		"POST accounts": func(w http.ResponseWriter, body []byte) {
			var request map[string]any
			json.Unmarshal(body, &request)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"account": map[string]any{"name": request["name"], "email": request["email"], "role": request["role"], "invited": true, "totp_required": request["totp_required"]},
				"link": "https://monpi.local/pi-appliance/#invite=abc"})
		},
		"PUT accounts/paul": func(w http.ResponseWriter, body []byte) {
			var request map[string]any
			json.Unmarshal(body, &request)
			email := "paul@example.org"
			if request["email"] != nil {
				email = request["email"].(string)
			}
			json.NewEncoder(w).Encode(map[string]any{"name": "paul", "email": email, "role": request["role"], "expires_at": request["expires"], "totp_required": request["totp_required"], "totp": true})
		},
		"POST accounts/paul/invitation": func(w http.ResponseWriter, _ []byte) {
			io.WriteString(w, `{"account": `+paulJSON+`, "sent": true}`)
		},
		"DELETE accounts/paul/totp": func(w http.ResponseWriter, _ []byte) {
			io.WriteString(w, `{"name": "paul", "role": "user", "totp": false}`)
		},
		"DELETE accounts/paul": func(w http.ResponseWriter, _ []byte) {
			w.WriteHeader(http.StatusNoContent)
		},
	}
	return pi
}

func (pi *fakePi) lastRequest() string {
	pi.mu.Lock()
	defer pi.mu.Unlock()
	return pi.requests[len(pi.requests)-1]
}

func TestUserList(t *testing.T) {
	pi := userPi(t, "")
	if code := pi.run("user", "list"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	out := pi.out.String()
	for _, want := range []string{"COMPTE", "marie", "administrateur", "paul@example.org", "activée", "ines", "invitation en attente", "exigée, pas encore activée"} {
		if !strings.Contains(out, want) {
			t.Errorf("liste sans %q :\n%s", want, out)
		}
	}
}

func TestUserShow(t *testing.T) {
	pi := userPi(t, "")
	if code := pi.run("user", "show", "Paul"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	out := pi.out.String()
	for _, want := range []string{"paul@example.org", "utilisateur", "Codes de secours restants  8", "Téléphone de Paul", "appairé le 20/11/2026", "pad, « courses »", "jusqu'au 28/11/2026"} {
		if !strings.Contains(out, want) {
			t.Errorf("détail sans %q :\n%s", want, out)
		}
	}
	if code := pi.run("user", "show"); code != 2 {
		t.Fatalf("sans compte : code %d", code)
	}
}

func TestUserAdd(t *testing.T) {
	pi := userPi(t, "")
	if code := pi.run("user", "add", "luc", "luc@example.org"); code != 0 || !strings.Contains(pi.out.String(), "https://monpi.local/pi-appliance/#invite=abc") {
		t.Fatalf("code %d : %s %s", code, pi.out.String(), pi.err.String())
	}
	if last := pi.lastRequest(); !strings.Contains(last, `"role":"user"`) || !strings.Contains(last, `"expires":null`) {
		t.Fatalf("demande %q", last)
	}
	if !strings.Contains(pi.out.String(), "lui sera proposée à sa première connexion") {
		t.Fatalf("proposition de la double authentification : %s", pi.out.String())
	}
	if code := pi.run("user", "add", "ines", "ines@example.org", "--role", "guest", "--until", "2026-12-31", "--require-2fa"); code != 0 {
		t.Fatalf("invité : code %d : %s", code, pi.err.String())
	}
	want, _ := json.Marshal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local))
	if last := pi.lastRequest(); !strings.Contains(last, `"role":"guest"`) || !strings.Contains(last, `"expires":`+string(want)) || !strings.Contains(last, `"totp_required":true`) {
		t.Fatalf("demande %q", last)
	}
	if !strings.Contains(pi.out.String(), "dès sa première connexion, la personne devra l'activer") {
		t.Fatalf("exigence annoncée : %s", pi.out.String())
	}
	count := len(pi.requests)
	for _, args := range [][]string{
		{"user", "add", "ines", "ines@example.org", "--role", "guest"},
		{"user", "add", "luc", "luc@example.org", "--until", "2026-12-31"},
		{"user", "add", "luc", "luc@example.org", "--role", "chef"},
		{"user", "add", "luc"},
	} {
		if code := pi.run(args...); code != 2 {
			t.Errorf("%v : code %d", args, code)
		}
	}
	if len(pi.requests) != count {
		t.Fatal("demande envoyée malgré une mauvaise utilisation")
	}
}

func TestUserEdit(t *testing.T) {
	pi := userPi(t, "")
	if code := pi.run("user", "edit", "paul", "--require-2fa", "yes"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	if last := pi.lastRequest(); !strings.HasPrefix(last, "PUT /pi-appliance/api/accounts/paul") || !strings.Contains(last, `"role":"user"`) || !strings.Contains(last, `"totp_required":true`) {
		t.Fatalf("demande %q", last)
	}
	if code := pi.run("user", "edit", "paul", "--role", "guest", "--until", "2026-12-31"); code != 0 {
		t.Fatalf("invité : code %d : %s", code, pi.err.String())
	}
	if code := pi.run("user", "edit", "paul", "--role", "guest"); code != 2 {
		t.Fatalf("invité sans date : code %d", code)
	}
	if code := pi.run("user", "edit", "paul"); code != 2 {
		t.Fatalf("rien à changer : code %d", code)
	}
	pi.out.Reset()
	if code := pi.run("user", "edit", "paul", "--email", "paul.nouveau@example.org"); code != 0 {
		t.Fatalf("adresse : code %d : %s", code, pi.err.String())
	}
	if last := pi.lastRequest(); !strings.Contains(last, `"email":"paul.nouveau@example.org"`) || !strings.Contains(last, `"role":"user"`) {
		t.Fatalf("demande %q", last)
	}
	if !strings.Contains(pi.out.String(), "Adresse e-mail : paul.nouveau@example.org (avant : paul@example.org)") {
		t.Fatalf("sortie : %s", pi.out.String())
	}
}

func TestUserResetTOTPAndDelete(t *testing.T) {
	pi := userPi(t, "non\n")
	if code := pi.run("user", "reset", "paul"); code != 0 || !strings.Contains(pi.out.String(), "Un e-mail est parti à paul@example.org") || !strings.Contains(pi.out.String(), "reste valable") {
		t.Fatalf("code %d : %s %s", code, pi.out.String(), pi.err.String())
	}
	count := len(pi.requests)
	if code := pi.run("user", "delete", "paul"); code != 1 || len(pi.requests) != count {
		t.Fatalf("refusé à la question : code %d", code)
	}
	if code := pi.run("user", "2fa-off", "paul", "--yes"); code != 0 || pi.lastRequest() != "DELETE /pi-appliance/api/accounts/paul/totp " {
		t.Fatalf("2fa-off : code %d : %s", code, pi.err.String())
	}
	pi.env.In = strings.NewReader("yes\n")
	if code := pi.run("user", "delete", "paul"); code != 0 || pi.lastRequest() != "DELETE /pi-appliance/api/accounts/paul " {
		t.Fatalf("confirmé : code %d : %s", code, pi.err.String())
	}
	pi.err.Reset()
	if code := pi.run("user", "delete", "inconnu", "--yes"); code != 1 || !strings.Contains(pi.err.String(), "n'existe pas") {
		t.Fatalf("compte inconnu : code %d : %s", code, pi.err.String())
	}
}

func TestUserHelp(t *testing.T) {
	pi := userPi(t, "")
	if code := pi.run("user"); code != 0 || !strings.Contains(pi.out.String(), "pia user add <compte> <e-mail>") {
		t.Fatalf("code %d : %s", code, pi.out.String())
	}
	if code := pi.run("user", "frobnicate"); code != 2 || !strings.Contains(pi.err.String(), "Commande inconnue : pia user frobnicate") {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
}
