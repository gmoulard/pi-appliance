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
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// DocumentFormat : version du catalogue publié (docs/static), lu par le site
// et par les consoles. Une console refuse un autre format.
const DocumentFormat = 1

//go:embed planned.yaml
var plannedFile []byte

var todoPattern = regexp.MustCompile(`^TODO-[0-9]+\.[0-9]+$`)

// Planned : application prévue, pas encore installable.
type Planned struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Icon        string   `yaml:"icon" json:"icon"`
	Category    string   `yaml:"category" json:"category"`
	Tags        []string `yaml:"tags" json:"tags"`
	TODO        string   `yaml:"todo" json:"todo"`
}

// PublishedApp : une application du catalogue publié, avec son manifeste.
type PublishedApp struct {
	App
	Compose string `json:"compose"`
}

// Document : le catalogue publié sur le site et lu en ligne par les consoles.
type Document struct {
	Format  int            `json:"format"`
	Apps    []PublishedApp `json:"apps"`
	Planned []Planned      `json:"planned"`
	Tags    []Tag          `json:"tags"`
}

// BuiltinPlanned : les applications prévues (planned.yaml), vérifiées contre
// le catalogue intégré.
func BuiltinPlanned(catalog Catalog) ([]Planned, error) {
	var planned []Planned
	decoder := yaml.NewDecoder(strings.NewReader(string(plannedFile)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&planned); err != nil {
		return nil, fmt.Errorf("planned.yaml : %w", err)
	}
	seen := map[string]bool{}
	for _, p := range planned {
		_, installable := catalog.Get(p.ID)
		switch {
		case !ValidLabel(p.ID), seen[p.ID]:
			return nil, fmt.Errorf("planned.yaml : identifiant %q invalide ou en double", p.ID)
		case installable:
			return nil, fmt.Errorf("planned.yaml : %s est déjà dans le catalogue", p.ID)
		case strings.TrimSpace(p.Name) == "", strings.TrimSpace(p.Description) == "", strings.TrimSpace(p.Category) == "":
			return nil, fmt.Errorf("planned.yaml : %s incomplet", p.ID)
		case len(p.Icon) > 2000 || !iconPattern.MatchString(p.Icon):
			return nil, fmt.Errorf("planned.yaml : icône de %s invalide", p.ID)
		case len(checkTags(p.Tags)) > 0:
			return nil, fmt.Errorf("planned.yaml : %s : %s", p.ID, strings.Join(checkTags(p.Tags), " ; "))
		case !todoPattern.MatchString(p.TODO):
			return nil, fmt.Errorf("planned.yaml : todo de %s invalide (TODO-x.y)", p.ID)
		}
		seen[p.ID] = true
	}
	return planned, nil
}

// Publish : le document publié pour un catalogue.
func Publish(catalog Catalog, planned []Planned) Document {
	apps := catalog.Apps()
	document := Document{Format: DocumentFormat, Apps: make([]PublishedApp, 0, len(apps)), Planned: planned, Tags: Tags}
	for _, app := range apps {
		document.Apps = append(document.Apps, PublishedApp{App: app, Compose: string(app.Compose)})
	}
	return document
}

// Manifest : un manifeste lu dans un catalogue publié, ou l'erreur qui le fait
// refuser par cette console.
type Manifest struct {
	ID  string
	App App
	Err error
}

// ReadDocument lit un catalogue publié. Chaque manifeste est relu et vérifié
// à partir de son fichier Compose seul : les autres champs du document ne
// comptent pas.
func ReadDocument(data []byte) ([]Manifest, error) {
	var document struct {
		Format int `json:"format"`
		Apps   []struct {
			ID      string `json:"id"`
			Compose string `json:"compose"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("catalogue illisible : %w", err)
	}
	if document.Format != DocumentFormat {
		return nil, fmt.Errorf("catalogue au format %d, cette console lit le format %d", document.Format, DocumentFormat)
	}
	if len(document.Apps) == 0 {
		return nil, errors.New("catalogue vide")
	}
	manifests := make([]Manifest, 0, len(document.Apps))
	seen := map[string]bool{}
	for _, entry := range document.Apps {
		if seen[entry.ID] {
			return nil, fmt.Errorf("application %q en double", entry.ID)
		}
		seen[entry.ID] = true
		app, err := Parse(entry.ID, []byte(entry.Compose))
		manifests = append(manifests, Manifest{ID: entry.ID, App: app, Err: err})
	}
	return manifests, nil
}

// NewCatalog : un catalogue fait des applications données (une par identifiant).
func NewCatalog(apps []App) Catalog {
	catalog := Catalog{apps: map[string]App{}}
	for _, app := range apps {
		catalog.apps[app.ID] = app
	}
	return catalog
}
