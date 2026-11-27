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
	"slices"
	"testing"
	"time"

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

type alertSender struct{ sent chan string }

func (s alertSender) Configured() bool { return true }

func (s alertSender) SendAlert(to, name, lang, subject, body string) error {
	s.sent <- to + " " + subject
	return nil
}

func (s alertSender) expect(t *testing.T, want ...string) {
	t.Helper()
	var got []string
	for range want {
		select {
		case mail := <-s.sent:
			got = append(got, mail)
		case <-time.After(5 * time.Second):
			t.Fatalf("reçus %v, attendus %v", got, want)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("reçus %v, attendus %v", got, want)
	}
	select {
	case mail := <-s.sent:
		t.Fatalf("e-mail en trop : %s", mail)
	case <-time.After(50 * time.Millisecond):
	}
}

// Compte créé ou supprimé, mot de passe changé (spécification 8.6) : aux
// autres administrateurs et à la personne concernée, pas à l'auteur.
func TestAccountAlerts(t *testing.T) {
	f := newFixture(t)
	sender := alertSender{sent: make(chan string, 10)}
	monitor := &alerts.Monitor{Path: filepath.Join(t.TempDir(), "alerts.json"), Sender: sender, Recipients: func() []alerts.Recipient {
		return []alerts.Recipient{{Name: "marie", Email: "marie@example.org"}, {Name: "paul", Email: "paul@example.org"}}
	}}
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	f.handler = Handler(Config{Version: "1.2.3", Catalog: catalog, Services: f.manager, Accounts: f.accounts, Alerts: monitor})
	cookie := f.admin(t)
	response := f.do(http.MethodPost, "/pi-appliance/api/accounts", "application/json", `{"name": "lea", "email": "lea@example.org", "role": "user"}`, cookie)
	if response.Code != http.StatusCreated {
		t.Fatalf("création : %d %s", response.Code, response.Body.String())
	}
	sender.expect(t, "paul@example.org pi-appliance: account created", "lea@example.org pi-appliance: account created")
	if code := f.do(http.MethodDelete, "/pi-appliance/api/accounts/lea", "", "", cookie).Code; code != http.StatusNoContent {
		t.Fatalf("suppression : %d", code)
	}
	sender.expect(t, "paul@example.org pi-appliance: account deleted", "lea@example.org pi-appliance: account deleted")
	next := "un autre mot de passe assez long"
	response = f.do(http.MethodPost, "/pi-appliance/api/account/password", "application/json", `{"current":"`+adminPassword+`","password":"`+next+`"}`, cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("mot de passe : %d %s", response.Code, response.Body.String())
	}
	sender.expect(t, "paul@example.org pi-appliance: password changed", "marie@example.org pi-appliance: password changed")
}
