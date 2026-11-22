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

package updates

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// channelReleases rend une version différente par canal.
type channelReleases map[string]*Release

func (c channelReleases) Latest(_ context.Context, channel string) (*Release, error) {
	return c[channel], nil
}

func TestChannelStableByDefault(t *testing.T) {
	dir := t.TempDir()
	checker := &Checker{SettingsFile: filepath.Join(dir, "updates.json"), Version: "1.2.0"}
	if err := checker.Load(); err != nil {
		t.Fatal(err)
	}
	if checker.Channel() != Stable {
		t.Fatalf("canal par défaut : %s", checker.Channel())
	}
	if status := checker.Status(context.Background()); status.Appliance.Channel != Stable {
		t.Fatalf("état : %+v", status.Appliance)
	}
}

func TestChannelSavedAndReloaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	checker := &Checker{SettingsFile: path}
	if err := checker.SetChannel(Beta); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %v", info.Mode().Perm())
	}
	again := &Checker{SettingsFile: path}
	if err := again.Load(); err != nil || again.Channel() != Beta {
		t.Fatalf("canal relu : %s, %v", again.Channel(), err)
	}
	if err := again.SetChannel("nightly"); err == nil {
		t.Fatal("canal inconnu accepté")
	}
	if err := (&Checker{}).SetChannel(Beta); err == nil {
		t.Fatal("canal changé sans fichier de réglages")
	}
}

func TestChannelUnknownValueIsStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updates.json")
	if err := os.WriteFile(path, []byte(`{"channel": "nightly"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	checker := &Checker{SettingsFile: path}
	if err := checker.Load(); err != nil || checker.Channel() != Stable {
		t.Fatalf("canal inconnu : %s, %v", checker.Channel(), err)
	}
}

func TestStableNeverOffersPreview(t *testing.T) {
	// Même si la source renvoie une préversion sur le canal stable, elle
	// n'est ni proposée ni installable.
	releases := channelReleases{Stable: {Tag: "v1.3.0-beta.1", Prerelease: true}}
	checker := &Checker{Releases: releases, Version: "v1.2.0"}
	checker.Check(context.Background())
	if appliance := checker.Status(context.Background()).Appliance; appliance.Available || appliance.Latest != "" {
		t.Fatalf("préversion proposée sur le canal stable : %+v", appliance)
	}
	releases[Stable] = &Release{Tag: "v1.3.0", Prerelease: true}
	checker.Check(context.Background())
	if appliance := checker.Status(context.Background()).Appliance; appliance.Available {
		t.Fatalf("version marquée préversion proposée sur le canal stable : %+v", appliance)
	}
}

func TestBetaChannelOffersPreview(t *testing.T) {
	releases := channelReleases{Stable: {Tag: "v1.2.1"}, Beta: {Tag: "v1.3.0-beta.1", Prerelease: true}}
	checker := &Checker{Releases: releases, Version: "v1.2.0", SettingsFile: filepath.Join(t.TempDir(), "updates.json")}
	checker.Check(context.Background())
	if appliance := checker.Status(context.Background()).Appliance; appliance.Latest != "v1.2.1" {
		t.Fatalf("canal stable : %+v", appliance)
	}
	if err := checker.SetChannel(Beta); err != nil {
		t.Fatal(err)
	}
	if appliance := checker.Status(context.Background()).Appliance; appliance.Latest != "" || appliance.Available {
		t.Fatalf("version de l'ancien canal gardée : %+v", appliance)
	}
	checker.Check(context.Background())
	if appliance := checker.Status(context.Background()).Appliance; appliance.Latest != "v1.3.0-beta.1" || !appliance.Available || appliance.Channel != Beta {
		t.Fatalf("canal bêta : %+v", appliance)
	}
	// Retour au canal stable avec une bêta en service : pas de retour en
	// arrière proposé, la prochaine version stable plus récente le sera.
	checker.Version = "v1.3.0-beta.1"
	if err := checker.SetChannel(Stable); err != nil {
		t.Fatal(err)
	}
	checker.Check(context.Background())
	if appliance := checker.Status(context.Background()).Appliance; appliance.Available {
		t.Fatalf("retour en arrière proposé : %+v", appliance)
	}
}

func TestInstallRefusesPreviewOnStable(t *testing.T) {
	checker := &Checker{Runner: &fakeScript{}, Version: "v1.2.0", DownloadDir: t.TempDir()}
	checker.release = &Release{Tag: "v1.3.0-beta.1", Assets: []Asset{
		{Name: ArchiveName("v1.3.0-beta.1"), URL: "https://example.test/a"},
		{Name: ArchiveName("v1.3.0-beta.1") + ".sha256", URL: "https://example.test/b"},
	}}
	_, err := checker.InstallAppliance(context.Background())
	var failure Failure
	if !errors.As(err, &failure) || failure.Code != "nothing" {
		t.Fatalf("préversion installée sur le canal stable : %v", err)
	}
}

func TestGitHubBetaPicksNewest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/gmoulard/pi-appliance/releases" || r.URL.Query().Get("per_page") != "30" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`[
 {"tag_name": "v1.4.0-beta.1", "draft": true, "prerelease": true},
 {"tag_name": "v1.3.0-beta.9", "prerelease": true},
 {"tag_name": "v1.3.0-beta.10", "prerelease": true},
 {"tag_name": "v1.2.0"},
 {"tag_name": "nightly", "prerelease": true}]`))
	}))
	defer server.Close()
	release, err := GitHub{Repo: DefaultRepo, BaseURL: server.URL, Client: server.Client()}.Latest(context.Background(), Beta)
	if err != nil || release == nil || release.Tag != "v1.3.0-beta.10" {
		t.Fatalf("bêta : %+v, %v", release, err)
	}
}

func TestGitHubBetaStableIsNewer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`[{"tag_name": "v1.3.0-beta.2", "prerelease": true}, {"tag_name": "v1.3.0"}]`))
	}))
	defer server.Close()
	release, err := GitHub{Repo: DefaultRepo, BaseURL: server.URL, Client: server.Client()}.Latest(context.Background(), Beta)
	if err != nil || release == nil || release.Tag != "v1.3.0" {
		t.Fatalf("bêta : %+v, %v", release, err)
	}
}

func TestGitHubStableSkipsPreview(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name": "v1.3.0-rc.1", "prerelease": false}`))
	}))
	defer server.Close()
	release, err := GitHub{Repo: DefaultRepo, BaseURL: server.URL, Client: server.Client()}.Latest(context.Background(), Stable)
	if err != nil || release != nil {
		t.Fatalf("préversion sur le canal stable : %+v, %v", release, err)
	}
}

func TestChannelRoute(t *testing.T) {
	checker := &Checker{SettingsFile: filepath.Join(t.TempDir(), "updates.json")}
	mux := http.NewServeMux()
	checker.Routes(mux, "/u", func(next http.HandlerFunc) http.HandlerFunc { return next })
	put := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("PUT", "/u/channel", strings.NewReader(body)))
		return recorder
	}
	if response := put(`{"channel": "beta"}`); response.Code != http.StatusOK || checker.Channel() != Beta {
		t.Fatalf("PUT beta : %d %s", response.Code, response.Body)
	}
	if response := put(`{"channel": "nightly"}`); response.Code != http.StatusBadRequest || checker.Channel() != Beta {
		t.Fatalf("PUT nightly : %d %s", response.Code, response.Body)
	}
	if response := put(`pas du json`); response.Code != http.StatusBadRequest {
		t.Fatalf("PUT illisible : %d", response.Code)
	}
}
