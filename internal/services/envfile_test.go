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

const meteoEnv = "# codes de NautiLink\r\nMETEO_FRANCE_KEY=abc'def\"x$y\r\n\r\n  TZ=Europe/Paris\r\n"

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func installWithEnv(t *testing.T, m *Manager, env string) string {
	t.Helper()
	if _, err := m.InstallCustom(context.Background(), CustomRequest{Name: "libre", Compose: []byte(simple), Service: "web", Port: 80, ConfirmRisks: true, Env: env}); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(m.Dir, "services", "libre")
}

func TestNormalizeAdminEnv(t *testing.T) {
	content, err := normalizeAdminEnv(meteoEnv)
	if err != nil {
		t.Fatal(err)
	}
	if content != "# codes de NautiLink\nMETEO_FRANCE_KEY=abc'def\"x$y\n\nTZ=Europe/Paris\n" {
		t.Fatalf("%q", content)
	}
	for _, empty := range []string{"", "\n\n", "# rien\n"} {
		if content, err := normalizeAdminEnv(empty); err != nil || content != "" {
			t.Fatalf("%q : %q %v", empty, content, err)
		}
	}
	for _, bad := range []string{"PI_HOST=x", "PI_INSTANCE=x", "PI_SMTP_HOST=x", "PI_DATABASE_PASSWORD=x", "A=1\nA=2", "export A=1", "1A=x", "sans égal", "A B=1", strings.Repeat("A=1\n", maxAdminLines+1)} {
		if _, err := normalizeAdminEnv(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%q accepté : %v", bad, err)
		}
	}
	if _, err := normalizeAdminEnv("PI_OTHER=1\n"); err != nil {
		t.Fatalf("PI_OTHER refusé : %v", err)
	}
}

func TestInstallCustomEnv(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.InstallCustom(context.Background(), CustomRequest{Name: "libre", Compose: []byte(simple), Service: "web", Port: 80, ConfirmRisks: true, Env: "PI_HOST=ailleurs"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("variable de la console acceptée : %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "services", "libre")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("service préparé malgré le refus : %v", err)
	}
	dir := installWithEnv(t, m, meteoEnv)
	env := readFile(t, filepath.Join(dir, envName))
	if !strings.HasPrefix(env, "PI_HOST='libre.maison.example.org'\nPI_INSTANCE='libre'\n"+adminEnvMark+"\n# codes de NautiLink\nMETEO_FRANCE_KEY=abc'def\"x$y\n") {
		t.Fatalf(".env : %q", env)
	}
	values, err := readEnv(filepath.Join(dir, envName))
	if err != nil || len(values) != 2 || values["PI_INSTANCE"] != "libre" {
		t.Fatalf("lignes de la console : %v %v", values, err)
	}
	if err := writeEnv(dir, values); err != nil {
		t.Fatal(err)
	}
	if again := readFile(t, filepath.Join(dir, envName)); again != env {
		t.Fatalf(".env réécrit autrement : %q", again)
	}
	source, err := m.ComposeSource("libre")
	if err != nil || !strings.Contains(source.Env, "METEO_FRANCE_KEY") {
		t.Fatalf("source : %+v %v", source, err)
	}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnvFile("cloud"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("service du catalogue : %v", err)
	}
}

func TestKeptEnvFile(t *testing.T) {
	m := manager(t, &fakeRunner{})
	installWithEnv(t, m, "A=1\n")
	if env, err := m.KeptEnvFile("libre"); err != nil || env != "" {
		t.Fatalf("service installé : %q %v", env, err)
	}
	if err := m.Remove(context.Background(), "libre", true); err != nil {
		t.Fatal(err)
	}
	if env, err := m.KeptEnvFile("libre"); err != nil || env != "A=1\n" {
		t.Fatalf("données gardées : %q %v", env, err)
	}
	installWithEnv(t, m, "")
	if err := m.Remove(context.Background(), "libre", false); err != nil {
		t.Fatal(err)
	}
	if env, err := m.KeptEnvFile("libre"); err != nil || env != "" {
		t.Fatalf("champ vidé à la réinstallation, puis données supprimées : %q %v", env, err)
	}
	if env, err := m.KeptEnvFile("../x"); err != nil || env != "" {
		t.Fatalf("nom invalide : %q %v", env, err)
	}
}

func TestSetEnvFile(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	dir := installWithEnv(t, m, "A=1\n")
	if err := m.SetEnvFile(context.Background(), "libre", "B=2\n"); err != nil {
		t.Fatal(err)
	}
	env := readFile(t, filepath.Join(dir, envName))
	if strings.Contains(env, "A=1") || !strings.HasSuffix(env, adminEnvMark+"\nB=2\n") {
		t.Fatalf(".env : %q", env)
	}
	if !runner.called("up --detach --wait --remove-orphans") {
		t.Fatal("service pas redémarré")
	}
	if err := m.SetEnvFile(context.Background(), "libre", "PI_DATABASE_URL=x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("variable de la console : %v", err)
	}
	before := readFile(t, filepath.Join(dir, envName))
	runner.fail = map[string]bool{"up --detach --wait --remove-orphans": true}
	if err := m.SetEnvFile(context.Background(), "libre", "C=3\n"); err == nil || errors.Is(err, ErrEnvRestored) {
		t.Fatalf("retour raté non dit : %v", err)
	}
	if got := readFile(t, filepath.Join(dir, envName)); got != before {
		t.Fatalf("ancien .env pas remis : %q", got)
	}
	calls := 0
	m.Runner = runnerFunc(func(args ...string) error {
		if strings.Contains(strings.Join(args, " "), "remove-orphans") {
			calls++
			if calls == 1 {
				return errors.New("ne démarre pas")
			}
		}
		return nil
	})
	if err := m.SetEnvFile(context.Background(), "libre", ""); !errors.Is(err, ErrEnvRestored) {
		t.Fatalf("retour réussi : %v", err)
	}
	if got := readFile(t, filepath.Join(dir, envName)); got != before {
		t.Fatalf("ancien .env pas remis : %q", got)
	}
	if got := readFile(t, filepath.Join(dir, adminEnvName)); got != "B=2\n" {
		t.Fatalf("ancien admin.env pas remis : %q", got)
	}
}

func TestRebuildEnv(t *testing.T) {
	m := manager(t, &fakeRunner{})
	dir := installWithEnv(t, m, "A=1\n")
	for _, step := range []struct {
		cleanup, dropEnv bool
		kept             bool
	}{{false, true, true}, {true, false, true}, {true, true, false}} {
		if _, err := m.Rebuild(context.Background(), "libre", step.cleanup, step.dropEnv); err != nil {
			t.Fatal(err)
		}
		env := readFile(t, filepath.Join(dir, envName))
		if strings.Contains(env, "A=1") != step.kept || !strings.Contains(env, "PI_INSTANCE='libre'") {
			t.Fatalf("%+v : %q", step, env)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, adminEnvName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("admin.env gardé : %v", err)
	}
}

type runnerFunc func(args ...string) error

func (f runnerFunc) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	return nil, f(args...)
}
