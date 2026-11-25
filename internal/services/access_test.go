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

package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// middlewareLabel lit le middleware posé sur le routeur du service « cloud ».
func middlewareLabel(t *testing.T, m *Manager) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.Dir, "services", "cloud", overrideName))
	if err != nil {
		t.Fatal(err)
	}
	var generated struct {
		Services map[string]struct {
			Labels map[string]string `json:"labels"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &generated); err != nil {
		t.Fatal(err)
	}
	return accessPart(t, generated.Services["app"].Labels["traefik.http.routers.pi-cloud.middlewares"])
}

// accessPart : middleware d'accès d'un routeur, après la page d'erreur toujours posée en premier.
func accessPart(t *testing.T, value string) (string, bool) {
	t.Helper()
	rest, found := strings.CutPrefix(value, errorsMiddleware)
	if !found {
		t.Fatalf("middlewares %q : %s attendu en premier", value, errorsMiddleware)
	}
	rest = strings.TrimPrefix(rest, ",")
	return rest, rest != ""
}

func listedAccess(t *testing.T, m *Manager) string {
	t.Helper()
	instances, err := m.List()
	if err != nil || len(instances) != 1 {
		t.Fatalf("liste %v %v", instances, err)
	}
	return instances[0].Access
}

func TestInstallAccess(t *testing.T) {
	cases := map[string]string{AccessPublic: "", AccessUsers: authMiddleware, AccessHome: homeMiddleware}
	for access, middleware := range cases {
		t.Run(access, func(t *testing.T) {
			m := manager(t, &fakeRunner{})
			app := nextcloud(t)
			app.Access = access
			result, err := m.Install(context.Background(), app, "cloud", nil)
			if err != nil || result.Instance.Access != access {
				t.Fatalf("installation : %v %+v", err, result.Instance)
			}
			if got, ok := middlewareLabel(t, m); got != middleware || ok != (middleware != "") {
				t.Fatalf("middleware %q (%v), attendu %q", got, ok, middleware)
			}
			if got := listedAccess(t, m); got != access {
				t.Fatalf("accès listé %q", got)
			}
		})
	}
}

func TestSetAccess(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	instance, err := m.SetAccess(context.Background(), "cloud", AccessHome)
	if err != nil || instance.Access != AccessHome {
		t.Fatalf("maison : %v %+v", err, instance)
	}
	if got, _ := middlewareLabel(t, m); got != homeMiddleware {
		t.Fatalf("middleware %q", got)
	}
	if !runner.called("up --detach --wait") || listedAccess(t, m) != AccessHome {
		t.Fatalf("service non redémarré ou accès non gardé : %v", runner.calls)
	}
	if _, err := m.SetAccess(context.Background(), "cloud", AccessUsers); err != nil {
		t.Fatal(err)
	}
	if got, _ := middlewareLabel(t, m); got != authMiddleware {
		t.Fatalf("middleware %q", got)
	}
	if _, err := m.SetAccess(context.Background(), "cloud", AccessPublic); err != nil {
		t.Fatal(err)
	}
	if got, ok := middlewareLabel(t, m); ok {
		t.Fatalf("middleware %q resté pour un service public", got)
	}
	for _, c := range []struct{ name, access string }{{"cloud", "tous"}, {"autre", AccessHome}, {"../x", AccessHome}} {
		if _, err := m.SetAccess(context.Background(), c.name, c.access); err == nil {
			t.Fatalf("%s %s accepté", c.name, c.access)
		}
	}
}

func TestSetAccessRollsBack(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(m.Dir, "services", "cloud", overrideName))
	runner.fail = map[string]bool{"up --detach": true}
	if _, err := m.SetAccess(context.Background(), "cloud", AccessHome); err == nil || !strings.Contains(err.Error(), "ni avec l'ancien réglage") {
		t.Fatalf("erreur %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(m.Dir, "services", "cloud", overrideName))
	if string(before) != string(after) || listedAccess(t, m) != AccessPublic {
		t.Fatal("ancien réglage non remis")
	}
}

func TestUpdateKeepsAccess(t *testing.T) {
	runner := &updateRunner{}
	m := installed(t, runner)
	if _, err := m.SetAccess(context.Background(), "cloud", AccessUsers); err != nil {
		t.Fatal(err)
	}
	app := nextcloud(t)
	app.Compose = newVersion(t, m)
	if _, err := m.Update(context.Background(), app, "cloud"); err != nil {
		t.Fatal(err)
	}
	if got, _ := middlewareLabel(t, m); got != authMiddleware || listedAccess(t, m) != AccessUsers {
		t.Fatalf("accès perdu à la mise à jour : %q", got)
	}
}

func TestNoWebPageHasNoAccess(t *testing.T) {
	app := nextcloud(t)
	app.Port = 0
	if got := webAccess(app); got != "" {
		t.Fatalf("accès %q pour un service sans page web", got)
	}
	app.Port, app.Access = 80, ""
	if got := webAccess(app); got != AccessPublic {
		t.Fatalf("accès par défaut %q", got)
	}
}

// Seuls Portainer (accès à Docker) et Claude Code (conteneur privilégié,
// pid: host), réservés aux administrateurs à la maison, ont tous les droits
// sur le Pi parmi les applications livrées ; les caméras sont confirmées
// (camera_test.go).
func TestMarketplaceRisks(t *testing.T) {
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range catalog.Apps() {
		risks, err := MarketplaceRisks(app)
		if err != nil {
			t.Fatalf("%s : %v", app.ID, err)
		}
		if (len(risks) > 0) != (app.ID == "portainer" || app.ID == "claude-code" || app.ID == CameraApp) {
			t.Errorf("%s : options dangereuses %v", app.ID, risks)
		}
	}
	claude, _ := catalog.Get("claude-code")
	risks, _ := MarketplaceRisks(claude)
	codes := []string{}
	for _, risk := range risks {
		codes = append(codes, risk.Code)
	}
	if strings.Join(codes, ",") != "privileged,host-pid" {
		t.Errorf("Claude Code : options dangereuses %v", codes)
	}
	claude.Access = AccessHome
	if _, err := MarketplaceRisks(claude); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Claude Code sans accès admins : %v", err)
	}
	portainer, _ := catalog.Get("portainer")
	portainer.Access = AccessHome
	if _, err := MarketplaceRisks(portainer); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Portainer sans accès admins : %v", err)
	}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: &fakeRunner{}}
	if _, err := m.Install(context.Background(), portainer, "portainer", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("installation de Portainer sans accès admins : %v", err)
	}
}
