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
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
