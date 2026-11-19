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

package console

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

type okRunner struct{}

func (okRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, nil
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	manager := &services.Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: okRunner{}}
	return Handler("1.2.3", catalog, manager)
}

func send(handler http.Handler, method, path, remote, contentType string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	if remote != "" {
		request.RemoteAddr = remote
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return send(newHandler(t), http.MethodGet, path, "", "", nil)
}

func install(handler http.Handler, remote, body string) *httptest.ResponseRecorder {
	return send(handler, http.MethodPost, "/pi-appliance/api/services", remote, "application/json", strings.NewReader(body))
}

func TestHealth(t *testing.T) {
	response := get(t, "/pi-appliance/api/health")
	if response.Code != http.StatusOK {
		t.Fatalf("code %d", response.Code)
	}
	var health Health
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health != (Health{Status: "ok", Version: "1.2.3"}) {
		t.Fatalf("réponse %+v", health)
	}
}

func TestConsolePage(t *testing.T) {
	response := get(t, "/pi-appliance/")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<h1>pi-appliance</h1>") {
		t.Fatalf("code %d, page %q", response.Code, response.Body.String())
	}
}

func TestRedirectWithoutSlash(t *testing.T) {
	response := get(t, "/pi-appliance")
	if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != "/pi-appliance/" {
		t.Fatalf("code %d, Location %q", response.Code, response.Header().Get("Location"))
	}
}

func TestOutsideConsole(t *testing.T) {
	if response := get(t, "/"); response.Code != http.StatusNotFound {
		t.Fatalf("code %d", response.Code)
	}
}

func TestCatalog(t *testing.T) {
	response := get(t, "/pi-appliance/api/catalog")
	var body struct {
		Apps []map[string]any `json:"apps"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Apps) == 0 || body.Apps[0]["id"] != "nextcloud" {
		t.Fatalf("catalogue %v", body.Apps)
	}
	if _, leaked := body.Apps[0]["Compose"]; leaked {
		t.Fatal("fichier Compose exposé dans le catalogue")
	}
}

func TestInstallListRemove(t *testing.T) {
	handler := newHandler(t)
	response := install(handler, "127.0.0.1:5000", `{"app": "nextcloud"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	var result services.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Instance.Name != "cloud" || result.Instance.Address != "cloud.maison.example.org" || len(result.Secrets) != 1 {
		t.Fatalf("résultat %+v", result)
	}
	if response := install(handler, "[::1]:5000", `{"app": "nextcloud"}`); response.Code != http.StatusConflict {
		t.Fatalf("doublon : code %d", response.Code)
	}
	list := send(handler, http.MethodGet, "/pi-appliance/api/services", "", "", nil)
	if !strings.Contains(list.Body.String(), `"name":"cloud"`) {
		t.Fatalf("liste %s", list.Body.String())
	}
	remove := func(query string) int {
		return send(handler, http.MethodDelete, "/pi-appliance/api/services/cloud"+query, "127.0.0.1:5000", "", nil).Code
	}
	if code := remove(""); code != http.StatusBadRequest {
		t.Fatalf("sans choix sur les données : code %d", code)
	}
	if code := remove("?data=keep"); code != http.StatusNoContent {
		t.Fatalf("désinstallation : code %d", code)
	}
	if code := remove("?data=keep"); code != http.StatusNotFound {
		t.Fatalf("seconde désinstallation : code %d", code)
	}
}

func TestInstallErrors(t *testing.T) {
	handler := newHandler(t)
	cases := map[string]struct {
		remote, contentType, body string
		code                      int
	}{
		"depuis le réseau":  {"192.168.1.20:5000", "application/json", `{"app": "nextcloud"}`, http.StatusForbidden},
		"pas du JSON":       {"127.0.0.1:5000", "text/plain", `{"app": "nextcloud"}`, http.StatusUnsupportedMediaType},
		"illisible":         {"127.0.0.1:5000", "application/json", `{"app":`, http.StatusBadRequest},
		"champ inconnu":     {"127.0.0.1:5000", "application/json", `{"app": "nextcloud", "privileged": true}`, http.StatusBadRequest},
		"application":       {"127.0.0.1:5000", "application/json", `{"app": "inconnue"}`, http.StatusNotFound},
		"nom":               {"127.0.0.1:5000", "application/json", `{"app": "nextcloud", "name": "Mon Cloud"}`, http.StatusBadRequest},
		"paramètre inconnu": {"127.0.0.1:5000", "application/json", `{"app": "nextcloud", "parameters": {"X": "1"}}`, http.StatusBadRequest},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			response := send(handler, http.MethodPost, "/pi-appliance/api/services", c.remote, c.contentType, strings.NewReader(c.body))
			if response.Code != c.code || !strings.Contains(response.Body.String(), `"error"`) {
				t.Fatalf("code %d, attendu %d : %s", response.Code, c.code, response.Body.String())
			}
		})
	}
}

func TestRemoveFromNetwork(t *testing.T) {
	response := send(newHandler(t), http.MethodDelete, "/pi-appliance/api/services/cloud?data=delete", "10.0.0.2:5000", "", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("code %d", response.Code)
	}
}
