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
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const simple = `services:
  web:
    image: nginx:latest
    volumes:
      - ./data:/data
      - site:/site
volumes:
  site: {}
`

func installSimple(t *testing.T, m *Manager) string {
	t.Helper()
	if _, err := m.InstallCustom(context.Background(), CustomRequest{Name: "libre", Compose: []byte(simple), Service: "web", Port: 80, ConfirmRisks: true}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Dir, "services", "libre")
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "notes.txt"), []byte("à garder ?"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func steps(calls []string) string {
	var list []string
	for _, call := range calls {
		for _, step := range []string{" pull", " down", " down --volumes", " create", " start", " up --detach --wait"} {
			if strings.HasSuffix(call, step) {
				list = append(list, strings.TrimSpace(step))
			}
		}
	}
	return strings.Join(list, ",")
}

func TestRebuildKeepsData(t *testing.T) {
	runner := &imageRunner{images: "nginx:latest\n"}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner}
	dir := installSimple(t, m)
	puller := &fakePuller{manager: m, present: map[string]bool{"nginx:latest": true}}
	m.Images = puller
	runner.calls = nil
	if _, err := m.Rebuild(context.Background(), "libre", false); err != nil {
		t.Fatal(err)
	}
	if len(puller.pulled) != 1 || puller.pulled[0] != "nginx:latest" {
		t.Fatalf("image déjà sur le Pi non retéléchargée : %v", puller.pulled)
	}
	if got := steps(runner.calls); got != "down,create,start,up --detach --wait" {
		t.Fatalf("étapes %s (%v)", got, runner.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "notes.txt")); err != nil {
		t.Fatalf("données perdues : %v", err)
	}
	if progress, ok := m.InstallProgress("libre"); !ok || progress.Step != StepDone || !progress.Finished {
		t.Fatalf("avancement %+v", progress)
	}
}

func TestRebuildCleanup(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	dir := installSimple(t, m)
	server := filepath.Join(m.Dir, "services", "pg")
	if err := os.MkdirAll(server, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(server, stateName), Instance{Name: "pg", App: PostgresApp}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.changeInstance("libre", func(instance *Instance) error {
		instance.Database = &Database{Server: "pg", Name: "pi_libre"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	if _, err := m.Rebuild(context.Background(), "libre", true); err != nil {
		t.Fatal(err)
	}
	if got := steps(runner.calls); got != "pull,down --volumes,create,start,up --detach --wait" {
		t.Fatalf("étapes %s (%v)", got, runner.calls)
	}
	if !runner.called("DROP DATABASE IF EXISTS pi_libre WITH (FORCE)") || !runner.called("CREATE DATABASE pi_libre OWNER pi_libre") || runner.called("DROP ROLE") {
		t.Fatalf("base non vidée : %v", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "data")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dossier de données gardé : %v", err)
	}
	for _, kept := range []string{composeName, overrideName, envName, stateName} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Fatalf("réglage %s effacé : %v", kept, err)
		}
	}
}

func TestRebuildWithoutNetwork(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	dir := installSimple(t, m)
	runner.calls = nil
	runner.fail = map[string]bool{" pull": true}
	_, err := m.Rebuild(context.Background(), "libre", true)
	if err == nil || !strings.Contains(err.Error(), "pas été touché") {
		t.Fatalf("erreur %v", err)
	}
	if runner.called("down") {
		t.Fatalf("service arrêté sans nouvelles images : %v", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "notes.txt")); err != nil {
		t.Fatalf("données effacées : %v", err)
	}
	if progress, _ := m.InstallProgress("libre"); progress.Step != StepDownload || progress.Error == "" {
		t.Fatalf("avancement %+v", progress)
	}
}

func TestRebuildCatalogRefused(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Rebuild(context.Background(), "cloud", false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("service du catalogue : %v", err)
	}
	if _, err := m.Rebuild(context.Background(), "absent", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service absent : %v", err)
	}
}
