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
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// servicesFixture : console dont l'adresse internet est maison.example.org
// (domaine des services), avec services-internet.yml dans un dossier de test.
func servicesFixture(t *testing.T) (fixture, string) {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	dynamic := filepath.Join(dir, "dynamic")
	if err := os.MkdirAll(dynamic, 0o755); err != nil {
		t.Fatal(err)
	}
	handler := Handler(Config{
		Version:              "1.2.3",
		Catalog:              catalog,
		Services:             manager,
		Accounts:             accounts,
		InternetFile:         filepath.Join(dir, "internet-console.json"),
		TraefikInternetFile:  filepath.Join(dynamic, "console-internet.yml"),
		ServicesInternetFile: filepath.Join(dynamic, "services-internet.yml"),
	})
	return fixture{handler: handler, accounts: accounts, manager: manager}, filepath.Join(dynamic, "services-internet.yml")
}

// installedApp déclare un service installé d'une application donnée, avec son fichier Compose.
func installedApp(t *testing.T, f fixture, name, app, access, compose string) {
	t.Helper()
	dir := filepath.Join(f.manager.Dir, "services", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(services.Instance{Name: name, App: app, Address: name + ".maison.example.org", Access: access})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instance.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
}

func internetViews(t *testing.T, f fixture, cookie *http.Cookie) map[string]ServiceView {
	t.Helper()
	response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie)
	list := decode[struct {
		Services []ServiceView `json:"services"`
	}](t, response)
	views := map[string]ServiceView{}
	for _, view := range list.Services {
		views[view.Name] = view
	}
	return views
}

// Adresse internet de chaque service (TODO-3.17) : public toujours, personnes
// connectées seulement console ouverte, maison jamais ; applications qui ne
// marchent pas sous un chemin, ou à mettre à jour, jamais publiées.
func TestServicesOnInternet(t *testing.T) {
	f, file := servicesFixture(t)
	admin := f.admin(t)
	installedApp(t, f, "www", "site-web", services.AccessPublic, "")
	installedApp(t, f, "wiki", "site-web", services.AccessUsers, "")
	installedApp(t, f, "prive", "site-web", services.AccessHome, "")
	installedApp(t, f, "hugo", "hugo", services.AccessPublic, "ancienne version")
	installedApp(t, f, "blog", "hugo", services.AccessPublic, "SITE_PATH: /${PI_INSTANCE}/")
	installedApp(t, f, "cloud", "nextcloud", services.AccessPublic, "")

	want := map[string][2]string{
		"www":   {"https://maison.example.org/www/", ""},
		"blog":  {"https://maison.example.org/blog/", ""},
		"wiki":  {"", "console"},
		"prive": {"", "home"},
		"hugo":  {"", "update"},
		"cloud": {"", "subpath"},
	}
	views := internetViews(t, f, admin)
	for name, expected := range want {
		if got := [2]string{views[name].InternetAddress, views[name].InternetReason}; got != expected {
			t.Errorf("%s : %v, attendu %v", name, got, expected)
		}
	}

	// Ouvrir la console publie aussi les services des personnes connectées.
	if response := f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", "", admin); response.Code != http.StatusOK {
		t.Fatalf("ouverture : %d %s", response.Code, response.Body.String())
	}
	if view := internetViews(t, f, admin)["wiki"]; view.InternetAddress != "https://maison.example.org/wiki/" {
		t.Fatalf("wiki, console ouverte : %+v", view)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, expected := range []string{
		"rule: \"(Host(`maison.example.org`)) && (Path(`/www`) || PathPrefix(`/www/`))\"",
		"middlewares: [pi-www-internet-slash@file, pi-appliance-errors@file, pi-www-internet-strip@file]",
		"middlewares: [pi-wiki-internet-slash@file, pi-appliance-errors@file, pi-appliance-auth@file, pi-wiki-internet-strip@file]",
		"service: pi-www@docker",
		"certResolver: letsencrypt",
		"prefixes: [\"/wiki\"]",
		"regex: '^(https?://[^/]+/www)(\\?.*)?$'",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("services-internet.yml sans %q :\n%s", expected, content)
		}
	}
	for _, absent := range []string{"pi-prive-", "pi-hugo-", "pi-cloud-"} {
		if strings.Contains(content, absent) {
			t.Errorf("services-internet.yml publie %s :\n%s", absent, content)
		}
	}

	// Fermer la console retire les services des personnes connectées.
	if response := f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=false", "", "", admin); response.Code != http.StatusOK {
		t.Fatalf("fermeture : %d", response.Code)
	}
	data, _ = os.ReadFile(file)
	if strings.Contains(string(data), "pi-wiki-") || !strings.Contains(string(data), "pi-www-") {
		t.Fatalf("console fermée :\n%s", data)
	}
}

