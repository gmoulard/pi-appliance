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

// at règle l'horloge du Pi sur l'heure donnée, le 21 novembre 2026.
func at(checker *Checker, hour int) {
	checker.Now = func() time.Time { return time.Date(2026, 11, 21, hour, 15, 0, 0, time.Local) }
}

// autoChecker : la version v1.1.0 publiée, v1.0.0 en service, réglages dans
// un fichier, l'horloge du Pi à l'heure donnée.
func autoChecker(t *testing.T, status string, hour int) (*Checker, *fakeScript) {
	t.Helper()
	release, client := publishedRelease(t, "")
	checker, script := applianceChecker(t, release, client, status)
	script.replies["apply"] = `{"ok": true, "started": true}`
	checker.SettingsFile = filepath.Join(t.TempDir(), "updates.json")
	at(checker, hour)
	return checker, script
}

// systemChecker : updates.sh seul, sans version publiée.
func systemChecker(t *testing.T, status string, hour int) (*Checker, *fakeScript) {
	t.Helper()
	script := &fakeScript{replies: map[string]string{"status": status, "refresh": `{"ok": true}`, "apply": `{"ok": true, "started": true}`}}
	checker := &Checker{Runner: script, Version: "v1.0.0", SettingsFile: filepath.Join(t.TempDir(), "updates.json")}
	at(checker, hour)
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
	if auto := checker.Status(context.Background()).Auto; auto != (Auto{Hour: DefaultHour}) {
		t.Fatalf("réglage par défaut : %+v", auto)
	}
	if checker.AutoInstall(context.Background()) != "" || checker.AutoApplySystem(context.Background()) || applianceStarted(script) || script.called("apply") {
		t.Fatalf("installé sans l'accord de l'administrateur : %v", script.calls)
	}
}

func TestAutoInstallsAtItsHour(t *testing.T) {
	checker, script := autoChecker(t, twoPackages, 2)
	if err := checker.SetAuto(Auto{Appliance: true, Hour: 2}); err != nil {
		t.Fatal(err)
	}
	if version := checker.AutoInstall(context.Background()); version != "v1.1.0" {
		t.Fatalf("version %q : %v", version, script.calls)
	}
	waitDownload(t, checker)
	if !applianceStarted(script) {
		t.Fatalf("updates.sh appliance non lancé : %v", script.calls)
	}
	if checker.AutoInstall(context.Background()) != "" {
		t.Fatal("seconde tentative la même nuit")
	}
	// Le système, non coché, ne bouge pas.
	if checker.AutoApplySystem(context.Background()) || script.called("apply") {
		t.Fatalf("système installé sans être coché : %v", script.calls)
	}
}

func TestAutoWaitsForItsHour(t *testing.T) {
	checker, script := autoChecker(t, twoPackages, 14)
	if err := checker.SetAuto(Auto{Appliance: true, System: true, Hour: 2}); err != nil {
		t.Fatal(err)
	}
	if checker.AutoInstall(context.Background()) != "" || checker.AutoApplySystem(context.Background()) || applianceStarted(script) || script.called("apply") {
		t.Fatalf("installé hors de l'heure choisie : %v", script.calls)
	}
}

func TestAutoSkipsFailedVersion(t *testing.T) {
	failed := `{"packages": [], "apply": null, "upgrade": {"state": "failed", "version": "v1.1.0", "rolled_back": true}, "reboot_required": false}`
	checker, script := autoChecker(t, failed, DefaultHour)
	if err := checker.SetAuto(Auto{Appliance: true, Hour: DefaultHour}); err != nil {
		t.Fatal(err)
	}
	if version := checker.AutoInstall(context.Background()); version != "" || applianceStarted(script) {
		t.Fatalf("version ratée réessayée toute seule : %q", version)
	}
}

func TestAutoSystemAppliesAtItsHour(t *testing.T) {
	checker, script := systemChecker(t, twoPackages, 1)
	if err := checker.SetAuto(Auto{System: true, Hour: 1}); err != nil {
		t.Fatal(err)
	}
	if !checker.AutoApplySystem(context.Background()) || !script.called("refresh") || !script.called("apply") {
		t.Fatalf("mises à jour non installées : %v", script.calls)
	}
	if checker.AutoApplySystem(context.Background()) {
		t.Fatal("seconde tentative la même nuit")
	}
}

