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
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var archiveContent = []byte("archive de mise à jour v1.1.0")

// publishedRelease : la version v1.1.0 sur un faux GitHub, avec son archive
// et sa somme de contrôle (checksum vide : somme juste).
func publishedRelease(t *testing.T, checksum string) (*Release, *http.Client) {
	t.Helper()
	if checksum == "" {
		checksum = fmt.Sprintf("%x  %s\n", sha256.Sum256(archiveContent), ArchiveName("v1.1.0"))
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/archive":
			w.Write(archiveContent)
		case "/checksum":
			w.Write([]byte(checksum))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	release := &Release{Tag: "v1.1.0", Assets: []Asset{
		{Name: ArchiveName("v1.1.0"), URL: server.URL + "/archive", Size: int64(len(archiveContent))},
		{Name: ArchiveName("v1.1.0") + ".sha256", URL: server.URL + "/checksum"},
	}}
	return release, server.Client()
}

func applianceChecker(t *testing.T, release *Release, client *http.Client, status string) (*Checker, *fakeScript) {
	t.Helper()
	dir := t.TempDir()
	script := &fakeScript{replies: map[string]string{
		"status":  status,
		"refresh": `{"ok": true}`,
		"appliance " + filepath.Join(dir, ArchiveName("v1.1.0")) + " v1.1.0": `{"ok": true, "started": true}`,
	}}
	checker := &Checker{Runner: script, Releases: fakeReleases{release: release}, Version: "v1.0.0", DownloadDir: dir, Client: client}
	checker.Check(context.Background())
	return checker, script
}

// waitDownload attend la fin du téléchargement lancé par InstallAppliance.
func waitDownload(t *testing.T, checker *Checker) Appliance {
	t.Helper()
	for i := 0; i < 200; i++ {
		appliance := checker.Status(context.Background()).Appliance
		if appliance.Download == nil || appliance.Download.State != "downloading" {
			return appliance
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("téléchargement sans fin")
	return Appliance{}
}

func TestInstallAppliance(t *testing.T) {
	release, client := publishedRelease(t, "")
	checker, script := applianceChecker(t, release, client, twoPackages)
	if appliance := checker.Status(context.Background()).Appliance; !appliance.Available || !appliance.Installable {
		t.Fatalf("version non proposée : %+v", appliance)
	}
	version, err := checker.InstallAppliance(context.Background())
	if err != nil || version != "v1.1.0" {
		t.Fatalf("%q, %v", version, err)
	}
	if appliance := waitDownload(t, checker); appliance.Download != nil {
		t.Fatalf("téléchargement : %+v", appliance.Download)
	}
	path := filepath.Join(checker.DownloadDir, ArchiveName("v1.1.0"))
	if !script.called("appliance " + path + " v1.1.0") {
		t.Fatalf("updates.sh non lancé : %v", script.calls)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(archiveContent) {
		t.Fatalf("archive %q, %v", data, err)
	}
}

func TestInstallApplianceBadChecksum(t *testing.T) {
	release, client := publishedRelease(t, fmt.Sprintf("%064x  archive\n", 0))
	checker, script := applianceChecker(t, release, client, twoPackages)
	if _, err := checker.InstallAppliance(context.Background()); err != nil {
		t.Fatal(err)
	}
	appliance := waitDownload(t, checker)
	if appliance.Download == nil || appliance.Download.State != "failed" || appliance.Download.Message == "" {
		t.Fatalf("téléchargement : %+v", appliance.Download)
	}
	for _, call := range script.calls {
		if len(call) >= 9 && call[:9] == "appliance" {
			t.Fatalf("archive abîmée installée : %v", script.calls)
		}
	}
	entries, _ := os.ReadDir(checker.DownloadDir)
	if len(entries) != 0 {
		t.Fatalf("fichiers laissés : %v", entries)
	}
	if !checker.Status(context.Background()).Appliance.Installable {
		t.Fatal("nouvel essai impossible après un échec")
	}
}

func TestInstallApplianceRefused(t *testing.T) {
	release, client := publishedRelease(t, "")
	upgrading := `{"packages": [], "apply": null, "upgrade": {"state": "running", "version": "v1.1.0"}, "reboot_required": false}`
	withoutArchive := &Release{Tag: "v1.1.0", Assets: release.Assets[1:]}
	cases := map[string]struct {
		release *Release
		version string
		status  string
		code    string
	}{
		"à jour":           {release, "v1.1.0", twoPackages, "nothing"},
		"sans archive":     {withoutArchive, "v1.0.0", twoPackages, "unavailable"},
		"installation":     {release, "v1.0.0", upgrading, "busy"},
		"étiquette piégée": {&Release{Tag: "v1;reboot", Assets: release.Assets}, "v1.0.0", twoPackages, "unavailable"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			checker, _ := applianceChecker(t, c.release, client, c.status)
			checker.Version = c.version
			if checker.Status(context.Background()).Appliance.Installable {
				t.Fatal("installation proposée")
			}
			_, err := checker.InstallAppliance(context.Background())
			var failure Failure
			if !errors.As(err, &failure) || failure.Code != c.code {
				t.Fatalf("erreur %v, attendu %s", err, c.code)
			}
		})
	}
}

func TestGitHubAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"tag_name": "v1.1.0", "assets": [{"name": "pi-appliance-v1.1.0-arm64.tar.gz", "browser_download_url": "https://github.com/x", "size": 42}]}`))
	}))
	defer server.Close()
	release, err := GitHub{Repo: "gmoulard/pi-appliance", BaseURL: server.URL}.Latest(context.Background(), Stable)
	if err != nil {
		t.Fatal(err)
	}
	if asset, ok := release.Asset(ArchiveName("v1.1.0")); !ok || asset.Size != 42 || asset.URL != "https://github.com/x" {
		t.Fatalf("fichier joint : %+v", release.Assets)
	}
}

// fakeLister : faux GitHub qui donne aussi la liste des versions.
type fakeLister struct {
	fakeReleases
	releases []Release
	err      error
}

func (f fakeLister) List(context.Context) ([]Release, error) { return f.releases, f.err }

func TestVersions(t *testing.T) {
	release, client := publishedRelease(t, "")
	checker, _ := applianceChecker(t, release, client, twoPackages)
	checker.Releases = fakeLister{fakeReleases: fakeReleases{release: release}, releases: []Release{
		{Tag: "v1.0.0"},
		*release,
		{Tag: "v1.2.0-beta.1", Assets: release.Assets},
		{Tag: "v1.3.0", Draft: true},
		{Tag: "nuit-1234"},
		{Tag: "v0.9.0", Assets: release.Assets},
	}}
	versions, err := checker.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, v := range versions {
		got += fmt.Sprintf("%s:%t:%t:%t ", v.Version, v.Installable, v.Current, v.Newer)
	}
	// Canal stable : ni préversion, ni brouillon, ni étiquette sans numéro.
	// v0.9.0 n'est pas installable : son archive porte un autre nom.
	if want := "v1.1.0:true:false:true v1.0.0:false:true:false v0.9.0:false:false:false "; got != want {
		t.Fatalf("versions %q, attendu %q", got, want)
	}
	checker.Releases = fakeLister{err: errors.New("hors ligne")}
	var failure Failure
	if _, err := checker.Versions(context.Background()); !errors.As(err, &failure) || failure.Code != "offline" {
		t.Fatalf("erreur %v", err)
	}
	checker.Releases = fakeReleases{}
	if _, err := checker.Versions(context.Background()); !errors.As(err, &failure) || failure.Code != "unavailable" {
		t.Fatalf("erreur %v", err)
	}
}

func TestInstallVersion(t *testing.T) {
	release, client := publishedRelease(t, "")
	checker, script := applianceChecker(t, release, client, twoPackages)
	// Version plus ancienne que la version en service : permise.
	checker.Version = "v1.2.0"
	checker.Releases = fakeLister{fakeReleases: fakeReleases{release: release}, releases: []Release{*release}}
	cases := map[string]string{"v1.2.0": "nothing", "1.2.0": "nothing", "v9.9.9": "unknown", "v1;reboot": "version"}
	for tag, code := range cases {
		var failure Failure
		if _, err := checker.InstallVersion(context.Background(), tag); !errors.As(err, &failure) || failure.Code != code {
			t.Fatalf("%s : erreur %v, attendu %s", tag, err, code)
		}
	}
	version, err := checker.InstallVersion(context.Background(), "1.1.0")
	if err != nil || version != "v1.1.0" {
		t.Fatalf("%q, %v", version, err)
	}
	if appliance := waitDownload(t, checker); appliance.Download != nil {
		t.Fatalf("téléchargement : %+v", appliance.Download)
	}
	if !script.called("appliance " + filepath.Join(checker.DownloadDir, ArchiveName("v1.1.0")) + " v1.1.0") {
		t.Fatalf("updates.sh non lancé : %v", script.calls)
	}
}

func TestInstallHTTP(t *testing.T) {
	release, client := publishedRelease(t, "")
	checker, _ := applianceChecker(t, release, client, twoPackages)
	checker.Releases = fakeLister{fakeReleases: fakeReleases{release: release}, releases: []Release{*release}}
	mux := http.NewServeMux()
	checker.Routes(mux, "/api/updates", func(h http.HandlerFunc) http.HandlerFunc { return h })
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := http.Get(server.URL + "/api/updates/versions")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("versions : %v %v", response, err)
	}
	response.Body.Close()
	for body, status := range map[string]int{`{"version": "v9.9.9"}`: http.StatusBadRequest, `{`: http.StatusBadRequest} {
		response, err := http.Post(server.URL+"/api/updates/appliance", "application/json", strings.NewReader(body))
		if err != nil || response.StatusCode != status {
			t.Fatalf("%s : %v %v", body, response, err)
		}
		response.Body.Close()
	}
	// Sans corps, comme l'écran de la console : la dernière version.
	response, err = http.Post(server.URL+"/api/updates/appliance", "", nil)
	if err != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("installation : %v %v", response, err)
	}
	response.Body.Close()
	waitDownload(t, checker)
}
