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
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/alerts"
	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/mail"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// Alertes par e-mail (spécification 8.6) : réglage par type, service arrêté
// exprès dans la console, destinataires.
func TestAlertsSettings(t *testing.T) {
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	smtp, err := mail.OpenSMTP(filepath.Join(dir, "smtp.json"))
	if err != nil {
		t.Fatal(err)
	}
	monitor := &alerts.Monitor{Path: filepath.Join(dir, "alerts.json")}
	manager := &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}}
	f := fixture{handler: Handler(Config{Version: "1.2.3", Catalog: catalog, Services: manager, Accounts: accounts, SMTP: smtp, Alerts: monitor}), accounts: accounts, manager: manager}
	if code := f.do(http.MethodGet, "/pi-appliance/api/alerts", "", "", nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("sans session : %d", code)
	}
	cookie := f.admin(t)
	status := decode[AlertsStatus](t, f.do(http.MethodGet, "/pi-appliance/api/alerts", "", "", cookie))
	if len(status.Types) != len(alerts.Types) || !status.Types[0].Enabled || status.Mail || status.Recipients != 0 || status.Verified {
		t.Fatalf("état : %+v", status)
	}
	response := f.do(http.MethodPut, "/pi-appliance/api/alerts", "application/json", `{"type": "disk", "enabled": false}`, cookie)
	status = decode[AlertsStatus](t, response)
	if response.Code != http.StatusOK || status.Types[0].Type != alerts.Disk || status.Types[0].Enabled || !status.Types[1].Enabled {
		t.Fatalf("type coupé : %d %+v", response.Code, status)
	}
	if code := f.do(http.MethodPut, "/pi-appliance/api/alerts", "application/json", `{"type": "autre", "enabled": false}`, cookie).Code; code != http.StatusBadRequest {
		t.Fatalf("type inconnu : %d", code)
	}
	// Adresse vérifiée : l'administrateur reçoit les alertes.
	_, code, err := accounts.VerifyEmailCode("marie", "192.168.1.20")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := accounts.ConfirmEmail("marie", code, false); err != nil {
		t.Fatal(err)
	}
	if recipients := AlertRecipients(accounts); len(recipients) != 1 || recipients[0].Email != "marie@example.org" {
		t.Fatalf("destinataires : %+v", recipients)
	}
	if code := f.install(`{"app": "nextcloud"}`, cookie).Code; code != http.StatusCreated {
		t.Fatalf("installation : code %d", code)
	}
	if code := f.do(http.MethodPost, "/pi-appliance/api/services/cloud/stop", "", "", cookie).Code; code != http.StatusOK || !monitor.Stopped("cloud") {
		t.Fatalf("arrêt exprès : %d %v", code, monitor.Stopped("cloud"))
	}
	if code := f.do(http.MethodPost, "/pi-appliance/api/services/cloud/start", "", "", cookie).Code; code != http.StatusOK || monitor.Stopped("cloud") {
		t.Fatalf("redémarrage : %d %v", code, monitor.Stopped("cloud"))
	}
}
