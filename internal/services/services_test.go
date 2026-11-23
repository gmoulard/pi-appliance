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

package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

type fakeRunner struct {
	calls []string
	fail  map[string]bool
}

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	for part := range f.fail {
		if strings.Contains(call, part) {
			return []byte("ligne 1\nerreur docker"), errors.New("exit status 1")
		}
	}
	return nil, nil
}

func (f *fakeRunner) called(part string) bool {
	for _, call := range f.calls {
		if strings.Contains(call, part) {
			return true
		}
	}
	return false
}

func nextcloud(t *testing.T) marketplace.App {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, _ := catalog.Get("nextcloud")
	return app
}

func manager(t *testing.T, runner *fakeRunner) *Manager {
	t.Helper()
	return &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner, MemoryMB: func() int { return 4096 }}
}

func TestInstall(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	result, err := m.Install(context.Background(), nextcloud(t), "cloud", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.Address != "cloud.maison.example.org" || result.Instance.App != "nextcloud" {
		t.Fatalf("instance %+v", result.Instance)
	}
	if len(result.Secrets) != 1 || len(result.Secrets["Mot de passe de l'administrateur de Nextcloud"]) != 24 {
		t.Fatalf("secrets %v", result.Secrets)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("avertissements %v", result.Warnings)
	}
	dir := filepath.Join(m.Dir, "services", "cloud")
	env, err := readEnv(filepath.Join(dir, envName))
	if err != nil {
		t.Fatal(err)
	}
	if env["PI_HOST"] != "cloud.maison.example.org" || env["PI_INSTANCE"] != "cloud" || len(env["DB_PASSWORD"]) != 24 || env["DB_PASSWORD"] == env["DB_ROOT_PASSWORD"] {
		t.Fatalf("env %v", env)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, envName))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("droits de .env : %v %v", info.Mode(), err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, overrideName))
	if err != nil {
		t.Fatal(err)
	}
	var generated struct {
		Services map[string]struct {
			Labels   map[string]string `json:"labels"`
			Networks []string          `json:"networks"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &generated); err != nil {
		t.Fatal(err)
	}
	labels := generated.Services["app"].Labels
	if labels["traefik.http.routers.pi-cloud.rule"] != "Host(`cloud.maison.example.org`)" || labels["traefik.http.services.pi-cloud.loadbalancer.server.port"] != "80" {
		t.Fatalf("labels %v", labels)
	}
	if labels["traefik.http.routers.pi-cloud.tls.certresolver"] != "letsencrypt" {
		t.Fatalf("résolveur de certificat absent des labels : %v", labels)
	}
	if !runner.called("compose --project-name pi-cloud --env-file .env --file compose.yaml --file pi-appliance.override.json up --detach --wait") {
		t.Fatalf("appels %v", runner.calls)
	}
	if runner.called("network create") {
		t.Fatal("réseau recréé alors qu'il existe")
	}
	instances, err := m.List()
	if err != nil || len(instances) != 1 || instances[0].Name != "cloud" {
		t.Fatalf("liste %v %v", instances, err)
	}
}

func TestLocalDomainDoesNotUseLetsEncrypt(t *testing.T) {
	for _, domain := range []string{"", "pi-appliance.local", "localhost", "192.168.1.20"} {
		manager := &Manager{Domain: domain}
		if manager.usesLetsEncrypt() {
			t.Errorf("le domaine local %q ne doit pas demander de certificat public", domain)
		}
	}
	if !(&Manager{Domain: "maison.pi-appliance.com"}).usesLetsEncrypt() {
		t.Fatal("un domaine public doit utiliser Let's Encrypt")
	}
}

func TestInstallCreatesNetwork(t *testing.T) {
	runner := &fakeRunner{fail: map[string]bool{"network inspect": true}}
	if _, err := manager(t, runner).Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if !runner.called("network create pi-appliance") {
		t.Fatalf("appels %v", runner.calls)
	}
}

func TestInstallTwice(t *testing.T) {
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); !errors.Is(err, ErrExists) {
		t.Fatalf("erreur %v", err)
	}
	if _, err := m.Install(context.Background(), nextcloud(t), "famille", nil); err != nil {
		t.Fatalf("seconde instance : %v", err)
	}
}

func TestInstallRejects(t *testing.T) {
	m := manager(t, &fakeRunner{})
	cases := map[string]struct {
		name   string
		values map[string]string
	}{
		"nom":               {"Cloud", nil},
		"nom réservé":       {"pi-appliance", nil},
		"chemin de Cockpit": {"cockpit", nil},
		"paramètre inconnu": {"cloud", map[string]string{"OTHER": "x"}},
		"retour à la ligne": {"cloud", map[string]string{"NEXTCLOUD_ADMIN_PASSWORD": "a\nb"}},
		"apostrophe":        {"cloud", map[string]string{"NEXTCLOUD_ADMIN_PASSWORD": "l'été"}},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			if _, err := m.Install(context.Background(), nextcloud(t), c.name, c.values); !errors.Is(err, ErrInvalid) {
				t.Fatalf("erreur %v", err)
			}
		})
	}
}

func TestInstallRequiredParameter(t *testing.T) {
	app := nextcloud(t)
	app.Parameters = []marketplace.Parameter{{Name: "TITLE", Label: "Titre"}}
	m := manager(t, &fakeRunner{})
	if _, err := m.Install(context.Background(), app, "cloud", nil); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Titre") {
		t.Fatalf("erreur %v", err)
	}
	if _, err := m.Install(context.Background(), app, "cloud", map[string]string{"TITLE": "Famille"}); err != nil {
		t.Fatal(err)
	}
}

func TestInstallMemoryWarning(t *testing.T) {
	m := manager(t, &fakeRunner{})
	m.MemoryMB = func() int { return 512 }
	result, err := m.Install(context.Background(), nextcloud(t), "cloud", nil)
	if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "512 Mo") {
		t.Fatalf("résultat %+v, erreur %v", result, err)
	}
}

func TestInstallFailureStopsService(t *testing.T) {
	runner := &fakeRunner{fail: map[string]bool{"up --detach --wait": true}}
	m := manager(t, runner)
	_, err := m.Install(context.Background(), nextcloud(t), "cloud", nil)
	if err == nil || !strings.Contains(err.Error(), "erreur docker") {
		t.Fatalf("erreur %v", err)
	}
	if !runner.called("down") {
		t.Fatalf("appels %v", runner.calls)
	}
	if instances, _ := m.List(); len(instances) != 0 {
		t.Fatalf("service compté comme installé : %v", instances)
	}
}

func TestRemoveKeepsData(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	before, _ := readEnv(filepath.Join(m.Dir, "services", "cloud", envName))
	if err := m.Remove(context.Background(), "cloud", true); err != nil {
		t.Fatal(err)
	}
	if runner.called("--volumes") {
		t.Fatal("volumes supprimés alors qu'il faut les garder")
	}
	result, err := m.Install(context.Background(), nextcloud(t), "cloud", nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := readEnv(filepath.Join(m.Dir, "services", "cloud", envName))
	if before["DB_PASSWORD"] != after["DB_PASSWORD"] || len(result.Secrets) != 0 {
		t.Fatalf("secrets non réutilisés : %v / %v, %v", before, after, result.Secrets)
	}
}

func TestRemoveDeletesData(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(context.Background(), "cloud", false); err != nil {
		t.Fatal(err)
	}
	if !runner.called("down --volumes") {
		t.Fatalf("appels %v", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "services", "cloud")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dossier encore là : %v", err)
	}
}

func TestRemoveUnknown(t *testing.T) {
	m := manager(t, &fakeRunner{})
	for _, name := range []string{"cloud", "../etc"} {
		if err := m.Remove(context.Background(), name, true); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s : erreur %v", name, err)
		}
	}
}

func TestRemoveFailure(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	runner.fail = map[string]bool{" down": true}
	if err := m.Remove(context.Background(), "cloud", false); err == nil {
		t.Fatal("échec de docker ignoré")
	}
	if instances, _ := m.List(); len(instances) != 1 {
		t.Fatalf("service perdu : %v", instances)
	}
}

func TestRenamed(t *testing.T) {
	fixed := &Manager{Domain: "maison.example.org"}
	fixed.Renamed("controltower")
	if got := fixed.CurrentDomain(); got != "maison.example.org" {
		t.Fatalf("adresse donnée par -domain changée : %s", got)
	}
	m := &Manager{Domain: "pi-appliance-1a2b.local", FollowName: true}
	m.Renamed("-mauvais-")
	if got := m.CurrentDomain(); got != "pi-appliance-1a2b.local" {
		t.Fatalf("nom invalide accepté : %s", got)
	}
	m.Renamed("controltower")
	if got := m.CurrentDomain(); got != "controltower.local" {
		t.Fatalf("adresse après renommage : %s", got)
	}
}