func TestTraefikServicesEmpty(t *testing.T) {
	published := map[string]servicePublication{"www": {Address: "https://a.example.org/www/", Access: services.AccessPublic}}
	if traefikServices(nil, published) != "" || traefikServices([]string{"a.example.org"}, nil) != "" {
		t.Fatal("configuration écrite sans adresse ou sans service")
	}
	// Un nom réservé n'est jamais publié, même s'il en a l'adresse.
	if content := traefikServices([]string{"a.example.org"}, map[string]servicePublication{"pi-appliance": {Address: "x"}}); content != "" {
		t.Fatalf("nom réservé publié :\n%s", content)
	}
}

// Personnes connectées sous un chemin de l'adresse de la console : connexion
// sur la même adresse, code lié à <adresse>/<nom>, cookie sous /<nom>.
func TestForwardAuthUnderPath(t *testing.T) {
	f, _ := servicesFixture(t)
	admin := f.admin(t)
	installedApp(t, f, "wiki", "site-web", services.AccessUsers, "")
	installedApp(t, f, "notes", "site-web", services.AccessUsers, "")
	installedApp(t, f, "www", "site-web", services.AccessPublic, "")
	if response := f.do(http.MethodPut, "/pi-appliance/api/internet-console?enabled=true", "", "", admin); response.Code != http.StatusOK {
		t.Fatalf("ouverture : %d", response.Code)
	}
	service := map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "maison.example.org", "X-Forwarded-Uri": "/wiki/doku.php?id=start", "X-Forwarded-Method": "GET", "X-Forwarded-For": "203.0.113.9"}
	location, state := loginRedirect(t, f, "/pi-appliance/api/auth/forward", service)
	if location.Host != "maison.example.org" || location.Query().Get("return") != "https://maison.example.org/wiki/doku.php?id=start" || state.Path != "/wiki" {
		t.Fatalf("connexion : %s, cookie %+v", location, state)
	}
	// Un service public sous la même adresse : pas de code.
	body, _ := json.Marshal(ForwardRequest{Return: "https://maison.example.org/www/", State: state.Value})
	if back := returnFrom(f, "maison.example.org", string(body), admin); strings.Contains(decode[map[string]string](t, back)["url"], loginParam) {
		t.Fatalf("service public : code donné : %s", back.Body.String())
	}

	body, _ = json.Marshal(ForwardRequest{Return: location.Query().Get("return"), State: state.Value})
	back := returnFrom(f, "maison.example.org", string(body), admin)
	target, err := url.Parse(decode[map[string]string](t, back)["url"])
	if back.Code != http.StatusOK || err != nil || target.Query().Get(loginParam) == "" {
		t.Fatalf("retour au service : %d %s", back.Code, back.Body.String())
	}
	callback := maps.Clone(service)
	callback["X-Forwarded-Uri"] = target.RequestURI()
	response := forwardTo(f, "/pi-appliance/api/auth/forward", "127.0.0.1:5000", state, callback)
	forward := cookieNamed(response, forwardCookie)
	if response.Code != http.StatusFound || forward == nil || forward.Path != "/wiki" || !forward.HttpOnly || !forward.Secure {
		t.Fatalf("échange du code : %d %v %+v", response.Code, response.Header(), forward)
	}
	if response := forwardTo(f, "/pi-appliance/api/auth/forward", "127.0.0.1:5000", forward, service); response.Code != http.StatusOK || response.Header().Get("Remote-User") != "marie" {
		t.Fatalf("avec session déléguée : %d", response.Code)
	}
	// La session de /wiki ne vaut ni pour /notes ni pour l'adresse entière.
	notes := maps.Clone(service)
	notes["X-Forwarded-Uri"] = "/notes/"
	if response := forwardTo(f, "/pi-appliance/api/auth/forward", "127.0.0.1:5000", forward, notes); response.Code != http.StatusFound {
		t.Fatalf("cookie de /wiki sur /notes : %d", response.Code)
	}
	other := maps.Clone(service)
	other["X-Forwarded-Uri"] = "/cockpit/"
	if response := forwardTo(f, "/pi-appliance/api/auth/forward", "127.0.0.1:5000", forward, other); response.Code != http.StatusFound {
		t.Fatalf("cookie de /wiki sur /cockpit : %d", response.Code)
	}
}

// En-tête X-Pi-Appliance exigé sur ce qui modifie : un formulaire d'une autre
// page (même d'un service sous l'adresse de la console) ne le pose pas.
func TestAPIHeaderRequired(t *testing.T) {
	f := newFixture(t)
	admin := f.admin(t)
	request := httptest.NewRequest(http.MethodPost, "/pi-appliance/api/logout", nil)
	request.RemoteAddr = "192.168.1.20:5000"
	request.AddCookie(admin)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("sans en-tête : %d", recorder.Code)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", admin); response.Code != http.StatusOK || response.Header().Get("X-Frame-Options") != "DENY" || response.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Fatalf("lecture : %d %v", response.Code, response.Header())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/logout", "", "", admin); response.Code != http.StatusNoContent {
		t.Fatalf("avec en-tête : %d", response.Code)
	}
}
