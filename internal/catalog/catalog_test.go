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

package catalog

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const essai = `x-pi-appliance:
  name: Essai
  description: Une application d'essai.
  icon: M4 5h16v14H4z
  category: Outils
  tags: [documents]
  license: MIT
  subdomain: essai
  service: app
  port: 80
  memory_mb: 64
  disk_mb: 64
  login: application
  access: users
services:
  app:
    image: nginx:1.30-alpine
`

func builtin(t *testing.T) marketplace.Catalog {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

// publish : un catalogue publié, fait du catalogue intégré et de manifestes
// remplacés ou ajoutés (identifiant → fichier Compose).
func publish(t *testing.T, changes map[string]string) []byte {
	t.Helper()
	apps := builtin(t).Apps()
	document := marketplace.Document{Format: marketplace.DocumentFormat}
	for _, app := range apps {
		compose := string(app.Compose)
		if changed, ok := changes[app.ID]; ok {
			compose = changed
			delete(changes, app.ID)
		}
		document.Apps = append(document.Apps, marketplace.PublishedApp{App: marketplace.App{ID: app.ID, Tags: app.Tags}, Compose: compose})
	}
	for id, compose := range changes {
		document.Apps = append(document.Apps, marketplace.PublishedApp{App: marketplace.App{ID: id}, Compose: compose})
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type github struct {
	document []byte
	name     string
	requests int
}

func (g *github) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.requests++
	if r.Header.Get("User-Agent") != "pi-appliance" {
		http.Error(w, "User-Agent", http.StatusBadRequest)
		return
	}
	name := g.name
	if name == "" {
		name = fmt.Sprintf("pi-appliance-%x.json", md5.Sum(g.document))
	}
	switch r.URL.Path {
	case "/docs/static/pi-appliance-catalog.json":
		fmt.Fprintf(w, `{"file": %q}`, name)
	case "/docs/static/" + name:
		w.Write(g.document)
	default:
		http.NotFound(w, r)
	}
}

func source(t *testing.T, g *github) *Source {
	t.Helper()
	server := httptest.NewTLSServer(g)
	t.Cleanup(server.Close)
	return &Source{Builtin: builtin(t), URL: server.URL + "/docs/static/pi-appliance-catalog.json", CacheFile: filepath.Join(t.TempDir(), "catalog.json"), Version: "1.2.3", Client: server.Client()}
}

func TestRefresh(t *testing.T) {
	wiki, _ := builtin(t).Get("dokuwiki")
	newer := strings.Replace(string(wiki.Compose), "description: ", "description: Nouvelle version. ", 1)
	s := source(t, &github{document: publish(t, map[string]string{"dokuwiki": newer, "essai": essai})})
	if s.Status().Online {
		t.Fatal("catalogue en ligne avant la lecture")
	}
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if app, ok := s.Get("essai"); !ok || app.Name != "Essai" {
		t.Fatalf("nouvelle application absente : %+v", app)
	}
	if app, _ := s.Get("dokuwiki"); !strings.HasPrefix(app.Description, "Nouvelle version.") {
		t.Fatalf("manifeste en ligne non pris : %q", app.Description)
	}
	if status := s.Status(); !status.Online || status.UpdatedAt == nil || status.Error != "" || len(status.Refused) != 0 {
		t.Fatalf("état %+v", status)
	}
	if len(s.Apps()) != len(builtin(t).Apps())+1 {
		t.Fatalf("%d applications", len(s.Apps()))
	}
}

// Le catalogue précédent reste quand la lecture en ligne échoue.
func TestRefreshFailures(t *testing.T) {
	good := publish(t, map[string]string{"essai": essai})
	cases := map[string]struct {
		g    *github
		want string
	}{
		"contenu abîmé":  {&github{document: good, name: "pi-appliance-00000000000000000000000000000000.json"}, "abîmé"},
		"format inconnu": {&github{document: []byte(strings.Replace(string(good), `"format":1`, `"format":2`, 1))}, "format 2"},
		"illisible":      {&github{document: []byte("<html>")}, "illisible"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := source(t, c.g)
			err := s.Refresh(context.Background())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("erreur %v, attendu %q", err, c.want)
			}
			if _, ok := s.Get("essai"); ok || s.Status().Online || !strings.Contains(s.Status().Error, c.want) {
				t.Fatalf("catalogue changé malgré l'erreur : %+v", s.Status())
			}
			if _, ok := s.Get("nextcloud"); !ok {
				t.Fatal("catalogue intégré perdu")
			}
		})
	}
	s := &Source{Builtin: builtin(t), URL: "http://example.org/catalog.json"}
	if err := s.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("adresse HTTP acceptée : %v", err)
	}
}

