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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeScript tient lieu de updates.sh : une réponse par commande.
type fakeScript struct {
	mu      sync.Mutex
	replies map[string]string
	fails   map[string]bool
	calls   []string
}

func (f *fakeScript) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	command := strings.Join(args, " ")
	f.calls = append(f.calls, command)
	if f.fails[command] {
		return []byte(f.replies[command]), errors.New("exit status 1")
	}
	return []byte(f.replies[command]), nil
}

func (f *fakeScript) called(command string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c == command {
			return true
		}
	}
	return false
}

// fakeReleases tient lieu de GitHub : aucun appel réseau.
type fakeReleases struct {
	release *Release
	err     error
}

func (f fakeReleases) Latest(context.Context) (*Release, error) {
	return f.release, f.err
}

const twoPackages = `{"packages": [
 {"name": "libssl3t64", "from": "3.5.1-1", "to": "3.5.1-1+deb13u1", "security": true},
 {"name": "raspi-firmware", "from": "1:1.20250430-1", "to": "1:1.20250915-1", "security": false}],
 "checked_at": "2026-11-20T03:00:00Z", "apply": null, "reboot_required": false}`

func TestStatusCountsSecurityPatches(t *testing.T) {
	script := &fakeScript{replies: map[string]string{"status": twoPackages}}
	checker := &Checker{Runner: script, Version: "dev"}
	status := checker.Status(context.Background())
	if status.System == nil || len(status.System.Packages) != 2 || status.System.Security != 1 {
		t.Fatalf("système : %+v", status.System)
	}
	if status.System.Busy() || status.SystemError != "" {
		t.Fatalf("état : %+v", status)
	}
	if script.called("apply") || script.called("refresh") {
		t.Fatal("la lecture de l'état ne doit rien installer ni relire les dépôts")
	}
}

func TestStatusWithoutScript(t *testing.T) {
	status := (&Checker{Version: "1.0.0"}).Status(context.Background())
	if status.System != nil || status.Appliance.Current != "1.0.0" || status.Appliance.Available {
		t.Fatalf("état : %+v", status)
	}
}

func TestStatusUnreadable(t *testing.T) {
	script := &fakeScript{replies: map[string]string{"status": "pas du JSON"}}
	status := (&Checker{Runner: script}).Status(context.Background())
	if status.System != nil || status.SystemError == "" {
		t.Fatalf("état : %+v", status)
	}
}

func TestApplyBusy(t *testing.T) {
	script := &fakeScript{
		replies: map[string]string{"apply": `{"ok": false, "error": "busy", "message": "Des mises à jour sont déjà en cours d'installation."}`},
		fails:   map[string]bool{"apply": true},
	}
	err := (&Checker{Runner: script}).ApplySystem(context.Background())
	var failure Failure
	if !errors.As(err, &failure) || failure.Code != "busy" {
		t.Fatalf("erreur : %v", err)
	}
}

func TestCheckFindsNewRelease(t *testing.T) {
	script := &fakeScript{replies: map[string]string{"refresh": `{"ok": true}`, "status": twoPackages}}
	now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	checker := &Checker{
		Runner:   script,
		Releases: fakeReleases{release: &Release{Tag: "v1.3.0", URL: "https://github.com/gmoulard/pi-appliance/releases/tag/v1.3.0"}},
		Version:  "v1.2.0",
		Now:      func() time.Time { return now },
	}
	if status := checker.Status(context.Background()); status.Appliance.Available || status.Appliance.CheckedAt != nil {
		t.Fatalf("avant la recherche : %+v", status.Appliance)
	}
	if !checker.Check(context.Background()) {
		t.Fatal("recherche refusée")
	}
	if !script.called("refresh") || script.called("apply") {
		t.Fatalf("appels : %v", script.calls)
	}
	appliance := checker.Status(context.Background()).Appliance
	if !appliance.Available || appliance.Latest != "v1.3.0" || appliance.URL == "" || !appliance.CheckedAt.Equal(now) {
		t.Fatalf("pi-appliance : %+v", appliance)
	}
}

