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
	"bytes"
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
	iconPattern     = regexp.MustCompile(`^[MmLlHhVvCcSsQqTtAaZz][MmLlHhVvCcSsQqTtAaZz0-9 ,.-]*$`)
	logins          = map[string]bool{"console": true, "application": true}
	// admins : administrateurs, à la maison, pour une application qui a la
	// main sur tout le Pi (accès à Docker) ; la console ne le laisse pas changer.
	accesses  = map[string]bool{"public": true, "users": true, "home": true, "admins": true}
	protocols = map[string]bool{"tcp": true, "udp": true}
	// Ports de Traefik et de la console, jamais ouverts par un manifeste.
	reservedPorts = map[int]bool{80: true, 443: true, 8080: true}
)

// HostPort est un port ouvert directement sur le Pi, sans Traefik, pour un
// service qui n'est pas un site web (VPN, partage de fichiers…). Le même
// numéro est utilisé sur le Pi et dans le conteneur publié (service).
type HostPort struct {
	Port     int    `yaml:"port" json:"port"`
	Protocol string `yaml:"protocol" json:"protocol"`
}

// FileMount : le dossier des fichiers du site (dépôt par Nextcloud) monté
// dans un service, à un chemin absolu.
type FileMount struct {
	Service string `yaml:"service" json:"-"`
	Path    string `yaml:"path" json:"-"`
}

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
	Icon        string      `yaml:"icon" json:"icon"`
	Category    string      `yaml:"category" json:"category"`
	Tags        []string    `yaml:"tags" json:"tags"`
	License     string      `yaml:"license" json:"license"`
	Subdomain   string      `yaml:"subdomain" json:"subdomain"`
	Service     string      `yaml:"service" json:"-"`
	Port        int         `yaml:"port" json:"-"`
	MemoryMB    int         `yaml:"memory_mb" json:"memory_mb"`
	DiskMB      int         `yaml:"disk_mb" json:"disk_mb"`
	Login       string      `yaml:"login" json:"login"`
	Access      string      `yaml:"access" json:"access"`
	Parameters  []Parameter `yaml:"parameters" json:"parameters"`
	HostPorts   []HostPort  `yaml:"host_ports" json:"host_ports,omitempty"`
	Files       []FileMount `yaml:"files" json:"-"`
	// SiteFiles : le service reçoit des fichiers déposés (champ files).
	SiteFiles bool `yaml:"-" json:"site_files"`
	// Web est faux pour un service sans page web (port absent) : rien n'est
	// publié par Traefik.
	Web     bool   `yaml:"-" json:"web"`
	Compose []byte `yaml:"-" json:"-"`
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

// labelLetters : lettres accentuées ou liées ramenées à des lettres simples
// par NormalizeLabel (« Été » donne « ete », « Œuvre » donne « oeuvre »).
var labelLetters = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ã", "a", "ä", "a", "å", "a", "æ", "ae",
	"ç", "c", "è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ñ", "n",
	"ò", "o", "ó", "o", "ô", "o", "õ", "o", "ö", "o", "ø", "o", "œ", "oe",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y", "ß", "ss",
)

// NormalizeLabel met un nom de service tapé librement au format de ValidLabel :
// minuscules, sans accents, chaque suite d'espaces ou d'autres signes devenue
// un seul tiret, sans tiret au début ni à la fin, 32 caractères au plus
// (« Mon blog » donne « mon-blog »). La console fait le même calcul pendant
// la saisie (normalizeServiceName, app.js), qui retire en plus les accents des
// lettres absentes de labelLetters ; ici, une telle lettre devient un tiret.
// Un nom qui reste
// invalide (vide, ou commençant par un chiffre) est refusé ensuite par ValidLabel.
func NormalizeLabel(name string) string {
	name = labelLetters.Replace(strings.ToLower(name))
	var out strings.Builder
	dash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && out.Len() > 0 {
				out.WriteByte('-')
			}
			out.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	result := out.String()
	if len(result) > 32 {
		result = strings.TrimRight(result[:32], "-")
	}
	return result
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
		if app.Tags == nil {
			return Catalog{}, fmt.Errorf("manifeste %s : %s", app.ID, strings.Join(checkTags(nil), " ; "))
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
	// Clé inconnue de x-pi-appliance refusée : une console plus ancienne que
	// le catalogue en ligne n'installe pas un manifeste qu'elle comprendrait mal.
	var strict struct {
		Meta  *App           `yaml:"x-pi-appliance"`
		Other map[string]any `yaml:",inline"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&strict); err != nil {
		return App{}, fmt.Errorf("manifeste %s : %w", id, err)
	}
	if file.Meta == nil {
		return App{}, fmt.Errorf("manifeste %s : section x-pi-appliance absente", id)
	}
	app := *file.Meta
	app.ID = id
	app.Web = app.Port != 0
	app.SiteFiles = len(app.Files) > 0
	app.Compose = withoutTags(data)
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
	if app.Tags != nil {
		problems = append(problems, checkTags(app.Tags)...)
	}
	if len(app.Icon) > 2000 || !iconPattern.MatchString(app.Icon) {
		fail("icon invalide (tracé SVG sur 24 × 24 : lettres de commande, chiffres, espaces, virgules, points, tirets)")
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
	switch {
	case app.Port == 0 && len(app.HostPorts) == 0:
		fail("port absent : un service sans page web déclare ses host_ports")
	case app.Port < 0 || app.Port > 65535:
		fail("port %d invalide", app.Port)
	}
	opened := map[HostPort]bool{}
	for _, port := range app.HostPorts {
		switch {
		case port.Port < 1 || port.Port > 65535:
			fail("host_ports : port %d invalide", port.Port)
		case reservedPorts[port.Port]:
			fail("host_ports : port %d réservé à Traefik et à la console", port.Port)
		case !protocols[port.Protocol]:
			fail("host_ports : protocole %q invalide (tcp ou udp)", port.Protocol)
		case opened[port]:
			fail("host_ports : port %d/%s en double", port.Port, port.Protocol)
		}
		opened[port] = true
	}
	for _, mount := range app.Files {
		if _, ok := file.Services[mount.Service]; !ok {
			fail("files : service %q absent de services", mount.Service)
		}
		if !path.IsAbs(mount.Path) || path.Clean(mount.Path) != mount.Path || mount.Path == "/" {
			fail("files : chemin %q invalide (chemin absolu, autre que /)", mount.Path)
		}
	}
	if app.MemoryMB <= 0 || app.DiskMB <= 0 {
		fail("memory_mb et disk_mb doivent être positifs")
	}
	if !logins[app.Login] {
		fail("login %q invalide (console ou application)", app.Login)
	}
	if !accesses[app.Access] {
		fail("access %q invalide (public, users, home ou admins)", app.Access)
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
