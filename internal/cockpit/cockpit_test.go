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

package cockpit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeScript tient lieu de cockpit.sh : une réponse par commande.
type fakeScript struct {
	replies map[string]string
	fails   map[string]bool
	calls   []string
	stdin   []string
}

func (f *fakeScript) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	command := strings.Join(args, " ")
	f.calls = append(f.calls, command)
	f.stdin = append(f.stdin, string(stdin))
	if f.fails[command] {
		return []byte(f.replies[command]), errors.New("exit status 1")
	}
	return []byte(f.replies[command]), nil
}

func serve(t *testing.T, script *fakeScript) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	client := &Client{Runner: script}
	client.Routes(mux, "/api/cockpit", func(h http.HandlerFunc) http.HandlerFunc { return h }, func(*http.Request) string { return "marie" })
	return mux
}

func do(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestStatus(t *testing.T) {
	script := &fakeScript{replies: map[string]string{
		"status": `{"installed": true, "active": true, "accounts": ["marie"], "path": "/cockpit/", "job": {"action": "install", "state": "running"}}`,
	}}
	client := &Client{Runner: script}
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || status.Path != "/cockpit/" || !status.Busy() || len(status.Accounts) != 1 {
		t.Fatalf("état : %+v", status)
	}
	response := do(serve(t, script), http.MethodGet, "/api/cockpit", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"installed":true`) || !strings.Contains(response.Body.String(), `"user":"marie"`) {
		t.Fatalf("GET : %d %s", response.Code, response.Body)
	}
}

func TestInstallWithoutPassword(t *testing.T) {
	script := &fakeScript{replies: map[string]string{"install": `{"ok": true, "started": true}`}}
	response := do(serve(t, script), http.MethodPost, "/api/cockpit/install", ``)
	if response.Code != http.StatusAccepted {
		t.Fatalf("install : %d %s", response.Code, response.Body)
	}
	if len(script.calls) != 1 || script.calls[0] != "install" || script.stdin[0] != "" {
		t.Fatalf("appels : %q, entrée %q", script.calls, script.stdin)
	}
	if response := do(serve(t, script), http.MethodPost, "/api/cockpit/password", `{"password": "x"}`); response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("plus de mot de passe à choisir : %d", response.Code)
	}
}

func TestScriptRefusal(t *testing.T) {
	script := &fakeScript{
		replies: map[string]string{"remove": `{"ok": false, "error": "busy", "message": "Une installation est déjà en cours."}`},
		fails:   map[string]bool{"remove": true},
	}
	response := do(serve(t, script), http.MethodDelete, "/api/cockpit", "")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "déjà en cours") {
		t.Fatalf("retrait refusé : %d %s", response.Code, response.Body)
	}
}

func TestErrorNeverShowsPassword(t *testing.T) {
	script := &fakeScript{fails: map[string]bool{"sync": true}}
	_, err := (&Client{Runner: script}).Sync(context.Background(), map[string]string{"marie": "un-mot-de-passe-tres-solide"})
	if err == nil || strings.Contains(err.Error(), "solide") {
		t.Fatalf("erreur : %v", err)
	}
}
