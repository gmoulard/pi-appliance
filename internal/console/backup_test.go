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
	"os"
	"path/filepath"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

func init() {
	backup.Iterations = 1000
}

func newBackupFixture(t *testing.T) (fixture, string) {
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
	root := filepath.Join(t.TempDir(), "disque")
	backups := &backup.Manager{Dir: dir, Services: manager, Target: func() (string, error) { return root, nil }}
	return fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, Backups: backups}), accounts: accounts}, dir
}

func TestBackupsAdminOnly(t *testing.T) {
	f, _ := newBackupFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/backups", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/backups", "application/json", "{}", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sauvegarde sans session : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/backups/20261120-030000/restore", "application/json", `{"service":"cloud"}`, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("restauration sans session : code %d", response.Code)
	}
}

// La création du compte administrateur protège la clé des sauvegardes par son mot de passe.
func TestBackupsKeyProtectedAtSetup(t *testing.T) {
	f, dir := newBackupFixture(t)
	cookie := f.admin(t)
	if _, err := os.Stat(filepath.Join(dir, "backup", "envelope.json")); err != nil {
		t.Fatalf("enveloppe de la clé : %v", err)
	}
	response := f.do(http.MethodGet, "/pi-appliance/api/backups", "", "", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	var status backup.Status
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Ready || !status.Destination || status.Settings != backup.DefaultSettings || len(status.Snapshots) != 0 {
		t.Fatalf("état : %+v", status)
	}
	response = f.do(http.MethodPut, "/pi-appliance/api/backups/settings", "application/json", `{"enabled":true,"hour":25,"daily":7}`, cookie)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("heure invalide : code %d", response.Code)
	}
	response = f.do(http.MethodPost, "/pi-appliance/api/backups/20261120-030000/restore", "application/json", `{"service":"cloud"}`, cookie)
	if response.Code != http.StatusNotFound {
		t.Fatalf("sauvegarde inconnue : code %d : %s", response.Code, response.Body.String())
	}
}
