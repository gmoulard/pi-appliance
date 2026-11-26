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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
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

type rollbackRunner struct {
	fakeRunner
	failRestart  bool
	failRollback bool
}

func (r *rollbackRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	output, err := r.fakeRunner.Run(ctx, dir, args...)
	call := strings.Join(args, " ")
	switch {
	case strings.HasSuffix(call, "images --format json"):
		return []byte(`[{"ID":"sha256:ancienne","Repository":"nginx","Tag":"latest"}]`), nil
	case strings.HasSuffix(call, "up --detach --wait") && r.failRestart:
		return []byte("le conteneur web s'arrête"), errors.New("exit status 1")
	case strings.HasSuffix(call, "up --detach --wait --force-recreate") && r.failRollback:
		return []byte("toujours pas"), errors.New("exit status 1")
	}
	return output, err
}

func TestRebuildRollback(t *testing.T) {
	runner := &rollbackRunner{}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner, MemoryMB: func() int { return 4096 }}
	installSimple(t, m)
	runner.calls = nil
	runner.failRestart = true
	_, err := m.Rebuild(context.Background(), "libre", false)
	if !errors.Is(err, ErrRebuildRolledBack) || !strings.Contains(err.Error(), "le conteneur web s'arrête") {
		t.Fatalf("erreur %v", err)
	}
	keep := "nginx:pi-rollback-libre-latest"
	order := []string{"image tag sha256:ancienne " + keep, " pull", " down", "image tag " + keep + " nginx:latest", "up --detach --wait --force-recreate", "image rm " + keep}
	next := 0
	for _, call := range runner.calls {
		if next < len(order) && strings.Contains(call, order[next]) {
			next++
		}
	}
	if next != len(order) {
		t.Fatalf("retour arrière incomplet (attendu %q) : %v", order[next], runner.calls)
	}
	if progress, _ := m.InstallProgress("libre"); progress.Error == "" || !progress.Finished {
		t.Fatalf("avancement %+v", progress)
	}
}

func TestRebuildRollbackFails(t *testing.T) {
	runner := &rollbackRunner{}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner, MemoryMB: func() int { return 4096 }}
	installSimple(t, m)
	runner.calls = nil
	runner.failRestart, runner.failRollback = true, true
	_, err := m.Rebuild(context.Background(), "libre", false)
	if err == nil || errors.Is(err, ErrRebuildRolledBack) || !strings.Contains(err.Error(), "non plus") {
		t.Fatalf("erreur %v", err)
	}
	if runner.called("image rm nginx:pi-rollback-libre-latest") {
		t.Fatalf("images d'avant retirées alors que le retour a raté : %v", runner.calls)
	}
}

func TestRebuildKeepsNoRollbackTag(t *testing.T) {
	runner := &rollbackRunner{}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner, MemoryMB: func() int { return 4096 }}
	installSimple(t, m)
	runner.calls = nil
	if _, err := m.Rebuild(context.Background(), "libre", false); err != nil {
		t.Fatal(err)
	}
	if !runner.called("image tag sha256:ancienne nginx:pi-rollback-libre-latest") || !runner.called("image rm nginx:pi-rollback-libre-latest") || runner.called("--force-recreate") {
		t.Fatalf("appels %v", runner.calls)
	}
}

func installHostMounts(t *testing.T, m *Manager, name string, sources ...string) {
	t.Helper()
	compose := "services:\n  web:\n    image: nginx:latest\n    volumes:\n"
	for i, source := range sources {
		compose += fmt.Sprintf("      - %s:/mnt/%d\n", source, i)
	}
	if _, err := m.InstallCustom(context.Background(), CustomRequest{Name: name, Compose: []byte(compose), Service: "web", Port: 80, ConfirmRisks: true}); err != nil {
		t.Fatal(err)
	}
}

func hostDir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{t.TempDir()}, parts...)...)
	if err := os.MkdirAll(filepath.Join(dir, "album"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "album", "photo.jpg"), []byte("photo"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRebuildCleanupHostDirs(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	photos := hostDir(t, "photos")
	link := filepath.Join(t.TempDir(), "lien")
	if err := os.Symlink(photos, link); err != nil {
		t.Fatal(err)
	}
	installHostMounts(t, m, "libre", photos, link, filepath.Join(t.TempDir(), "absent"))
	dirs, _, err := m.CleanupDirs("libre")
	if err != nil || len(dirs) != 1 || dirs[0] != photos {
		t.Fatalf("dossiers %v, %v", dirs, err)
	}
	if _, err := m.Rebuild(context.Background(), "libre", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(photos, "album", "photo.jpg")); err != nil {
		t.Fatalf("Rebuild sans cleanup a effacé : %v", err)
	}
	if _, err := m.Rebuild(context.Background(), "libre", true); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(photos)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dossier du Pi non vidé ou retiré : %v, %v", entries, err)
	}
}

func unixSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return filepath.Join(dir, "s.sock")
}

