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
// NE PAS ajouter de clé au fichier Compose publié (compose) sans changer de
// format : les consoles déjà installées le relisent strictement et refusent
// une clé inconnue de x-pi-appliance. Une nouvelle information va à côté,
// dans les champs de l'application, que ces consoles ignorent (tags,
// local_certificate : withoutPublishedKeys).
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

// PublishableInFormat1 : faux pour un service sans page web dont l'accès n'est
// pas « public » (ports « maison seulement », TODO-4.8). Les consoles 0.2.7 et
// avant ignorent access sans page web et ouvriraient ses ports à tous : il
// n'est pas publié, les consoles qui le connaissent gardent leur version
// intégrée (spécification 6.4).
// NE PAS le publier tant qu'une console publiée l'installerait ports ouverts.
func PublishableInFormat1(app App) bool {
	return app.Port != 0 || app.Access == "" || app.Access == "public"
}

// Publish : le document publié pour un catalogue.
func Publish(catalog Catalog, planned []Planned) Document {
	apps := catalog.Apps()
	document := Document{Format: DocumentFormat, Apps: make([]PublishedApp, 0, len(apps)), Planned: planned, Tags: Tags}
	for _, app := range apps {
		if !PublishableInFormat1(app) {
			continue
		}
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
			ID      string   `json:"id"`
			Compose string   `json:"compose"`
			Tags    []string `json:"tags"`

			LocalCertificate *LocalCertificate `json:"local_certificate"`
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
		if err == nil && app.Tags == nil {
			app.Tags = entry.Tags
			if problems := checkTags(app.Tags); len(problems) > 0 {
				err = fmt.Errorf("manifeste %s : %s", entry.ID, strings.Join(problems, " ; "))
			}
		}
		if err == nil && app.LocalCertificate == nil && entry.LocalCertificate != nil {
			var file composeFile
			yaml.Unmarshal([]byte(entry.Compose), &file)
			if problems := checkLocalCertificate(entry.LocalCertificate, file); len(problems) > 0 {
				err = fmt.Errorf("manifeste %s : %s", entry.ID, strings.Join(problems, " ; "))
			}
			app.LocalCertificate = entry.LocalCertificate
		}
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
