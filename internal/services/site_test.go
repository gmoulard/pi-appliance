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

package services

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/marketplace"
)

func builtinApp(t *testing.T, id string) marketplace.App {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get(id)
	if !ok {
		t.Fatalf("%s absent du catalogue", id)
	}
	return app
}

func overrideVolumes(t *testing.T, m *Manager, name string) map[string][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), overrideName))
	if err != nil {
		t.Fatal(err)
	}
	var override struct {
		Services map[string]struct {
			Volumes []string `json:"volumes"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &override); err != nil {
		t.Fatal(err)
	}
	volumes := map[string][]string{}
	for service, content := range override.Services {
		volumes[service] = content.Volumes
	}
	return volumes
}

func siteZIP(t *testing.T, files map[string]string) *bytes.Reader {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range files {
		w, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buffer.Bytes())
}

// Le dossier du site est monté dans chaque service du champ files : Apache et
// FTP voient les mêmes fichiers, le constructeur Hugo reçoit les sources.
func TestSiteFilesMounted(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), builtinApp(t, "site-web"), "www", map[string]string{"FTP_PASSWORD": ""}); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(m.serviceDir("www"), siteDir)
	volumes := overrideVolumes(t, m, "www")
	if len(volumes["web"]) != 1 || volumes["web"][0] != site+":/var/www/html" || len(volumes["ftp"]) != 1 || volumes["ftp"][0] != site+":/var/www/html" {
		t.Fatalf("volumes %v", volumes)
	}
	if _, err := m.Install(context.Background(), builtinApp(t, "hugo"), "blog", nil); err != nil {
		t.Fatal(err)
	}
	volumes = overrideVolumes(t, m, "blog")
	if len(volumes["builder"]) != 1 || volumes["builder"][0] != filepath.Join(m.serviceDir("blog"), siteDir)+":/src" || len(volumes["web"]) != 0 {
		t.Fatalf("volumes %v", volumes)
	}
}

func TestDeployZIPStaticSite(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), builtinApp(t, "mkdocs"), "docs", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(context.Background(), builtinApp(t, "dokuwiki"), "wiki", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.DeployZIP(context.Background(), "docs", siteZIP(t, map[string]string{"mkdocs.yml": "site_name: Essai\n", "docs/index.md": "# Essai\n"})); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(m.serviceDir("docs"), siteDir, "docs", "index.md")); err != nil || string(data) != "# Essai\n" {
		t.Fatalf("fichier déposé : %q, %v", data, err)
	}
	if err := m.DeployZIP(context.Background(), "wiki", siteZIP(t, map[string]string{"index.html": "x"})); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ZIP accepté pour DokuWiki : %v", err)
	}
}

func TestListSiteBuild(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), builtinApp(t, "astro"), "vitrine", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(context.Background(), builtinApp(t, "dokuwiki"), "wiki", nil); err != nil {
		t.Fatal(err)
	}
	list := func() map[string]Instance {
		instances, err := m.List()
		if err != nil {
			t.Fatal(err)
		}
		byName := map[string]Instance{}
		for _, instance := range instances {
			byName[instance.Name] = instance
		}
		return byName
	}
	if got := list(); !got["vitrine"].SiteFiles || got["vitrine"].Build != nil || got["wiki"].SiteFiles {
		t.Fatalf("avant la construction : %+v", got)
	}
	dir := filepath.Join(m.serviceDir("vitrine"), siteDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, buildStateName), []byte("failed\n2026-11-20T07:30:03Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	build := list()["vitrine"].Build
	if build == nil || build.State != "failed" || !build.At.Equal(time.Date(2026, 11, 20, 7, 30, 3, 0, time.UTC)) {
		t.Fatalf("construction %+v", build)
	}
	for _, content := range []string{"inconnu\n2026-11-20T07:30:03Z\n", "ok\nhier\n", "ok\n"} {
		if err := os.WriteFile(filepath.Join(dir, buildStateName), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if build := list()["vitrine"].Build; build != nil {
			t.Errorf("%q lu comme %+v", content, build)
		}
	}
}

// Un nouveau dépôt garde le dossier du site : les conteneurs démarrés le
// montent et doivent voir les nouveaux fichiers.
func TestDeployZIPKeepsFolder(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), builtinApp(t, "hugo"), "blog", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.DeployZIP(context.Background(), "blog", siteZIP(t, map[string]string{"ancien.md": "a"})); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(m.serviceDir("blog"), siteDir)
	before, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DeployZIP(context.Background(), "blog", siteZIP(t, map[string]string{"hugo.toml": "title = 'x'\n", "content/a.md": "b"})); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("dossier du site remplacé : les conteneurs ne verraient pas les nouveaux fichiers")
	}
	if _, err := os.Stat(filepath.Join(root, "ancien.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ancien fichier gardé : %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "content", "a.md")); err != nil {
		t.Fatal(err)
	}
}
