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
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

func etherpad(t *testing.T) marketplace.App {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get(EtherpadApp)
	if !ok {
		t.Fatal("Etherpad absent du catalogue")
	}
	return app
}

// padOverride : middleware et REQUIRE_AUTHENTICATION posés pour le service « pad ».
func padOverride(t *testing.T, m *Manager) (string, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.Dir, "services", "pad", overrideName))
	if err != nil {
		t.Fatal(err)
	}
	var generated struct {
		Services map[string]struct {
			Labels      map[string]string `json:"labels"`
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &generated); err != nil {
		t.Fatal(err)
	}
	service := generated.Services["pad"]
	return service.Labels["traefik.http.routers.pi-pad.middlewares"], service.Environment["REQUIRE_AUTHENTICATION"]
}

func TestEtherpadAccess(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), etherpad(t), "pad", nil); err != nil {
		t.Fatal(err)
	}
	if middleware, require := padOverride(t, m); middleware != padMiddleware || require != "true" {
		t.Fatalf("utilisateurs connectés : %q %q", middleware, require)
	}
	name, authorization, ok := m.EtherpadLogin("PAD.maison.example.org.")
	env, _ := readEnv(filepath.Join(m.Dir, "services", "pad", envName))
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:"+env[etherpadUserParam]))
	if !ok || name != "pad" || env[etherpadUserParam] == "" || authorization != want {
		t.Fatalf("connexion d'Etherpad : %q %q %v", name, authorization, ok)
	}
	if _, err := m.ShareReady("pad"); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"autre.maison.example.org", "pad.ailleurs.example.org", "", "maison.example.org"} {
		if _, _, ok := m.EtherpadLogin(host); ok {
			t.Fatalf("%q reconnu comme Etherpad", host)
		}
	}
	if _, err := m.SetAccess(context.Background(), "pad", AccessPublic); err != nil {
		t.Fatal(err)
	}
	if middleware, require := padOverride(t, m); middleware != "" || require != "false" {
		t.Fatalf("public : %q %q", middleware, require)
	}
	if _, _, ok := m.EtherpadLogin("pad.maison.example.org"); ok {
		t.Fatal("Etherpad public : pas de connexion par la console")
	}
	if _, err := m.ShareReady("pad"); err == nil {
		t.Fatal("liens de partage permis pour un Etherpad public")
	}
	if _, err := m.SetAccess(context.Background(), "pad", AccessHome); err != nil {
		t.Fatal(err)
	}
	if middleware, require := padOverride(t, m); middleware != homeMiddleware || require != "false" {
		t.Fatalf("maison : %q %q", middleware, require)
	}
	if _, err := m.SetAccess(context.Background(), "pad", AccessUsers); err != nil {
		t.Fatal(err)
	}
	if middleware, require := padOverride(t, m); middleware != padMiddleware || require != "true" {
		t.Fatalf("retour aux utilisateurs connectés : %q %q", middleware, require)
	}
}

// Un Etherpad installé avant les liens de partage n'a pas le mot de passe de
// son compte interne : il garde l'authentification déléguée habituelle.
func TestEtherpadWithoutUserPassword(t *testing.T) {
	m := manager(t, &fakeRunner{})
	app := etherpad(t)
	app.Access = AccessPublic
	if _, err := m.Install(context.Background(), app, "pad", nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.Dir, "services", "pad", envName)
	data, _ := os.ReadFile(path)
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, etherpadUserParam+"=") {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetAccess(context.Background(), "pad", AccessUsers); err != nil {
		t.Fatal(err)
	}
	if middleware, require := padOverride(t, m); middleware != authMiddleware || require == "true" {
		t.Fatalf("ancien Etherpad : %q %q", middleware, require)
	}
	if _, err := m.ShareReady("pad"); err == nil || !strings.Contains(err.Error(), "à jour") {
		t.Fatalf("liens de partage sans mise à jour : %v", err)
	}
}

func TestShareReadyOnlyEtherpad(t *testing.T) {
	m := manager(t, &fakeRunner{})
	app := nextcloud(t)
	app.Access = AccessUsers
	if _, err := m.Install(context.Background(), app, "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ShareReady("cloud"); err == nil {
		t.Fatal("liens de partage pour Nextcloud")
	}
	if _, err := m.ShareReady("absent"); err == nil {
		t.Fatal("liens de partage pour un service absent")
	}
}
