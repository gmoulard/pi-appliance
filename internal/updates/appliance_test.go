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
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
