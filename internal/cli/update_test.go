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
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const versionsReply = `{"current": "v0.1.6", "channel": "stable", "versions": [
	{"version": "v0.1.10", "installable": true, "newer": true},
	{"version": "v0.1.6", "installable": true, "current": true},
	{"version": "v0.1.5", "installable": true},
	{"version": "v0.1.0"}]}`

// fakeUpdates : la console installe la version demandée en quelques
// relectures : téléchargement, installation, console qui ne répond plus, fin.
func fakeUpdates(t *testing.T, pi *fakePi, final string) {
	t.Helper()
	updatePoll, checkWait = time.Millisecond, time.Second
	t.Cleanup(func() { updatePoll, updateTimeout, checkWait = 2*time.Second, 20*time.Minute, 30*time.Second })
	reads, requested := 0, ""
	old := `{"state": "done", "version": "v0.1.5", "at": "2026-11-20T10:00:00Z"}`
	pi.routes = map[string]func(http.ResponseWriter, []byte){
		"POST updates/check":   func(w http.ResponseWriter, _ []byte) { w.WriteHeader(http.StatusAccepted) },
		"GET updates/versions": func(w http.ResponseWriter, _ []byte) { io.WriteString(w, versionsReply) },
		"POST updates/appliance": func(w http.ResponseWriter, body []byte) {
			requested = "v0.1.10"
			if strings.Contains(string(body), "v0.1.5") {
				requested = "v0.1.5"
			}
			w.WriteHeader(http.StatusAccepted)
		},
		"GET updates": func(w http.ResponseWriter, _ []byte) {
			upgrade := old
			if requested != "" {
				reads++
				switch {
				case reads == 1:
				case reads == 2:
					upgrade = fmt.Sprintf(`{"state": "running", "version": %q, "at": "2026-11-21T19:00:00Z"}`, requested)
				case reads == 3:
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				default:
					upgrade = fmt.Sprintf(final, requested)
				}
			}
			fmt.Fprintf(w, `{"checking": false, "system": {"upgrade": %s},
				"appliance": {"current": "v0.1.6", "channel": "stable", "latest": "v0.1.10", "available": true, "installable": true}}`, upgrade)
		},
	}
}

func TestUpdateLatest(t *testing.T) {
	pi := newFakePi(t, "o\n")
	fakeUpdates(t, pi, `{"state": "done", "version": %q, "at": "2026-11-21T19:02:00Z"}`)
	if code := pi.run("update"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	for _, want := range []string{"Installer pi-appliance v0.1.10 (le Pi est en v0.1.6)", "téléchargement", "installation", "la console redémarre", "pi-appliance v0.1.10 est installé."} {
		if !strings.Contains(pi.out.String(), want) {
			t.Errorf("sans %q :\n%s", want, pi.out.String())
		}
	}
}

func TestUpdateRefused(t *testing.T) {
	pi := newFakePi(t, "n\n")
	fakeUpdates(t, pi, "")
	if code := pi.run("mise-a-jour"); code != 0 || !strings.Contains(pi.out.String(), "Rien n'a été changé.") {
		t.Fatalf("code %d : %s %s", code, pi.out.String(), pi.err.String())
	}
	for _, request := range pi.requests {
		if strings.HasPrefix(request, "POST /pi-appliance/api/updates/appliance") {
			t.Fatalf("installation lancée sans accord : %v", pi.requests)
		}
	}
}

func TestUpdateList(t *testing.T) {
	pi := newFakePi(t, "")
	fakeUpdates(t, pi, "")
	if code := pi.run("update", "list"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	for _, want := range []string{"canal stable", "v0.1.10  (dernière)", "v0.1.6  (installée sur ce Pi)", "  v0.1.5\n", "v0.1.0  (image à flasher"} {
		if !strings.Contains(pi.out.String(), want) {
			t.Errorf("sans %q :\n%s", want, pi.out.String())
		}
	}
}

func TestUpdateToVersion(t *testing.T) {
	pi := newFakePi(t, "")
	fakeUpdates(t, pi, `{"state": "failed", "version": %q, "message": "La version n'a pas démarré.", "rolled_back": true, "at": "2026-11-21T19:02:00Z"}`)
	// Version plus ancienne : avertissement, puis échec et retour en arrière.
	pi.env.In = strings.NewReader("oui\n")
	if code := pi.run("update", "0.1.5"); code != 1 || !strings.Contains(pi.err.String(), "l'ancienne version a été remise") {
		t.Fatalf("code %d : %s %s", code, pi.out.String(), pi.err.String())
	}
	if !strings.Contains(pi.out.String(), "Revenir à pi-appliance v0.1.5, plus ancienne que v0.1.6") {
		t.Errorf("sans avertissement :\n%s", pi.out.String())
	}
	found := false
	for _, request := range pi.requests {
		found = found || request == `POST /pi-appliance/api/updates/appliance {"version":"v0.1.5"}`
	}
	if !found {
		t.Fatalf("version non demandée : %v", pi.requests)
	}
	for args, want := range map[string]string{"v0.1.6": "déjà en v0.1.6", "v0.1.0": "flasher son image", "v9.9.9": "n'est pas dans la liste"} {
		pi.out.Reset()
		pi.err.Reset()
		pi.run("update", args, "--oui")
		if !strings.Contains(pi.out.String()+pi.err.String(), want) {
			t.Errorf("%s : sans %q : %s %s", args, want, pi.out.String(), pi.err.String())
		}
	}
}
