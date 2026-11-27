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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// Pied de page des pages servies par Go et pages d'erreur de Traefik (TODO-5.21).

func pageFixture(t *testing.T, version, commit, date string) http.Handler {
	t.Helper()
	f := newFixture(t)
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return Handler(Config{Version: version, Commit: commit, Date: date, Catalog: catalog, Services: f.manager, Accounts: f.accounts})
}

func TestReleaseVersion(t *testing.T) {
	for version, release := range map[string]bool{"0.1.4": true, "v0.1.4": true, "0.2.0-beta.1": true, "dev": false, "": false, "2026-11-22-abc1234": false, "abc1234": false} {
		if ReleaseVersion(version) != release {
			t.Errorf("%q : %v attendu", version, release)
		}
	}
}

func TestErrorPages(t *testing.T) {
	handler := pageFixture(t, "0.1.4", "0123456789abcdef0123456789abcdef01234567", "2026-11-22")
	cases := []struct {
		path, remote, language string
		status                 int
		want, absent           []string
	}{
		{"/pi-appliance/erreur/404", "192.168.1.20:5000", "", http.StatusNotFound,
			[]string{"Page introuvable", `lang="fr"`, "pi-appliance 0.1.4", "/commit/0123456789abcdef0123456789abcdef01234567", "<code>0123456</code>", "du 22 novembre 2026", "licence CC BY-NC-ND 4.0", LicenseURL, `href="https://maison.example.org/pi-appliance/"`}, nil},
		{"/pi-appliance/erreur/502", "192.168.1.20:5000", "en-GB,en;q=0.8", http.StatusBadGateway,
			[]string{"Service unavailable", "Try again", "of November 22, 2026", "Public code, CC BY-NC-ND 4.0 license", "Source code"}, nil},
		{"/pi-appliance/erreur/504?lang=ar", "192.168.1.20:5000", "", http.StatusGatewayTimeout,
			[]string{`dir="rtl"`, "الخدمة غير متاحة"}, nil},
		// Depuis internet : la page s'ouvre, sans lien vers la console.
		{"/pi-appliance/erreur/404", "203.0.113.5:5000", "", http.StatusNotFound,
			[]string{"Page introuvable"}, []string{"Ouvrir la console"}},
		{"/pi-appliance/erreur/418", "192.168.1.20:5000", "", http.StatusNotFound,
			[]string{"Page introuvable"}, nil},
	}
	for _, c := range cases {
		request := httptest.NewRequest(http.MethodGet, c.path, nil)
		request.Header.Set("X-Pi-Appliance", "1")
		request.RemoteAddr = c.remote
		if c.language != "" {
			request.Header.Set("Accept-Language", c.language)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		body := response.Body.String()
		if response.Code != c.status || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s : code %d, %s", c.path, response.Code, response.Header().Get("Content-Type"))
		}
		for _, want := range c.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s : %q absent de %s", c.path, want, body)
			}
		}
		for _, absent := range c.absent {
			if strings.Contains(body, absent) {
				t.Errorf("%s : %q présent", c.path, absent)
			}
		}
	}
	// Une demande POST (routeur de dernier recours) a aussi sa page.
	request := httptest.NewRequest(http.MethodPost, "/pi-appliance/erreur/404", nil)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "192.168.1.20:5000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "Page introuvable") {
		t.Fatalf("POST : code %d", response.Code)
	}
}

// Version de développement : pas de version, seulement le commit ; sans commit, licence et code source seuls.
func TestFooterDevelopment(t *testing.T) {
	c := &console{Config: Config{Version: "2026-11-22-0123456", Commit: "0123456789abcdef", Date: "2026-11-22"}}
	footer := c.footerHTML("fr")
	if strings.Contains(footer, "pi-appliance 2026") || !strings.Contains(footer, "<code>0123456</code>") {
		t.Fatalf("pied de page %s", footer)
	}
	c = &console{Config: Config{Version: "dev"}}
	footer = c.footerHTML("de")
	if strings.Contains(footer, "commit") || strings.Contains(footer, "Commit") || !strings.Contains(footer, "Öffentlicher Code, Lizenz CC BY-NC-ND 4.0") || !strings.Contains(footer, "Quellcode") {
		t.Fatalf("pied de page %s", footer)
	}
}

func TestHealthCommit(t *testing.T) {
	handler := pageFixture(t, "dev", "0123456789abcdef", "2026-11-22")
	request := httptest.NewRequest(http.MethodGet, "/pi-appliance/api/health", nil)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "192.168.1.20:5000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var health Health
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil || health != (Health{Status: "ok", Version: "dev", Commit: "0123456789abcdef", Date: "2026-11-22"}) {
		t.Fatalf("%+v %v", health, err)
	}
}

// Étiquettes d'un service hors catalogue : plus de réglage dans sa fiche
// (demande de l'utilisateur du 2026-11-27, TODO-5.28), mais celles choisies
// avant, gardées dans son instance.json, restent lues ; un service du
// catalogue prend celles de son application.
func TestServiceTags(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	dir := filepath.Join(f.manager.Dir, "services", "perso")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(services.Instance{Name: "perso", App: services.CustomApp, AppName: "Docker Compose libre", Address: "perso.maison.example.org", Tags: []string{"maison", "fichiers"}})
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), state, 0o600); err != nil {
		t.Fatal(err)
	}
	if response := f.do(http.MethodPut, "/pi-appliance/api/services/perso/tags", "application/json", `{"tags": ["reseau"]}`, cookie); response.Code == http.StatusOK {
		t.Fatalf("PUT api/services/perso/tags encore accepté : %s", response.Body.String())
	}
	if response := f.install(`{"app": "dokuwiki", "name": "wiki"}`, cookie); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie)
	listed := decode[struct {
		Services []ServiceView `json:"services"`
	}](t, response)
	tags := map[string]string{}
	for _, service := range listed.Services {
		tags[service.Name] = strings.Join(service.Tags, ",")
	}
	if len(tags) != 2 || tags["perso"] != "maison,fichiers" || tags["wiki"] == "" || strings.Contains(response.Body.String(), "tags_editable") {
		t.Fatalf("services %+v", listed.Services)
	}
}

// Adresse nue (routeur pi-appliance-root) : la console, ou la page « service
// indisponible » sur l'adresse d'un service dont le routeur manque.
func TestRootPage(t *testing.T) {
	f := newFixture(t)
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, _ := catalog.Get("site-web")
	if _, err := f.manager.Install(context.Background(), app, "www", nil); err != nil {
		t.Fatal(err)
	}
	for host, want := range map[string]int{"maison.example.org": http.StatusFound, "www.maison.example.org": http.StatusServiceUnavailable, "autre.example.org": http.StatusFound} {
		request := httptest.NewRequest(http.MethodGet, "/pi-appliance/erreur/racine", nil)
		request.Host = host
		request.RemoteAddr = "192.168.1.20:5000"
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		if response.Code != want {
			t.Errorf("%s : code %d, %d attendu", host, response.Code, want)
		}
		if want == http.StatusFound && response.Header().Get("Location") != "/pi-appliance/" {
			t.Errorf("%s : renvoi vers %q", host, response.Header().Get("Location"))
		}
		if want == http.StatusServiceUnavailable && !strings.Contains(response.Body.String(), "Service indisponible") {
			t.Errorf("%s : %s", host, response.Body.String())
		}
	}
}