func TestCheckReleaseErrorKeepsLastKnown(t *testing.T) {
	releases := &switchReleases{release: &Release{Tag: "1.3.0"}}
	checker := &Checker{Releases: releases, Version: "1.2.0"}
	checker.Check(context.Background())
	releases.err = errors.New("réseau coupé")
	checker.Check(context.Background())
	appliance := checker.Status(context.Background()).Appliance
	if !appliance.Available || appliance.Latest != "1.3.0" || appliance.Error == "" {
		t.Fatalf("pi-appliance : %+v", appliance)
	}
}

type switchReleases struct {
	release *Release
	err     error
}

func (s *switchReleases) Latest(context.Context) (*Release, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.release, nil
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		current, latest string
		want            bool
	}{
		{"1.2.0", "1.3.0", true},
		{"v1.2.0", "v1.2.0", false},
		{"1.2.0", "v1.2.0", false},
		{"1.10.0", "1.9.0", false},
		{"1.2", "1.2.1", true},
		{"2.0.0", "1.9.9", false},
		{"1.2.0-beta.1", "1.2.0", true},
		{"1.2.0", "1.2.0-beta.1", false},
		{"1.2.0-beta.1", "1.2.0-beta.2", true},
		{"0c602d9", "1.0.0", true},
		{"1234567", "1.0.0", true},
		{"dev", "1.0.0", false},
		{"1.0.0", "", false},
	} {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, attendu %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestGitHubLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/gmoulard/pi-appliance/releases/latest" || r.Header.Get("User-Agent") == "" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name": "v1.3.0", "html_url": "https://example.test/v1.3.0", "prerelease": false}`))
	}))
	defer server.Close()
	release, err := GitHub{Repo: DefaultRepo, BaseURL: server.URL, Client: server.Client()}.Latest(context.Background())
	if err != nil || release == nil || release.Tag != "v1.3.0" || release.URL != "https://example.test/v1.3.0" {
		t.Fatalf("version : %+v, %v", release, err)
	}
}

func TestGitHubNoRelease(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	release, err := GitHub{Repo: DefaultRepo, BaseURL: server.URL, Client: server.Client()}.Latest(context.Background())
	if err != nil || release != nil {
		t.Fatalf("aucune version publiée : %+v, %v", release, err)
	}
}

func TestGitHubError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	if _, err := (GitHub{Repo: DefaultRepo, BaseURL: server.URL, Client: server.Client()}).Latest(context.Background()); err == nil {
		t.Fatal("limite de GitHub : erreur attendue")
	}
}

func TestRoutes(t *testing.T) {
	script := &fakeScript{replies: map[string]string{"status": twoPackages, "apply": `{"ok": true, "started": true}`, "refresh": `{"ok": true}`}}
	checker := &Checker{Runner: script, Version: "dev"}
	mux := http.NewServeMux()
	guarded := 0
	checker.Routes(mux, "/api/updates", func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			guarded++
			next(w, r)
		}
	})
	serve := func(method, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder
	}
	response := serve("GET", "/api/updates")
	var status Status
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &status) != nil || status.System.Security != 1 {
		t.Fatalf("GET : %d %s", response.Code, response.Body)
	}
	if response := serve("POST", "/api/updates/system"); response.Code != http.StatusAccepted || !script.called("apply") {
		t.Fatalf("POST system : %d %s", response.Code, response.Body)
	}
	if response := serve("POST", "/api/updates/check"); response.Code != http.StatusAccepted {
		t.Fatalf("POST check : %d %s", response.Code, response.Body)
	}
	if guarded != 3 {
		t.Fatalf("routes protégées : %d sur 3", guarded)
	}
}

func TestApplyWithoutScript(t *testing.T) {
	mux := http.NewServeMux()
	(&Checker{}).Routes(mux, "/u", func(next http.HandlerFunc) http.HandlerFunc { return next })
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest("POST", "/u/system", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("sans script : %d", recorder.Code)
	}
}
