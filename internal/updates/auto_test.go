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

package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// autoChecker : la version v1.1.0 publiée, v1.0.0 en service, réglages dans
// un fichier, et l'horloge du Pi à l'heure donnée.
func autoChecker(t *testing.T, status string, hour int) (*Checker, *fakeScript) {
	t.Helper()
	release, client := publishedRelease(t, "")
	checker, script := applianceChecker(t, release, client, status)
	checker.SettingsFile = filepath.Join(t.TempDir(), "updates.json")
	checker.Now = func() time.Time { return time.Date(2026, 11, 21, hour, 15, 0, 0, time.Local) }
	return checker, script
}

func applianceStarted(script *fakeScript) bool {
	script.mu.Lock()
	defer script.mu.Unlock()
	for _, call := range script.calls {
		if strings.HasPrefix(call, "appliance ") {
			return true
		}
	}
	return false
}

func TestAutoOffByDefault(t *testing.T) {
	checker, script := autoChecker(t, twoPackages, DefaultHour)
	if err := checker.Load(); err != nil {
		t.Fatal(err)
	}
	if auto := checker.Status(context.Background()).Auto; auto.Enabled || auto.Hour != DefaultHour {
		t.Fatalf("réglage par défaut : %+v", auto)
	}
	if version := checker.AutoInstall(context.Background()); version != "" || applianceStarted(script) {
		t.Fatalf("installé sans l'accord de l'administrateur : %q %v", version, script.calls)
	}
}

func TestAutoInstallsAtItsHour(t *testing.T) {
	checker, script := autoChecker(t, twoPackages, 2)
	if err := checker.SetAuto(Auto{Enabled: true, Hour: 2}); err != nil {
		t.Fatal(err)
	}
	if version := checker.AutoInstall(context.Background()); version != "v1.1.0" {
		t.Fatalf("version %q : %v", version, script.calls)
	}
	waitDownload(t, checker)
	if !applianceStarted(script) {
		t.Fatalf("updates.sh appliance non lancé : %v", script.calls)
	}
	// Une seule tentative par nuit.
	if version := checker.AutoInstall(context.Background()); version != "" {
		t.Fatalf("seconde tentative la même nuit : %q", version)
	}
}

func TestAutoWaitsForItsHour(t *testing.T) {
	checker, script := autoChecker(t, twoPackages, 14)
	if err := checker.SetAuto(Auto{Enabled: true, Hour: 2}); err != nil {
		t.Fatal(err)
	}
	if version := checker.AutoInstall(context.Background()); version != "" || applianceStarted(script) {
		t.Fatalf("installé hors de l'heure choisie : %q", version)
	}
}

func TestAutoSkipsFailedVersion(t *testing.T) {
	failed := `{"packages": [], "apply": null, "upgrade": {"state": "failed", "version": "v1.1.0", "rolled_back": true}, "reboot_required": false}`
	checker, script := autoChecker(t, failed, DefaultHour)
	if err := checker.SetAuto(Auto{Enabled: true, Hour: DefaultHour}); err != nil {
		t.Fatal(err)
	}
	if version := checker.AutoInstall(context.Background()); version != "" || applianceStarted(script) {
		t.Fatalf("version ratée réessayée toute seule : %q", version)
	}
}

