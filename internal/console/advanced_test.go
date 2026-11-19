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
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const freeCompose = `services:
  web:
    image: nginx:1.27
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
`

func composeBody(confirm bool) string {
	body, _ := json.Marshal(ComposeRequest{Name: "outils", Compose: freeCompose, Service: "web", Port: 80, ConfirmRisks: confirm})
	return string(body)
}

func TestInstallCompose(t *testing.T) {
	f := newFixture(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/compose", "application/json", composeBody(true), nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/compose", "application/json", composeBody(false), cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"docker-socket"`) {
		t.Fatalf("sans confirmation : code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/compose", "application/json", composeBody(true), cookie)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "ni vérifié ni pris en charge") {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/services/outils/update", "", "", cookie)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "Docker Compose libre") {
		t.Fatalf("mise à jour : code %d : %s", response.Code, response.Body.String())
	}
	bad, _ := json.Marshal(ComposeRequest{Name: "autre", Compose: "services: [", Service: "web", Port: 80})
	if response := f.do(http.MethodPost, "/pi-appliance/api/compose", "application/json", string(bad), cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("fichier illisible : code %d", response.Code)
	}
}

func TestAdvancedSettings(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/services", "application/json", `{"app": "dokuwiki", "name": "wiki"}`, cookie); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	response := f.do(http.MethodGet, "/pi-appliance/api/services/wiki/advanced", "", "", cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"services":["`) {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	var current struct {
		Settings struct {
			Service string `json:"service"`
		} `json:"settings"`
	}
	json.Unmarshal(response.Body.Bytes(), &current)
	body := `{"settings": {"service": "` + current.Settings.Service + `", "volumes": [{"source": "/srv/wiki", "target": "/import"}]}}`
	if response := f.do(http.MethodPut, "/pi-appliance/api/services/wiki/advanced", "application/json", body, cookie); response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "host-mount") {
		t.Fatalf("sans confirmation : code %d : %s", response.Code, response.Body.String())
	}
	confirmed := strings.Replace(body, `{"settings"`, `{"confirm_risks": true, "settings"`, 1)
	if response := f.do(http.MethodPut, "/pi-appliance/api/services/wiki/advanced", "application/json", confirmed, cookie); response.Code != http.StatusOK {
		t.Fatalf("confirmé : code %d : %s", response.Code, response.Body.String())
	}
	invalid := `{"settings": {"service": "` + current.Settings.Service + `", "ports": [{"host": 443, "container": 80}]}}`
	if response := f.do(http.MethodPut, "/pi-appliance/api/services/wiki/advanced", "application/json", invalid, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("port réservé : code %d", response.Code)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services/inconnu/advanced", "", "", cookie); response.Code != http.StatusNotFound {
		t.Fatalf("service inconnu : code %d", response.Code)
	}
}
