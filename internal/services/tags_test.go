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

package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEraseTags(t *testing.T) {
	m := &Manager{Dir: t.TempDir(), Domain: "maison.local", Runner: &fakeRunner{}}
	states := map[string]string{
		"perso": `{"name": "perso", "app": "custom", "address": "perso.maison.local", "tags": ["maison", "fichiers"], "mail": true}`,
		"wiki":  `{"name": "wiki", "app": "dokuwiki", "address": "wiki.maison.local"}`,
	}
	for name, state := range states {
		dir := filepath.Join(m.Dir, "services", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, stateName), []byte(state), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	erased, err := m.EraseTags()
	if err != nil || strings.Join(erased, ",") != "perso" {
		t.Fatalf("effacées %v %v", erased, err)
	}
	data, _ := os.ReadFile(filepath.Join(m.Dir, "services", "perso", stateName))
	if strings.Contains(string(data), "tags") || strings.Contains(string(data), "maison\"") || !strings.Contains(string(data), `"mail": true`) {
		t.Fatalf("instance.json %s", data)
	}
	if info, _ := os.Stat(filepath.Join(m.Dir, "services", "perso", stateName)); info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %v", info.Mode())
	}
	wiki, _ := os.ReadFile(filepath.Join(m.Dir, "services", "wiki", stateName))
	if string(wiki) != states["wiki"] {
		t.Fatalf("wiki réécrit : %s", wiki)
	}
	instances, err := m.List()
	if err != nil || len(instances) != 2 || !instances[0].Mail {
		t.Fatalf("%+v %v", instances, err)
	}
	if erased, err := m.EraseTags(); err != nil || len(erased) != 0 {
		t.Fatalf("seconde passe %v %v", erased, err)
	}
}
