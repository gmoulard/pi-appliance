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

package console

import (
	"encoding/json"
	"net/http"
	"os"
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

func TestRebuildCompose(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	if response := f.do(http.MethodPost, "/pi-appliance/api/compose", "application/json", composeBody(true), admin); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	accept := f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil)
	paul := sessionCookie(t, accept)
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/outils/rebuild?data=delete", "", "", paul); response.Code != http.StatusForbidden {
		t.Fatalf("par un utilisateur : code %d", response.Code)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services/outils/cleanup", "", "", paul); response.Code != http.StatusForbidden {
		t.Fatalf("dossiers à vider, par un utilisateur : code %d", response.Code)
	}
	// Prise Docker présente : laissée telle quelle (2026-11-26) ; absente de
	// cette machine, /var/run/docker.sock n'est qu'un chemin d'un dossier système.
	if info, err := os.Stat("/var/run/docker.sock"); err == nil && info.Mode()&os.ModeSocket != 0 {
		if response := f.do(http.MethodGet, "/pi-appliance/api/services/outils/cleanup", "", "", admin); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"kept":["/var/run/docker.sock"]`) {
			t.Fatalf("prise Docker montée : code %d : %s", response.Code, response.Body.String())
		}
	} else {
		if response := f.do(http.MethodGet, "/pi-appliance/api/services/outils/cleanup", "", "", admin); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "docker.sock") {
			t.Fatalf("chemin système monté : code %d : %s", response.Code, response.Body.String())
		}
		if response := f.do(http.MethodPost, "/pi-appliance/api/services/outils/rebuild?data=delete", "", "", admin); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "rien n'a été effacé") {
			t.Fatalf("Rebuild cleanup avec un chemin système : code %d : %s", response.Code, response.Body.String())
		}
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/outils/rebuild", "", "", admin); response.Code != http.StatusBadRequest {
		t.Fatalf("sans data : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/outils/rebuild?data=keep", "", "", admin); response.Code != http.StatusOK {
		t.Fatalf("data=keep : code %d : %s", response.Code, response.Body.String())
	}
	simple := `{"name": "simple", "compose": "services:\n  web:\n    image: nginx:1.27\n", "service": "web", "port": 80}`
	if response := f.do(http.MethodPost, "/pi-appliance/api/compose", "application/json", simple, admin); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services/simple/cleanup", "", "", admin); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"dirs":[]`) {
		t.Fatalf("dossiers à vider : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/simple/rebuild?data=delete", "", "", admin); response.Code != http.StatusOK {
		t.Fatalf("data=delete : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services", "application/json", `{"app": "dokuwiki", "name": "wiki"}`, admin); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/services/wiki/rebuild?data=keep", "", "", admin); response.Code != http.StatusBadRequest {
		t.Fatalf("service du catalogue : code %d : %s", response.Code, response.Body.String())
	}
}

func TestComposeSourceAPI(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	body, _ := json.Marshal(ComposeRequest{Name: "outils", Compose: freeCompose, Service: "web", Port: 80, ConfirmRisks: true, SourceURL: "https://github.com/exemple/depot/blob/main/compose.yaml"})
	if response := f.do(http.MethodPost, "/pi-appliance/api/compose", "application/json", string(body), admin); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	token := f.invite(t, admin, `{"name": "paul", "email": "paul@example.org", "role": "user"}`)
	paul := sessionCookie(t, f.do(http.MethodPost, "/pi-appliance/api/invitation", "application/json", `{"token": "`+token+`", "password": "huit car"}`, nil))
	if response := f.do(http.MethodGet, "/pi-appliance/api/services/outils/source", "", "", paul); response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "nginx") {
		t.Fatalf("par un utilisateur : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services/outils/source", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	response := f.do(http.MethodGet, "/pi-appliance/api/services/outils/source", "", "", admin)
	var source struct {
		Compose string `json:"compose"`
		URL     string `json:"url"`
		Service string `json:"service"`
		Port    int    `json:"port"`
	}
	json.Unmarshal(response.Body.Bytes(), &source)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || source.Compose != freeCompose || source.URL != "https://github.com/exemple/depot/blob/main/compose.yaml" || source.Service != "web" || source.Port != 80 {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
}
