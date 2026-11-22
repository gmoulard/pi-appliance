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

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Le catalogue publié (docs/static) suit les manifestes : sinon, lancer
// « go run ./cmd/marketplace-json » depuis la racine du dépôt.
func TestPublishedCatalogUpToDate(t *testing.T) {
	filename, data, index, err := render()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join("..", "..", "docs", "static")
	for name, want := range map[string][]byte{indexName: index, filename: data} {
		got, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("docs/static/%s n'est pas à jour : lancez « go run ./cmd/marketplace-json » depuis la racine du dépôt", name)
		}
	}
}

func TestGenerateRemovesOldCatalogs(t *testing.T) {
	directory := t.TempDir()
	old := filepath.Join(directory, "pi-appliance-0123456789abcdef0123456789abcdef.json")
	other := filepath.Join(directory, "autre.json")
	for _, path := range []string{old, other} {
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := generate(directory); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("ancien catalogue gardé")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("fichier sans rapport supprimé")
	}
	filename, _, _, _ := render()
	if _, err := os.Stat(filepath.Join(directory, filename)); err != nil {
		t.Error(err)
	}
}

// legacyApp : clés de x-pi-appliance connues des consoles déjà installées
// (0.1.0 à 0.1.3, marketplace.App sans tags), qui relisent strictement le
// fichier Compose de chaque application du catalogue en ligne.
type legacyApp struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Icon        string `yaml:"icon"`
	Category    string `yaml:"category"`
	License     string `yaml:"license"`
	Subdomain   string `yaml:"subdomain"`
	Service     string `yaml:"service"`
	Port        int    `yaml:"port"`
	MemoryMB    int    `yaml:"memory_mb"`
	DiskMB      int    `yaml:"disk_mb"`
	Login       string `yaml:"login"`
	Access      string `yaml:"access"`
	Parameters  []struct {
		Name     string `yaml:"name"`
		Label    string `yaml:"label"`
		Generate bool   `yaml:"generate"`
		Default  string `yaml:"default"`
	} `yaml:"parameters"`
	HostPorts []struct {
		Port     int    `yaml:"port"`
		Protocol string `yaml:"protocol"`
	} `yaml:"host_ports"`
	Files []struct {
		Service string `yaml:"service"`
		Path    string `yaml:"path"`
	} `yaml:"files"`
}

// Le catalogue publié reste lisible par les consoles déjà installées : format
// 1 et aucune clé inconnue d'elles dans les fichiers Compose (spécification 6.5).
func TestPublishedCatalogReadableByInstalledConsoles(t *testing.T) {
	_, data, _, err := render()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Format int `json:"format"`
		Apps   []struct {
			ID      string   `json:"id"`
			Compose string   `json:"compose"`
			Tags    []string `json:"tags"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Format != 1 {
		t.Fatalf("format %d : les consoles installées ne lisent que le format 1", document.Format)
	}
	for _, app := range document.Apps {
		var strict struct {
			Meta  *legacyApp     `yaml:"x-pi-appliance"`
			Other map[string]any `yaml:",inline"`
		}
		decoder := yaml.NewDecoder(strings.NewReader(app.Compose))
		decoder.KnownFields(true)
		if err := decoder.Decode(&strict); err != nil || strict.Meta == nil {
			t.Errorf("%s refusé par une console 0.1.x : %v", app.ID, err)
		}
		if len(app.Tags) == 0 {
			t.Errorf("%s sans étiquettes dans le catalogue publié", app.ID)
		}
	}
}
