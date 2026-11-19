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

package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gmoulard/pi-appliance/marketplace"
)

type catalogDocument struct {
	Apps []marketplace.App `json:"apps"`
}

type catalogIndex struct {
	File string `json:"file"`
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	catalog, err := marketplace.Builtin()
	if err != nil {
		return fmt.Errorf("lecture du catalogue : %w", err)
	}
	data, err := json.MarshalIndent(catalogDocument{Apps: catalog.Apps()}, "", "  ")
	if err != nil {
		return fmt.Errorf("encodage du catalogue : %w", err)
	}
	data = append(data, '\n')

	filename := fmt.Sprintf("pi-appliance-%x.json", md5.Sum(data))
	directory := filepath.Join("docs", "static")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("création de %s : %w", directory, err)
	}
	if err := os.WriteFile(filepath.Join(directory, filename), data, 0o644); err != nil {
		return fmt.Errorf("écriture du catalogue : %w", err)
	}

	index, err := json.MarshalIndent(catalogIndex{File: filename}, "", "  ")
	if err != nil {
		return fmt.Errorf("encodage de l'index du catalogue : %w", err)
	}
	index = append(index, '\n')
	if err := os.WriteFile(filepath.Join(directory, "pi-appliance-catalog.json"), index, 0o644); err != nil {
		return fmt.Errorf("écriture de l'index du catalogue : %w", err)
	}
	fmt.Printf("Catalogue écrit dans %s\n", filepath.Join(directory, filename))
	return nil
}
