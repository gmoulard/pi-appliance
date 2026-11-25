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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/diagnostic"
	"github.com/gmoulard/pi-appliance/internal/system"
)

type diagnosticRunner struct{}

func (diagnosticRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	if args[0] == "logs" {
		return []byte("journaux masqués\n"), nil
	}
	return []byte(`{"units":[{"id":"traefik","unit":"pi-appliance-traefik.service","state":"failed","restarts":3}],"traefik":{"container":"restarting","restarts":7,"https":false},"errors":[],"failed":[],"undervoltage":{"thisBoot":1,"previousBoot":null}}`), nil
}

func diagnosticFixture(t *testing.T) (fixture, *diagnostic.Window) {
	t.Helper()
	f := newFixture(t)
	dir := t.TempDir()
	window := &diagnostic.Window{File: filepath.Join(dir, "diagnostic"), State: filepath.Join(dir, "diagnostic-until")}
	f.handler = Handler(Config{
		Version:          "1.2.3",
		Services:         f.manager,
		Accounts:         f.accounts,
		ACMEEmailFile:    f.acmeEmailFile,
		Resources:        func() system.Resources { return system.Resources{Power: &system.Power{UndervoltageSinceBoot: true}} },
		Diagnostic:       &diagnostic.Client{Runner: diagnosticRunner{}},
		DiagnosticWindow: window,
	})
	return f, window
}

func TestDiagnosticNeedsAdminOrFile(t *testing.T) {
	f, window := diagnosticFixture(t)
	cookie := f.admin(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/diagnostic", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/diagnostic/logs", "", "", nil); response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "journaux") {
		t.Fatalf("journaux sans session : code %d", response.Code)
	}
	response := f.do(http.MethodGet, "/pi-appliance/api/diagnostic", "", "", cookie)
	report := decode[DiagnosticReport](t, response)
	if response.Code != http.StatusOK || report.Status == nil || report.Traefik.Restarts != 7 || report.ByFile || report.Resources == nil || !report.Resources.Power.UndervoltageSinceBoot {
		t.Fatalf("administrateur : code %d, %+v", response.Code, report)
	}
	logs := f.do(http.MethodGet, "/pi-appliance/api/diagnostic/logs", "", "", cookie)
	if logs.Code != http.StatusOK || logs.Body.String() != "journaux masqués\n" || !strings.HasPrefix(logs.Header().Get("Content-Disposition"), `attachment; filename="pi-appliance-diagnostic-`) {
		t.Fatalf("journaux : code %d, %q, %s", logs.Code, logs.Body.String(), logs.Header().Get("Content-Disposition"))
	}

	if err := os.WriteFile(window.File, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := window.Consume(); !found || err != nil {
		t.Fatal(found, err)
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/diagnostic", "", "", nil)
	report = decode[DiagnosticReport](t, response)
	if response.Code != http.StatusOK || !report.ByFile || report.OpenUntil == "" {
		t.Fatalf("fichier diagnostic : code %d, %+v", response.Code, report)
	}
	if response := f.do(http.MethodDelete, "/pi-appliance/api/diagnostic", "", "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("fermeture : code %d", response.Code)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/diagnostic", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("après fermeture : code %d", response.Code)
	}
}

func TestDiagnosticWithResetFile(t *testing.T) {
	f, _ := diagnosticFixture(t)
	f.admin(t)
	file := filepath.Join(t.TempDir(), "reset")
	os.WriteFile(file, nil, 0o600)
	if _, err := f.accounts.ConsumeResetFile(file); err != nil {
		t.Fatal(err)
	}
	if response := f.do(http.MethodGet, "/pi-appliance/api/diagnostic", "", "", nil); response.Code != http.StatusOK {
		t.Fatalf("fichier reset : code %d", response.Code)
	}
}

func TestDiagnosticPage(t *testing.T) {
	response := get(t, "/pi-appliance/diagnostic")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "diagnosticSection") {
		t.Fatalf("code %d", response.Code)
	}
}

func TestRescueHandler(t *testing.T) {
	f, _ := diagnosticFixture(t)
	rescue := RescueHandler(f.handler)
	serve := func(method, path, remote string, headers ...string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, nil)
		request.Header.Set("X-Pi-Appliance", "1")
		request.RemoteAddr = remote
		for i := 0; i+1 < len(headers); i += 2 {
			request.Header.Set(headers[i], headers[i+1])
		}
		recorder := httptest.NewRecorder()
		rescue.ServeHTTP(recorder, request)
		return recorder
	}
	if response := serve(http.MethodGet, "/pi-appliance/diagnostic", "203.0.113.5:4000"); response.Code != http.StatusForbidden {
		t.Fatalf("depuis internet : code %d", response.Code)
	}
	for _, path := range []string{"/pi-appliance/diagnostic", "/pi-appliance/app.js", "/pi-appliance/i18n/ja.js", "/pi-appliance/api/session"} {
		if response := serve(http.MethodGet, path, "192.168.1.20:4000"); response.Code != http.StatusOK {
			t.Fatalf("%s : code %d", path, response.Code)
		}
	}
	if response := serve(http.MethodGet, "/pi-appliance/api/diagnostic", "[fe80::1]:4000"); response.Code != http.StatusUnauthorized {
		t.Fatalf("diagnostic sans session : code %d", response.Code)
	}
	if response := serve(http.MethodGet, "/", "10.42.0.12:4000"); response.Code != http.StatusFound || response.Header().Get("Location") != "/pi-appliance/diagnostic" {
		t.Fatalf("adresse nue : code %d, %s", response.Code, response.Header().Get("Location"))
	}
	for _, path := range []string{"/pi-appliance/api/services", "/pi-appliance/api/network"} {
		if response := serve(http.MethodGet, path, "192.168.1.20:4000"); response.Code != http.StatusNotFound {
			t.Fatalf("%s : code %d", path, response.Code)
		}
	}
	if response := serve(http.MethodPost, "/pi-appliance/api/setup", "192.168.1.20:4000"); response.Code != http.StatusNotFound {
		t.Fatalf("création du compte par le secours : code %d", response.Code)
	}
	var forwarded string
	recorder := RescueHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Get("X-Forwarded-For") + r.Header.Get("X-Forwarded-Proto")
	}))
	request := httptest.NewRequest(http.MethodPost, "/pi-appliance/api/login", nil)
	request.Header.Set("X-Pi-Appliance", "1")
	request.RemoteAddr = "127.0.0.1:4000"
	request.Header.Set("X-Forwarded-For", "192.168.1.99")
	request.Header.Set("X-Forwarded-Proto", "https")
	recorder.ServeHTTP(httptest.NewRecorder(), request)
	if forwarded != "" {
		t.Fatalf("en-têtes du client transmis à la console : %q", forwarded)
	}
}
