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
	"context"
	"encoding/json"
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

// Le dossier du site est monté dans chaque service du champ files : Apache
// voit les fichiers du site, le constructeur Hugo reçoit les sources.
func TestSiteFilesMounted(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), builtinApp(t, "site-web"), "www", nil); err != nil {
		t.Fatal(err)
	}
	site := filepath.Join(m.serviceDir("www"), siteDir)
	volumes := overrideVolumes(t, m, "www")
	if len(volumes["web"]) != 1 || volumes["web"][0] != site+":/var/www/html" {
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