func TestAutoSavedWithChannel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	checker := &Checker{SettingsFile: path}
	if err := checker.SetAuto(Auto{Enabled: true, Hour: 0}); err != nil {
		t.Fatal(err)
	}
	if err := checker.SetChannel(Beta); err != nil {
		t.Fatal(err)
	}
	again := &Checker{SettingsFile: path}
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if again.Channel() != Beta || again.Auto() != (Auto{Enabled: true, Hour: 0}) {
		t.Fatalf("relu : %s %+v", again.Channel(), again.Auto())
	}
	if err := again.SetAuto(Auto{Enabled: true, Hour: 24}); err == nil {
		t.Fatal("heure 24 acceptée")
	}
	if err := (&Checker{}).SetAuto(Auto{Enabled: true}); err == nil {
		t.Fatal("réglage enregistré sans fichier de réglages")
	}
	// Fichier d'une version précédente : canal seul, installation arrêtée.
	if err := os.WriteFile(path, []byte(`{"channel": "stable"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := again.Load(); err != nil || again.Auto() != (Auto{Hour: DefaultHour}) {
		t.Fatalf("ancien fichier : %+v, %v", again.Auto(), err)
	}
}

func TestAutoRoute(t *testing.T) {
	checker := &Checker{SettingsFile: filepath.Join(t.TempDir(), "updates.json")}
	mux := http.NewServeMux()
	checker.Routes(mux, "/api/updates", func(h http.HandlerFunc) http.HandlerFunc { return h })
	for body, code := range map[string]int{
		`{"enabled": true, "hour": 5}`:  http.StatusOK,
		`{"enabled": true, "hour": 30}`: http.StatusBadRequest,
		`{"hour": 5}`:                   http.StatusBadRequest,
		`pas du JSON`:                   http.StatusBadRequest,
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/updates/auto", strings.NewReader(body)))
		if response.Code != code {
			t.Errorf("%s : code %d, attendu %d", body, response.Code, code)
		}
	}
	if auto := checker.Auto(); auto != (Auto{Enabled: true, Hour: 5}) {
		t.Fatalf("réglage : %+v", auto)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/updates/auto", strings.NewReader(`{"enabled": false}`)))
	if auto := checker.Auto(); response.Code != http.StatusOK || auto != (Auto{Hour: 5}) {
		t.Fatalf("arrêt : code %d, %+v", response.Code, auto)
	}
}

// systemChecker : updates.sh avec les réponses données, réglages dans un
// fichier, horloge du Pi à l'heure donnée.
func systemChecker(t *testing.T, status string, hour int) (*Checker, *fakeScript) {
	t.Helper()
	script := &fakeScript{replies: map[string]string{"status": status, "refresh": `{"ok": true}`, "apply": `{"ok": true, "started": true}`}}
	checker := &Checker{Runner: script, Version: "v1.0.0", SettingsFile: filepath.Join(t.TempDir(), "updates.json")}
	checker.Now = func() time.Time { return time.Date(2026, 11, 21, hour, 15, 0, 0, time.Local) }
	return checker, script
}

func TestAutoSystemOffByDefault(t *testing.T) {
	checker, script := systemChecker(t, twoPackages, DefaultSystemHour)
	if auto := checker.Status(context.Background()).AutoSystem; auto.Enabled || auto.Hour != DefaultSystemHour {
		t.Fatalf("réglage par défaut : %+v", auto)
	}
	if checker.AutoApplySystem(context.Background()) || script.called("apply") {
		t.Fatalf("installé sans l'accord de l'administrateur : %v", script.calls)
	}
}

func TestAutoSystemAppliesAtItsHour(t *testing.T) {
	checker, script := systemChecker(t, twoPackages, 1)
	if err := checker.SetAutoSystem(Auto{Enabled: true, Hour: 1}); err != nil {
		t.Fatal(err)
	}
	if !checker.AutoApplySystem(context.Background()) || !script.called("refresh") || !script.called("apply") {
		t.Fatalf("mises à jour non installées : %v", script.calls)
	}
	if checker.AutoApplySystem(context.Background()) {
		t.Fatal("seconde tentative la même nuit")
	}
}

func TestAutoSystemSkips(t *testing.T) {
	upToDate := `{"packages": [], "apply": null, "reboot_required": false}`
	upgrading := `{"packages": [{"name": "libssl3t64", "security": true}], "apply": null, "upgrade": {"state": "running", "version": "v1.1.0"}, "reboot_required": false}`
	for name, c := range map[string]struct {
		status string
		hour   int
	}{
		"hors de l'heure":       {twoPackages, 14},
		"rien à installer":      {upToDate, 5},
		"pi-appliance installé": {upgrading, 5},
	} {
		t.Run(name, func(t *testing.T) {
			checker, script := systemChecker(t, c.status, c.hour)
			if err := checker.SetAutoSystem(Auto{Enabled: true, Hour: 5}); err != nil {
				t.Fatal(err)
			}
			if checker.AutoApplySystem(context.Background()) || script.called("apply") {
				t.Fatalf("installation lancée : %v", script.calls)
			}
		})
	}
}

// Les deux réglages sont indépendants : changer l'un ne touche pas l'autre.
func TestAutoSystemIndependent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	checker := &Checker{SettingsFile: path, Runner: &fakeScript{}}
	if err := checker.SetAutoSystem(Auto{Enabled: true, Hour: 2}); err != nil {
		t.Fatal(err)
	}
	if auto := checker.Auto(); auto != (Auto{Hour: DefaultHour}) {
		t.Fatalf("pi-appliance changé : %+v", auto)
	}
	if err := checker.SetAuto(Auto{Enabled: true, Hour: 23}); err != nil {
		t.Fatal(err)
	}
	again := &Checker{SettingsFile: path, Runner: &fakeScript{}}
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if again.Auto() != (Auto{Enabled: true, Hour: 23}) || again.AutoSystem() != (Auto{Enabled: true, Hour: 2}) {
		t.Fatalf("relu : %+v %+v", again.Auto(), again.AutoSystem())
	}
	if err := (&Checker{SettingsFile: path}).SetAutoSystem(Auto{Enabled: true}); err == nil {
		t.Fatal("système réglé sans script des mises à jour")
	}
	mux := http.NewServeMux()
	again.Routes(mux, "/api/updates", func(h http.HandlerFunc) http.HandlerFunc { return h })
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/updates/auto-system", strings.NewReader(`{"enabled": false}`)))
	if response.Code != http.StatusOK || again.AutoSystem() != (Auto{Hour: 2}) || again.Auto() != (Auto{Enabled: true, Hour: 23}) {
		t.Fatalf("code %d : %+v %+v", response.Code, again.AutoSystem(), again.Auto())
	}
}
