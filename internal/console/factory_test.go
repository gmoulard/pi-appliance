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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/alerts"
	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/cockpit"
	"github.com/gmoulard/pi-appliance/internal/metrics"
	"github.com/gmoulard/pi-appliance/internal/network"
	"github.com/gmoulard/pi-appliance/marketplace"
)

const factoryPath = "/pi-appliance/api/factory-reset"

func TestFactoryResetNeedsAdminPassword(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true}`, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"password": "`+adminPassword+`"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("sans confirmation : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true, "password": "pas le bon"}`, cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("mauvais mot de passe : code %d : %s", response.Code, response.Body.String())
	}
	if f.accounts.SetupNeeded() {
		t.Fatal("comptes effacés malgré un refus")
	}
}

type scriptCalls struct {
	mu    sync.Mutex
	out   map[string]string
	calls []string
}

func (s *scriptCalls) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, strings.Join(args, " "))
	return []byte(s.out[args[0]]), nil
}

func (s *scriptCalls) list() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.calls, ",")
}

// Tout est effacé sauf le Wi-Fi ; le disque de sauvegarde est seulement oublié.
func TestFactoryResetErasesAllButWiFi(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	for _, body := range []string{`{"app": "postgresql", "name": "base"}`, `{"app": "dokuwiki", "name": "wiki"}`} {
		if response := f.install(body, cookie); response.Code != http.StatusCreated {
			t.Fatalf("installation : %d %s", response.Code, response.Body.String())
		}
	}
	dir := t.TempDir()
	disk := &scriptCalls{out: map[string]string{"forget": `{"ok": true}`}}
	backups := &backup.Manager{Dir: dir, Services: f.manager, Disks: &backup.Disks{Runner: disk}}
	if err := backups.SetSettings(backup.DefaultSettings); err != nil {
		t.Fatal(err)
	}
	if err := backups.Protect(adminPassword); err != nil {
		t.Fatal(err)
	}
	net := &scriptCalls{out: map[string]string{"name-default": `{"ok": true, "name": "pi-appliance-3f2a"}`}}
	alertsFile := filepath.Join(dir, "alerts.json")
	monitor := &alerts.Monitor{Path: alertsFile}
	if err := monitor.SetEnabled(alerts.Disk, false); err != nil {
		t.Fatal(err)
	}
	internetFile := filepath.Join(dir, "internet-console.json")
	if err := os.WriteFile(internetFile, []byte(`{"enabled": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cockpitAccess := &cockpit.AccessFile{Path: filepath.Join(dir, "cockpit-access.json")}
	if err := cockpitAccess.Set(cockpit.Access{Internet: true, Users: true}); err != nil {
		t.Fatal(err)
	}
	history := &metrics.History{Path: filepath.Join(dir, "history.json")}
	cpu := 42.0
	if err := history.Record(metrics.Sample{CPUPercent: &cpu}); err != nil {
		t.Fatal(err)
	}
	if err := history.Save(); err != nil {
		t.Fatal(err)
	}
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	f.handler = Handler(Config{Version: "1.2.3", Catalog: catalog, Services: f.manager, Accounts: f.accounts, ACMEEmailFile: f.acmeEmailFile,
		Backups: backups, Network: &network.Client{Runner: net}, Alerts: monitor, InternetFile: internetFile, CockpitAccess: cockpitAccess, History: history})
	response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true, "password": "`+adminPassword+`"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	var result FactoryResetResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Name != "pi-appliance-3f2a" || len(result.Problems) != 0 {
		t.Fatalf("%s %v", response.Body.String(), err)
	}
	if !f.accounts.SetupNeeded() {
		t.Fatal("comptes gardés")
	}
	if instances, _ := f.manager.List(); len(instances) != 0 {
		t.Fatalf("services gardés : %v", instances)
	}
	if got := net.list(); got != "name-default" {
		t.Fatalf("réseau : %s (le Wi-Fi ne doit pas être touché)", got)
	}
	if got := disk.list(); got != "forget" {
		t.Fatalf("disque de sauvegarde : %s (jamais formaté)", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup")); !os.IsNotExist(err) {
		t.Fatalf("réglages et clé des sauvegardes gardés : %v", err)
	}
	if points := history.Points(); len(points) != 0 {
		t.Fatalf("historique des mesures gardé : %v", points)
	}
	for _, file := range []string{internetFile, f.acmeEmailFile, history.Path} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("%s gardé : %v", file, err)
		}
	}
	if access := cockpitAccess.Get(); access.Internet || access.Users {
		t.Fatalf("accès à Cockpit gardé : %+v", access)
	}
	for _, setting := range (&alerts.Monitor{Path: alertsFile}).Settings() {
		if !setting.Enabled {
			t.Fatalf("alerte %s encore coupée", setting.Type)
		}
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/services", "", "", cookie); response.Code != http.StatusUnauthorized {
		t.Fatalf("ancienne session encore ouverte : %d", response.Code)
	}
}

func TestFactoryResetWithResetFile(t *testing.T) {
	f := newFixture(t)
	f.admin(t)
	file := filepath.Join(t.TempDir(), "reset")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.accounts.ConsumeResetFile(file); err != nil {
		t.Fatal(err)
	}
	if response := f.do(http.MethodPost, factoryPath, "application/json", `{"confirm": true}`, nil); response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if !f.accounts.SetupNeeded() || f.accounts.ResetPending() {
		t.Fatal("pas revenu à la création du compte")
	}
}

func TestFactoryResetPage(t *testing.T) {
	response := get(t, "/pi-appliance/reset-to-factory")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "factorySection") {
		t.Fatalf("code %d", response.Code)
	}
}