// Un manifeste en ligne qui ajoute une option dangereuse, ou qu'une console
// plus ancienne ne comprend pas, est refusé : la version intégrée reste.
func TestRefreshRefusesManifests(t *testing.T) {
	wiki, _ := builtin(t).Get("dokuwiki")
	privileged := strings.Replace(string(wiki.Compose), "restart: unless-stopped", "restart: unless-stopped\n    privileged: true", 1)
	if privileged == string(wiki.Compose) {
		t.Fatal("manifeste DokuWiki sans restart: unless-stopped")
	}
	socket := essai + "    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n"
	unknown := strings.Replace(essai, "port: 80", "port: 80\n  tunnel: true", 1)
	vpn, _ := builtin(t).Get("wireguard")
	s := source(t, &github{document: publish(t, map[string]string{"dokuwiki": privileged, "essai": socket, "futur": unknown, "wireguard": string(vpn.Compose)})})
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if app, _ := s.Get("dokuwiki"); strings.Contains(string(app.Compose), "privileged") {
		t.Fatal("conteneur privilégié accepté")
	}
	for _, id := range []string{"essai", "futur"} {
		if _, ok := s.Get(id); ok {
			t.Errorf("%s accepté", id)
		}
	}
	if got := strings.Join(s.Status().Refused, ","); got != "dokuwiki,essai,futur" {
		t.Fatalf("refusés : %s", got)
	}
	if _, ok := s.Get("wireguard"); !ok {
		t.Fatal("WireGuard refusé alors que ses droits sont ceux de la version intégrée")
	}
}

func TestCache(t *testing.T) {
	g := &github{document: publish(t, map[string]string{"essai": essai})}
	s := source(t, g)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := &Source{Builtin: builtin(t), CacheFile: s.CacheFile, Version: "1.2.3"}
	restarted.LoadCache()
	if _, ok := restarted.Get("essai"); !ok || !restarted.Status().Online {
		t.Fatal("copie du catalogue en ligne non reprise")
	}
	upgraded := &Source{Builtin: builtin(t), CacheFile: s.CacheFile, Version: "1.3.0"}
	upgraded.LoadCache()
	if _, ok := upgraded.Get("essai"); ok {
		t.Fatal("copie d'une autre version de la console reprise")
	}
}

// Le catalogue publié du dépôt est accepté en entier par cette console.
func TestPublishedCatalogAccepted(t *testing.T) {
	catalog := builtin(t)
	planned, err := marketplace.BuiltinPlanned(catalog)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(marketplace.Publish(catalog, planned))
	if err != nil {
		t.Fatal(err)
	}
	s := source(t, &github{document: data})
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if refused := s.Status().Refused; len(refused) != 0 {
		t.Fatalf("refusés : %v", refused)
	}
}

// Portainer (accès à Docker) n'est accepté en ligne que par une console qui
// l'a dans son catalogue intégré : une console plus ancienne le refuse.
func TestPortainerNeedsBuiltinVersion(t *testing.T) {
	catalog := builtin(t)
	portainer, ok := catalog.Get("portainer")
	if !ok {
		t.Fatal("Portainer absent du catalogue intégré")
	}
	document := publish(t, map[string]string{"portainer": string(portainer.Compose)})
	var older []marketplace.App
	for _, app := range catalog.Apps() {
		if app.ID != "portainer" {
			older = append(older, app)
		}
	}
	s := source(t, &github{document: document})
	s.Builtin = marketplace.NewCatalog(older)
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("portainer"); ok || strings.Join(s.Status().Refused, ",") != "portainer" {
		t.Fatalf("Portainer accepté par une console qui ne l'a pas : %v", s.Status().Refused)
	}
	s = source(t, &github{document: document})
	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("portainer"); !ok {
		t.Fatalf("Portainer refusé : %v", s.Status().Refused)
	}
}

// Bouton Marketplace : une lecture par minute au plus, et un catalogue
// inchangé n'est pas retéléchargé.
func TestRefreshNow(t *testing.T) {
	g := &github{document: publish(t, map[string]string{"essai": essai})}
	s := source(t, g)
	ctx := context.Background()
	if changed, err := s.RefreshNow(ctx); err != nil || !changed || g.requests != 2 {
		t.Fatalf("première lecture : changed=%v err=%v requêtes=%d", changed, err, g.requests)
	}
	if changed, err := s.RefreshNow(ctx); err != nil || changed || g.requests != 2 {
		t.Fatalf("second clic dans la minute : changed=%v err=%v requêtes=%d", changed, err, g.requests)
	}
	first := *s.Status().UpdatedAt
	s.lastRead = time.Now().Add(-MinRefresh)
	if changed, err := s.RefreshNow(ctx); err != nil || changed || g.requests != 3 {
		t.Fatalf("catalogue inchangé : changed=%v err=%v requêtes=%d (index seul attendu)", changed, err, g.requests)
	}
	if status := s.Status(); !status.UpdatedAt.After(first) || status.Error != "" {
		t.Fatalf("date de lecture non avancée : %+v", status)
	}
	wiki, _ := builtin(t).Get("dokuwiki")
	g.document = publish(t, map[string]string{"dokuwiki": strings.Replace(string(wiki.Compose), "description: ", "description: Nouvelle version. ", 1)})
	s.lastRead = time.Now().Add(-MinRefresh)
	if changed, err := s.RefreshNow(ctx); err != nil || !changed || g.requests != 5 {
		t.Fatalf("catalogue changé : changed=%v err=%v requêtes=%d", changed, err, g.requests)
	}
	if app, _ := s.Get("dokuwiki"); !strings.HasPrefix(app.Description, "Nouvelle version.") {
		t.Fatalf("nouveau manifeste non pris : %q", app.Description)
	}
	if _, ok := s.Get("essai"); ok {
		t.Fatal("application retirée du catalogue en ligne encore là")
	}
}

// Sans adresse en ligne, le bouton ne lit rien.
func TestRefreshNowOffline(t *testing.T) {
	s := &Source{Builtin: builtin(t)}
	if changed, err := s.RefreshNow(context.Background()); changed || err != nil {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
}
