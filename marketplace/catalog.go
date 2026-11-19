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
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed */compose.yaml
var builtin embed.FS

var (
	labelPattern    = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,30}[a-z0-9])?$`)
	variablePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	logins          = map[string]bool{"console": true, "application": true}
	accesses        = map[string]bool{"public": true, "users": true, "home": true}
)

type Parameter struct {
	Name     string `yaml:"name" json:"name"`
	Label    string `yaml:"label" json:"label"`
	Generate bool   `yaml:"generate" json:"generate"`
	Default  string `yaml:"default" json:"default,omitempty"`
}

type App struct {
	ID          string      `yaml:"-" json:"id"`
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Category    string      `yaml:"category" json:"category"`
	License     string      `yaml:"license" json:"license"`
	Subdomain   string      `yaml:"subdomain" json:"subdomain"`
	Service     string      `yaml:"service" json:"-"`
	Port        int         `yaml:"port" json:"-"`
	MemoryMB    int         `yaml:"memory_mb" json:"memory_mb"`
	DiskMB      int         `yaml:"disk_mb" json:"disk_mb"`
	Login       string      `yaml:"login" json:"login"`
	Access      string      `yaml:"access" json:"access"`
	Parameters  []Parameter `yaml:"parameters" json:"parameters"`
	Compose     []byte      `yaml:"-" json:"-"`
}

type composeFile struct {
	Meta     *App `yaml:"x-pi-appliance"`
	Services map[string]struct {
		Image string `yaml:"image"`
	} `yaml:"services"`
}

type Catalog struct {
	apps map[string]App
}

func ValidLabel(name string) bool {
	return labelPattern.MatchString(name)
}

func Builtin() (Catalog, error) {
	return Load(builtin)
}

func Load(fsys fs.FS) (Catalog, error) {
	paths, err := fs.Glob(fsys, "*/compose.yaml")
	if err != nil {
		return Catalog{}, err
	}
	if len(paths) == 0 {
		return Catalog{}, errors.New("catalogue vide : aucun */compose.yaml")
	}
	catalog := Catalog{apps: map[string]App{}}
	for _, file := range paths {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return Catalog{}, err
		}
		app, err := Parse(path.Dir(file), data)
		if err != nil {
			return Catalog{}, err
		}
		catalog.apps[app.ID] = app
	}
	return catalog, nil
}

func Parse(id string, data []byte) (App, error) {
	var file composeFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return App{}, fmt.Errorf("manifeste %s : %w", id, err)
	}
	if file.Meta == nil {
		return App{}, fmt.Errorf("manifeste %s : section x-pi-appliance absente", id)
	}
	app := *file.Meta
	app.ID = id
	app.Compose = data
	if problems := check(app, file); len(problems) > 0 {
		return App{}, fmt.Errorf("manifeste %s : %s", id, strings.Join(problems, " ; "))
	}
	return app, nil
}

func check(app App, file composeFile) []string {
	var problems []string
	fail := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if !ValidLabel(app.ID) {
		fail("identifiant %q invalide (minuscules, chiffres, tirets)", app.ID)
	}
	for field, value := range map[string]string{"name": app.Name, "description": app.Description, "category": app.Category, "license": app.License} {
		if strings.TrimSpace(value) == "" {
			fail("%s manquant", field)
		}
	}
	if !ValidLabel(app.Subdomain) {
		fail("sous-domaine %q invalide", app.Subdomain)
	}
	if len(file.Services) == 0 {
		fail("aucun service Docker")
	}
	for name, service := range file.Services {
		if strings.TrimSpace(service.Image) == "" {
			fail("service %s sans image", name)
		}
	}
	if _, ok := file.Services[app.Service]; !ok {
		fail("service exposé %q absent de services", app.Service)
	}
	if app.Port < 1 || app.Port > 65535 {
		fail("port %d invalide", app.Port)
	}
	if app.MemoryMB <= 0 || app.DiskMB <= 0 {
		fail("memory_mb et disk_mb doivent être positifs")
	}
	if !logins[app.Login] {
		fail("login %q invalide (console ou application)", app.Login)
	}
	if !accesses[app.Access] {
		fail("access %q invalide (public, users ou home)", app.Access)
	}
	seen := map[string]bool{}
	for _, parameter := range app.Parameters {
		switch {
		case !variablePattern.MatchString(parameter.Name):
			fail("paramètre %q invalide (majuscules, chiffres, _)", parameter.Name)
		case strings.HasPrefix(parameter.Name, "PI_"):
			fail("paramètre %s : préfixe PI_ réservé", parameter.Name)
		case seen[parameter.Name]:
			fail("paramètre %s en double", parameter.Name)
		case !parameter.Generate && strings.TrimSpace(parameter.Label) == "":
			fail("paramètre %s sans label", parameter.Name)
		}
		seen[parameter.Name] = true
	}
	return problems
}

func (c Catalog) Apps() []App {
	apps := make([]App, 0, len(c.apps))
	for _, app := range c.apps {
		apps = append(apps, app)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps
}

func (c Catalog) Get(id string) (App, bool) {
	app, ok := c.apps[id]
	return app, ok
}
