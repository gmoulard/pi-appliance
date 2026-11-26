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
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

type restorePi struct {
	fixture
	dir       string
	backups   *backup.Manager
	restarted chan struct{}
}

func newRestorePi(t *testing.T, disk, host string) restorePi {
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
	backups := &backup.Manager{Dir: dir, Services: manager, Host: host, Target: func() (string, error) { return filepath.Join(disk, host), nil }}
	restarted := make(chan struct{}, 1)
	handler := Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, Backups: backups, Restart: func() { restarted <- struct{}{} }})
	return restorePi{fixture: fixture{handler: handler, accounts: accounts}, dir: dir, backups: backups, restarted: restarted}
}

func TestWholeRestoreFromSetup(t *testing.T) {
	disk := t.TempDir()
	old := newRestorePi(t, disk, "ancien")
	old.admin(t)
	snapshot, err := old.backups.Backup(context.Background(), backup.Manual)
	if err != nil {
		t.Fatal(err)
	}
	if response := old.do(http.MethodGet, "/pi-appliance/api/setup/restore", "", "", nil); response.Code != http.StatusForbidden {
		t.Fatalf("appliance qui a un compte : code %d", response.Code)
	}

	pi := newRestorePi(t, disk, "pi-appliance")
	response := pi.do(http.MethodGet, "/pi-appliance/api/setup/restore", "", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	var status WholeRestoreStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.DiskReady || len(status.Backups) != 1 || status.Backups[0].Host != "ancien" || status.Backups[0].ID != snapshot.ID {
		t.Fatalf("état : %+v", status)
	}

	request := httptest.NewRequest(http.MethodGet, "/pi-appliance/api/setup/restore", nil)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "203.0.113.7:5000"
	recorder := httptest.NewRecorder()
	pi.handler.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatalf("depuis internet : code %d", recorder.Code)
	}

	body := func(password string) string {
		return `{"host":"ancien","id":"` + snapshot.ID + `","password":"` + password + `"}`
	}
	response = pi.do(http.MethodPost, "/pi-appliance/api/setup/restore", "application/json", body("autre chose"), nil)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"password"`) {
		t.Fatalf("mauvais mot de passe : code %d : %s", response.Code, response.Body.String())
	}
	response = pi.do(http.MethodPost, "/pi-appliance/api/setup/restore", "application/json", body(adminPassword), nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("restauration : code %d : %s", response.Code, response.Body.String())
	}
	select {
	case <-pi.restarted:
	case <-time.After(10 * time.Second):
		t.Fatal("la console n'a pas redémarré")
	}
	data, err := os.ReadFile(filepath.Join(pi.dir, "accounts.json"))
	if err != nil || !strings.Contains(string(data), `"marie"`) {
		t.Fatalf("comptes remis : %q, %v", data, err)
	}
	if last := pi.backups.Last(); last == nil || !last.OK || last.Action != "restore" {
		t.Fatalf("résultat : %+v", last)
	}
	response = pi.do(http.MethodPost, "/pi-appliance/api/setup", "application/json", `{"name": "paul", "email": "paul@example.org", "password": "`+adminPassword+`"}`, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("création du compte pendant la restauration : code %d", response.Code)
	}
	reopened, err := auth.Open(filepath.Join(pi.dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.SetupNeeded() {
		t.Fatal("comptes de la sauvegarde absents après le redémarrage")
	}
}

func TestWholeRestoreThrottled(t *testing.T) {
	disk := t.TempDir()
	old := newRestorePi(t, disk, "ancien")
	old.admin(t)
	snapshot, err := old.backups.Backup(context.Background(), backup.Manual)
	if err != nil {
		t.Fatal(err)
	}
	pi := newRestorePi(t, disk, "pi-appliance")
	body := `{"host":"ancien","id":"` + snapshot.ID + `","password":"faux"}`
	for i := 0; i < 5; i++ {
		if response := pi.do(http.MethodPost, "/pi-appliance/api/setup/restore", "application/json", body, nil); response.Code != http.StatusForbidden {
			t.Fatalf("essai %d : code %d", i+1, response.Code)
		}
	}
	if response := pi.do(http.MethodPost, "/pi-appliance/api/setup/restore", "application/json", body, nil); response.Code != http.StatusTooManyRequests {
		t.Fatalf("sixième essai : code %d", response.Code)
	}
}