// Une prise montée (comme /var/run/docker.sock pour Portainer) est laissée
// telle quelle, et le reste du service est nettoyé (décision du 2026-11-26).
func TestRebuildCleanupKeepsSockets(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	photos := hostDir(t, "photos")
	socket := unixSocket(t)
	link := symlink(t, socket)
	installHostMounts(t, m, "libre", photos, socket, link)
	dirs, kept, err := m.CleanupDirs("libre")
	if err != nil || len(dirs) != 1 || dirs[0] != photos || len(kept) != 2 || !slices.Contains(kept, socket) || !slices.Contains(kept, link) {
		t.Fatalf("dossiers %v, prises %v, %v", dirs, kept, err)
	}
	if _, err := m.Rebuild(context.Background(), "libre", true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(socket); err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("prise touchée : %v", err)
	}
	if entries, err := os.ReadDir(photos); err != nil || len(entries) != 0 {
		t.Fatalf("dossier du Pi non vidé : %v, %v", entries, err)
	}
}

func TestRebuildCleanupRefused(t *testing.T) {
	cases := map[string]func(t *testing.T, m *Manager) string{
		"racine":                func(*testing.T, *Manager) string { return "/" },
		"etc":                   func(*testing.T, *Manager) string { return "/etc" },
		"sous etc":              func(*testing.T, *Manager) string { return "/etc/ssh" },
		"usr local":             func(*testing.T, *Manager) string { return "/usr/local" },
		"bin":                   func(*testing.T, *Manager) string { return "/bin" },
		"sbin":                  func(*testing.T, *Manager) string { return "/sbin" },
		"lib64":                 func(*testing.T, *Manager) string { return "/lib64" },
		"boot":                  func(*testing.T, *Manager) string { return "/boot/firmware" },
		"proc":                  func(*testing.T, *Manager) string { return "/proc" },
		"sys":                   func(*testing.T, *Manager) string { return "/sys" },
		"dev":                   func(*testing.T, *Manager) string { return "/dev" },
		"run":                   func(*testing.T, *Manager) string { return "/run" },
		"var":                   func(*testing.T, *Manager) string { return "/var" },
		"var log":               func(*testing.T, *Manager) string { return "/var/log" },
		"var lib":               func(*testing.T, *Manager) string { return "/var/lib" },
		"var lib docker":        func(*testing.T, *Manager) string { return "/var/lib/docker/volumes" },
		"home":                  func(*testing.T, *Manager) string { return "/home" },
		"home pi":               func(*testing.T, *Manager) string { return "/home/pi" },
		"root":                  func(*testing.T, *Manager) string { return "/root/notes" },
		"opt":                   func(*testing.T, *Manager) string { return "/opt" },
		"srv":                   func(*testing.T, *Manager) string { return "/srv/partage" },
		"mnt usb":               func(*testing.T, *Manager) string { return "/mnt/usb" },
		"media":                 func(*testing.T, *Manager) string { return "/media" },
		"données de pi":         func(_ *testing.T, m *Manager) string { return m.Dir },
		"parent des données":    func(_ *testing.T, m *Manager) string { return filepath.Dir(m.Dir) },
		"autre service":         func(_ *testing.T, m *Manager) string { return filepath.Join(m.Dir, "services", "autre") },
		"dossier du service":    func(_ *testing.T, m *Manager) string { return m.serviceDir("libre") },
		"réglages du service":   func(_ *testing.T, m *Manager) string { return filepath.Join(m.serviceDir("libre"), composeName) },
		"fichier":               func(t *testing.T, _ *Manager) string { return filepath.Join(hostDir(t), "album", "photo.jpg") },
		"lien vers etc":         func(t *testing.T, _ *Manager) string { return symlink(t, "/etc") },
		"lien vers les données": func(t *testing.T, m *Manager) string { return symlink(t, m.Dir) },
		"monté par un autre": func(t *testing.T, m *Manager) string {
			shared := hostDir(t, "partage")
			installHostMounts(t, m, "autre", shared)
			return shared
		},
		"dans le dossier d'un autre": func(t *testing.T, m *Manager) string {
			shared := hostDir(t, "partage")
			installHostMounts(t, m, "autre", shared)
			return filepath.Join(shared, "album")
		},
		"réglage avancé d'un autre": func(t *testing.T, m *Manager) string {
			shared := hostDir(t, "partage")
			installHostMounts(t, m, "autre")
			if err := writeJSON(filepath.Join(m.serviceDir("autre"), advancedSettings), Advanced{Service: "web", Volumes: []Mount{{Source: filepath.Dir(shared), Target: "/import"}}}, 0o600); err != nil {
				t.Fatal(err)
			}
			return shared
		},
	}
	for label, bad := range cases {
		t.Run(label, func(t *testing.T) {
			runner := &fakeRunner{}
			m := manager(t, runner)
			if err := os.MkdirAll(filepath.Join(m.Dir, "services", "autre"), 0o700); err != nil {
				t.Fatal(err)
			}
			good := hostDir(t, "photos")
			installHostMounts(t, m, "libre", good, bad(t, m))
			runner.calls = nil
			if _, _, err := m.CleanupDirs("libre"); !errors.Is(err, ErrInvalid) {
				t.Fatalf("liste acceptée : %v", err)
			}
			_, err := m.Rebuild(context.Background(), "libre", true)
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "rien n'a été effacé") {
				t.Fatalf("erreur %v", err)
			}
			if runner.called("pull") || runner.called("down") {
				t.Fatalf("service touché : %v", runner.calls)
			}
			if _, err := os.Stat(filepath.Join(good, "album", "photo.jpg")); err != nil {
				t.Fatalf("effacé malgré le refus : %v", err)
			}
		})
	}
}

func symlink(t *testing.T, target string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "lien")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	return link
}
