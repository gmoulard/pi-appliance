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

package marketplace

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

const valid = `x-pi-appliance:
  name: Wiki
  description: Un wiki.
  icon: M4 5a2 2 0 0 1 2-2h12v16H6a2 2 0 0 0-2 2zM4 19V5
  category: Documents
  license: GPL-2.0
  subdomain: wiki
  service: web
  port: 8080
  memory_mb: 128
  disk_mb: 256
  login: application
  access: users
  parameters:
    - name: TITLE
      label: Titre
      default: Mon wiki
services:
  web:
    image: example/wiki:1
`

func TestBuiltinCatalog(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get("nextcloud")
	if !ok {
		t.Fatal("nextcloud absent du catalogue")
	}
	if app.Subdomain != "cloud" || app.Service != "app" || app.Port != 80 || app.Icon == "" || len(app.Compose) == 0 {
		t.Fatalf("nextcloud %+v", app)
	}
}

var composeVariable = regexp.MustCompile(`\$\{([A-Za-z0-9_]+)\}`)

// Chaque manifeste livré se lit, vise une image à version fixée et n'utilise
// que des variables déclarées (paramètres ou PI_INSTANCE, PI_HOST).
func TestBuiltinManifests(t *testing.T) {
	paths, err := fs.Glob(builtin, "*/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, file := range paths {
		id := path.Dir(file)
		ids[id] = true
		t.Run(id, func(t *testing.T) {
			data, err := fs.ReadFile(builtin, file)
			if err != nil {
				t.Fatal(err)
			}
			app, err := Parse(id, data)
			if err != nil {
				t.Fatal(err)
			}
			var compose composeFile
			if err := yaml.Unmarshal(data, &compose); err != nil {
				t.Fatal(err)
			}
			for name, service := range compose.Services {
				_, tag, ok := strings.Cut(service.Image, ":")
				if !ok || tag == "latest" || tag == "stable" {
					t.Errorf("service %s : image %q sans version fixée", name, service.Image)
				}
			}
			declared := map[string]bool{"PI_INSTANCE": true, "PI_HOST": true}
			for _, parameter := range app.Parameters {
				declared[parameter.Name] = true
			}
			for _, match := range composeVariable.FindAllStringSubmatch(strings.ReplaceAll(string(data), "$$", ""), -1) {
				if !declared[match[1]] {
					t.Errorf("variable %s utilisée mais non déclarée", match[1])
				}
			}
		})
	}
	for _, id := range []string{"nextcloud", "site-web", "dokuwiki", "etherpad"} {
		if !ids[id] {
			t.Errorf("manifeste %s absent", id)
		}
	}
}

func TestLoadSortsByName(t *testing.T) {
	other := strings.Replace(valid, "name: Wiki", "name: Agenda", 1)
	catalog, err := Load(fstest.MapFS{
		"wiki/compose.yaml":   {Data: []byte(valid)},
		"agenda/compose.yaml": {Data: []byte(other)},
	})
	if err != nil {
		t.Fatal(err)
	}
	apps := catalog.Apps()
	if len(apps) != 2 || apps[0].ID != "agenda" || apps[1].ID != "wiki" {
		t.Fatalf("apps %+v", apps)
	}
}

func TestLoadEmpty(t *testing.T) {
	if _, err := Load(fstest.MapFS{}); err == nil {
		t.Fatal("catalogue vide accepté")
	}
}

func TestParseRejects(t *testing.T) {
	cases := map[string]struct{ old, new, want string }{
		"sans métadonnées":    {"x-pi-appliance:", "x-other:", "x-pi-appliance absente"},
		"service inconnu":     {"service: web", "service: db", "service exposé"},
		"sous-domaine":        {"subdomain: wiki", "subdomain: Wiki_1", "sous-domaine"},
		"port":                {"port: 8080", "port: 0", "port 0"},
		"image":               {"image: example/wiki:1", "image: ''", "sans image"},
		"paramètre minuscule": {"name: TITLE", "name: title", "paramètre \"title\""},
		"préfixe réservé":     {"name: TITLE", "name: PI_HOST", "préfixe PI_"},
		"accès":               {"access: users", "access: everyone", "access"},
		"connexion":           {"login: application", "login: sso", "login"},
		"mémoire":             {"memory_mb: 128", "memory_mb: 0", "memory_mb"},
		"licence":             {"license: GPL-2.0", "license: ''", "license manquant"},
		"yaml":                {"services:", "services: [", "manifeste wiki"},
		"icône absente":       {"  icon: M4 5a2 2 0 0 1 2-2h12v16H6a2 2 0 0 0-2 2zM4 19V5\n", "", "icon invalide"},
		"icône piégée":        {"icon: M4 5", "icon: M4 5\"/><script>", "icon invalide"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("wiki", []byte(strings.Replace(valid, c.old, c.new, 1)))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("erreur %v, attendu %q", err, c.want)
			}
		})
	}
}

func TestParseBadID(t *testing.T) {
	if _, err := Parse("Wiki", []byte(valid)); err == nil || !strings.Contains(err.Error(), "identifiant") {
		t.Fatalf("erreur %v", err)
	}
}

func TestValidLabel(t *testing.T) {
	for label, want := range map[string]bool{"cloud": true, "blog-2": true, "a": true, "-a": false, "a-": false, "2a": false, "A": false, "": false} {
		if ValidLabel(label) != want {
			t.Errorf("ValidLabel(%q) != %v", label, want)
		}
	}
}