func TestAutoSystemNothingToDo(t *testing.T) {
	checker, script := systemChecker(t, `{"packages": [], "apply": null, "reboot_required": false}`, 5)
	if err := checker.SetAuto(Auto{System: true, Hour: 5}); err != nil {
		t.Fatal(err)
	}
	if checker.AutoApplySystem(context.Background()) || script.called("apply") {
		t.Fatalf("installation lancée : %v", script.calls)
	}
}

// Même heure pour les deux : le système attend la fin de l'installation de
// pi-appliance et réessaie au regard suivant, la même nuit.
func TestAutoSystemWaitsForAppliance(t *testing.T) {
	upgrading := `{"packages": [{"name": "libssl3t64", "security": true}], "apply": null, "upgrade": {"state": "running", "version": "v1.1.0"}, "reboot_required": false}`
	checker, script := systemChecker(t, upgrading, 5)
	if err := checker.SetAuto(Auto{Appliance: true, System: true, Hour: 5}); err != nil {
		t.Fatal(err)
	}
	if checker.AutoApplySystem(context.Background()) || script.called("apply") {
		t.Fatalf("installation lancée pendant celle de pi-appliance : %v", script.calls)
	}
	script.mu.Lock()
	script.replies["status"] = twoPackages
	script.mu.Unlock()
	if !checker.AutoApplySystem(context.Background()) || !script.called("apply") {
		t.Fatalf("pas de nouvel essai après pi-appliance : %v", script.calls)
	}
}

func TestAutoSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	checker := &Checker{SettingsFile: path, Runner: &fakeScript{}}
	if err := checker.SetAuto(Auto{System: true, Hour: 0}); err != nil {
		t.Fatal(err)
	}
	if err := checker.SetChannel(Beta); err != nil {
		t.Fatal(err)
	}
	again := &Checker{SettingsFile: path}
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if again.Channel() != Beta || again.Auto() != (Auto{System: true, Hour: 0}) {
		t.Fatalf("relu : %s %+v", again.Channel(), again.Auto())
	}
	if err := again.SetAuto(Auto{Appliance: true, Hour: 24}); err == nil {
		t.Fatal("heure 24 acceptée")
	}
	if err := (&Checker{}).SetAuto(Auto{Appliance: true}); err == nil {
		t.Fatal("réglage enregistré sans fichier de réglages")
	}
	if err := (&Checker{SettingsFile: filepath.Join(t.TempDir(), "u.json")}).SetAuto(Auto{System: true}); err == nil {
		t.Fatal("système réglé sans script des mises à jour")
	}
	// Fichier d'une version précédente : canal seul, installations arrêtées.
	if err := os.WriteFile(path, []byte(`{"channel": "stable"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := again.Load(); err != nil || again.Auto() != (Auto{Hour: DefaultHour}) {
		t.Fatalf("ancien fichier : %+v, %v", again.Auto(), err)
	}
}

// Les deux cases sont indépendantes : changer l'une ne touche ni l'autre ni l'heure.
func TestAutoRoute(t *testing.T) {
	checker := &Checker{SettingsFile: filepath.Join(t.TempDir(), "updates.json"), Runner: &fakeScript{}}
	mux := http.NewServeMux()
	checker.Routes(mux, "/api/updates", func(h http.HandlerFunc) http.HandlerFunc { return h })
	put := func(body string) int {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/updates/auto", strings.NewReader(body)))
		return response.Code
	}
	for _, step := range []struct {
		body string
		code int
		want Auto
	}{
		{`{"appliance": true, "hour": 2}`, http.StatusOK, Auto{Appliance: true, Hour: 2}},
		{`{"system": true}`, http.StatusOK, Auto{Appliance: true, System: true, Hour: 2}},
		{`{"appliance": false}`, http.StatusOK, Auto{System: true, Hour: 2}},
		{`{"hour": 30}`, http.StatusBadRequest, Auto{System: true, Hour: 2}},
		{`{}`, http.StatusBadRequest, Auto{System: true, Hour: 2}},
		{`pas du JSON`, http.StatusBadRequest, Auto{System: true, Hour: 2}},
	} {
		if code := put(step.body); code != step.code || checker.Auto() != step.want {
			t.Errorf("%s : code %d, %+v ; attendu %d, %+v", step.body, code, checker.Auto(), step.code, step.want)
		}
	}
}
