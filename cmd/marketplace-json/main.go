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

// marketplace-json écrit le catalogue publié (docs/static) à partir des
// manifestes de marketplace/ et de marketplace/planned.yaml. Le site et les
// consoles le lisent ; un test vérifie qu'il est à jour.
package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const indexName = "pi-appliance-catalog.json"

var catalogName = regexp.MustCompile(`^pi-appliance-[0-9a-f]{32}\.json$`)

type catalogIndex struct {
	File string `json:"file"`
}

func main() {
	if err := generate(filepath.Join("docs", "static")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// render : nom du fichier du catalogue (MD5 de son contenu), son contenu et l'index.
func render() (string, []byte, []byte, error) {
	catalog, err := marketplace.Builtin()
	if err != nil {
		return "", nil, nil, fmt.Errorf("lecture du catalogue : %w", err)
	}
	planned, err := marketplace.BuiltinPlanned(catalog)
	if err != nil {
		return "", nil, nil, err
	}
	data, err := json.MarshalIndent(marketplace.Publish(catalog, planned), "", "  ")
	if err != nil {
		return "", nil, nil, fmt.Errorf("encodage du catalogue : %w", err)
	}
	data = append(data, '\n')
	filename := fmt.Sprintf("pi-appliance-%x.json", md5.Sum(data))
	index, err := json.MarshalIndent(catalogIndex{File: filename}, "", "  ")
	if err != nil {
		return "", nil, nil, fmt.Errorf("encodage de l'index du catalogue : %w", err)
	}
	return filename, data, append(index, '\n'), nil
}

func generate(directory string) error {
	filename, data, index, err := render()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("création de %s : %w", directory, err)
	}
	if err := os.WriteFile(filepath.Join(directory, filename), data, 0o644); err != nil {
		return fmt.Errorf("écriture du catalogue : %w", err)
	}
	if err := os.WriteFile(filepath.Join(directory, indexName), index, 0o644); err != nil {
		return fmt.Errorf("écriture de l'index du catalogue : %w", err)
	}
	// Les anciennes versions du catalogue ne sont plus lues par personne.
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if catalogName.MatchString(entry.Name()) && entry.Name() != filename {
			if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil {
				return err
			}
		}
	}
	fmt.Printf("Catalogue écrit dans %s\n", filepath.Join(directory, filename))
	return nil
}
