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

package marketplace

import (
	"encoding/json"
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
  tags: [documents]
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
	for _, id := range []string{"nextcloud", "site-web", "dokuwiki", "etherpad", "postgresql", "wireguard"} {
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
		"sans métadonnées":       {"x-pi-appliance:", "x-other:", "x-pi-appliance absente"},
		"service inconnu":        {"service: web", "service: db", "service exposé"},
		"sous-domaine":           {"subdomain: wiki", "subdomain: Wiki_1", "sous-domaine"},
		"port":                   {"port: 8080", "port: 0", "port absent"},
		"port négatif":           {"port: 8080", "port: -1", "port -1"},
		"port du Pi":             {"port: 8080", "port: 0\n  host_ports:\n    - port: 70000\n      protocol: udp", "port 70000"},
		"port réservé":           {"port: 8080", "port: 0\n  host_ports:\n    - port: 443\n      protocol: tcp", "réservé"},
		"protocole":              {"port: 8080", "port: 0\n  host_ports:\n    - port: 51820\n      protocol: sctp", "protocole"},
		"port en double":         {"port: 8080", "port: 0\n  host_ports:\n    - port: 51820\n      protocol: udp\n    - port: 51820\n      protocol: udp", "en double"},
		"image":                  {"image: example/wiki:1", "image: ''", "sans image"},
		"paramètre minuscule":    {"name: TITLE", "name: title", "paramètre \"title\""},
		"préfixe réservé":        {"name: TITLE", "name: PI_HOST", "préfixe PI_"},
		"accès":                  {"access: users", "access: everyone", "access"},
		"connexion":              {"login: application", "login: sso", "login"},
		"mémoire":                {"memory_mb: 128", "memory_mb: 0", "memory_mb"},
		"licence":                {"license: GPL-2.0", "license: ''", "license manquant"},
		"yaml":                   {"services:", "services: [", "manifeste wiki"},
		"icône absente":          {"  icon: M4 5a2 2 0 0 1 2-2h12v16H6a2 2 0 0 0-2 2zM4 19V5\n", "", "icon invalide"},
		"icône piégée":           {"icon: M4 5", "icon: M4 5\"/><script>", "icon invalide"},
		"fichiers service":       {"port: 8080", "port: 8080\n  files:\n    - service: db\n      path: /src", "files : service"},
		"fichiers relatif":       {"port: 8080", "port: 8080\n  files:\n    - service: web\n      path: src", "files : chemin"},
		"clé inconnue":           {"port: 8080", "port: 8080\n  porte: 80", "porte"},
		"clé inconnue imbriquée": {"name: TITLE", "name: TITLE\n      secret: true", "secret"},
		"fichiers racine":        {"port: 8080", "port: 8080\n  files:\n    - service: web\n      path: /", "files : chemin"},
		"sans étiquette":         {"tags: [documents]", "tags: []", "tags : de 1 à 4"},
		"étiquette inconnue":     {"tags: [documents]", "tags: [jeux]", "étiquette \"jeux\" inconnue"},
		"étiquette en double":    {"tags: [documents]", "tags: [documents, documents]", "en double"},
		"trop d'étiquettes":      {"tags: [documents]", "tags: [documents, fichiers, reseau, maison, donnees]", "tags : de 1 à 4"},
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

// Un service sans page web (VPN) ouvre ses ports directement sur le Pi.
func TestParseWithoutWebPage(t *testing.T) {
	app, err := Parse("wiki", []byte(strings.Replace(valid, "port: 8080", "port: 0\n  host_ports:\n    - port: 51820\n      protocol: udp", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if app.Web || len(app.HostPorts) != 1 || app.HostPorts[0] != (HostPort{Port: 51820, Protocol: "udp"}) {
		t.Fatalf("app %+v", app)
	}
	web, err := Parse("wiki", []byte(valid))
	if err != nil || !web.Web {
		t.Fatalf("site web %+v, %v", web, err)
	}
}

func TestBuiltinWireGuard(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get("wireguard")
	if !ok || app.Web || len(app.HostPorts) != 1 || app.HostPorts[0] != (HostPort{Port: 51820, Protocol: "udp"}) {
		t.Fatalf("wireguard %+v", app)
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

// Les générateurs de sites statiques (TODO-2.46) reçoivent leurs fichiers
// source dans le constructeur, et le site web dans Apache.
func TestBuiltinSiteFiles(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"hugo", "mkdocs", "jekyll", "astro", "nextjs"} {
		app, ok := catalog.Get(id)
		if !ok {
			t.Fatalf("%s absent du catalogue", id)
		}
		if !app.SiteFiles || len(app.Files) != 1 || app.Files[0] != (FileMount{Service: "builder", Path: "/src"}) || app.Service != "web" || app.Port != 80 {
			t.Errorf("%s : %+v", id, app)
		}
	}
	site, _ := catalog.Get("site-web")
	if len(site.Files) != 1 || site.Files[0] != (FileMount{Service: "web", Path: "/var/www/html"}) {
		t.Errorf("site-web : %+v", site.Files)
	}
	if wiki, _ := catalog.Get("dokuwiki"); wiki.SiteFiles {
		t.Error("dokuwiki ne reçoit pas de fichiers de site")
	}
}

// Les étiquettes ne sont pas dans le fichier Compose publié (lu strictement
// par les consoles 0.1.x) mais dans le champ tags, repris à la lecture.
func TestPublishedTags(t *testing.T) {
	catalog, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Publish(catalog, nil))
	if err != nil {
		t.Fatal(err)
	}
	manifests, err := ReadDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, manifest := range manifests {
		builtin, _ := catalog.Get(manifest.ID)
		if manifest.Err != nil || strings.Join(manifest.App.Tags, ",") != strings.Join(builtin.Tags, ",") {
			t.Errorf("%s : %v, étiquettes %v", manifest.ID, manifest.Err, manifest.App.Tags)
		}
		if strings.Contains(string(manifest.App.Compose), "tags:") {
			t.Errorf("%s : tags dans le fichier Compose publié", manifest.ID)
		}
	}
	refused := strings.Replace(string(data), `"tags":["site-web"]`, `"tags":["jeux"]`, 1)
	manifests, err = ReadDocument([]byte(refused))
	if err != nil {
		t.Fatal(err)
	}
	bad := 0
	for _, manifest := range manifests {
		if manifest.Err != nil {
			bad++
		}
	}
	if bad != 1 {
		t.Fatalf("%d manifestes refusés, attendu 1", bad)
	}
}

func TestWithoutTags(t *testing.T) {
	cases := map[string]string{
		"x-pi-appliance:\n  name: A\n  tags: [site-web]\n  port: 80\nservices: {}\n":                    "x-pi-appliance:\n  name: A\n  port: 80\nservices: {}\n",
		"x-pi-appliance:\n  name: A\n  tags:\n    - site-web\n    - maison\n  port: 80\nservices: {}\n": "x-pi-appliance:\n  name: A\n  port: 80\nservices: {}\n",
		"x-pi-appliance:\n  name: A\n  tags:\n  - site-web\n  port: 80\nservices:\n  tags: {}\n":        "x-pi-appliance:\n  name: A\n  port: 80\nservices:\n  tags: {}\n",
	}
	for in, want := range cases {
		if got := string(withoutTags([]byte(in))); got != want {
			t.Errorf("withoutTags(%q) = %q", in, got)
		}
	}
}
