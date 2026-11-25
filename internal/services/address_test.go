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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

func TestServiceAddress(t *testing.T) {
	for _, test := range []struct{ name, domain, want string }{
		{"www", "maison.local", "www-maison.local"},
		{"www", "pi-appliance-1a2b.local", "www-pi-appliance-1a2b.local"},
		{"www", "maison.pi-appliance.com", "www.maison.pi-appliance.com"},
		{"www", "maison.duckdns.org", "www.maison.duckdns.org"},
	} {
		if got := ServiceAddress(test.name, test.domain); got != test.want {
			t.Errorf("ServiceAddress(%q, %q) = %q", test.name, test.domain, got)
		}
	}
}

func app(t *testing.T, id string) marketplace.App {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	found, ok := catalog.Get(id)
	if !ok {
		t.Fatalf("%s absent du catalogue", id)
	}
	return found
}

func labelsOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, overrideName))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Services map[string]struct {
			Labels map[string]string `json:"labels"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	for _, service := range file.Services {
		if service.Labels != nil {
			return service.Labels
		}
	}
	return nil
}

func TestInstallLocalAddress(t *testing.T) {
	runner := &fakeRunner{}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.local", Runner: runner}
	result, err := m.Install(context.Background(), app(t, "site-web"), "www", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.Address != "www-maison.local" {
		t.Fatalf("adresse %s", result.Instance.Address)
	}
	if rule := labelsOf(t, m.serviceDir("www"))["traefik.http.routers.pi-www.rule"]; rule != "Host(`www-maison.local`)" {
		t.Fatalf("règle %s", rule)
	}
	long := strings.Repeat("a", 32)
	m.Domain = strings.Repeat("b", 31) + ".local"
	if _, err := m.Install(context.Background(), app(t, "site-web"), long, nil); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "31 caractères au plus") {
		t.Fatalf("adresse de plus de 63 caractères : %v", err)
	}
}

// oldInstall : service installé avant TODO-2.61, à l'adresse www.maison.local.
func oldInstall(t *testing.T, m *Manager, id, name string) string {
	t.Helper()
	domain := m.Domain
	m.Domain = "maison.example"
	if _, err := m.Install(context.Background(), app(t, id), name, nil); err != nil {
		t.Fatal(err)
	}
	m.Domain = domain
	dir := m.serviceDir(name)
	for _, file := range []string{envName, overrideName, stateName} {
		data, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(strings.ReplaceAll(string(data), name+".maison.example", name+".maison.local")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestRelocate(t *testing.T) {
	runner := &fakeRunner{}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.local", Runner: runner}
	www := oldInstall(t, m, "site-web", "www")
	cloud := oldInstall(t, m, "nextcloud", "cloud")
	if _, err := m.Install(context.Background(), app(t, "site-web"), "neuf", nil); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	var logs []string
	m.Relocate(context.Background(), func(format string, args ...any) { logs = append(logs, format) })
	instances, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"cloud": "cloud-maison.local", "neuf": "neuf-maison.local", "www": "www-maison.local"}
	for _, instance := range instances {
		if instance.Address != want[instance.Name] {
			t.Errorf("%s : adresse %s", instance.Name, instance.Address)
		}
	}
	env, err := readEnv(filepath.Join(www, envName))
	if err != nil || env["PI_HOST"] != "www-maison.local" || env["PI_INSTANCE"] != "www" {
		t.Fatalf("env %v %v", env, err)
	}
	if rule := labelsOf(t, www)["traefik.http.routers.pi-www.rule"]; rule != "Host(`www-maison.local`)" {
		t.Fatalf("règle %s", rule)
	}
	if rule := labelsOf(t, cloud)["traefik.http.routers.pi-cloud.rule"]; rule != "Host(`cloud-maison.local`)" {
		t.Fatalf("règle de Nextcloud %s", rule)
	}
	if !runner.called("--project-name pi-www") || runner.called("--project-name pi-neuf") || !runner.called("occ config:system:set trusted_domains 1 --value=cloud-maison.local") {
		t.Fatalf("appels %v", runner.calls)
	}
	if len(logs) != 2 {
		t.Fatalf("journal %v", logs)
	}
	runner.calls = nil
	m.Relocate(context.Background(), func(string, ...any) {})
	if len(runner.calls) != 0 {
		t.Fatalf("déjà déplacés, appels %v", runner.calls)
	}
}

func TestRelocateRenamedAndFailure(t *testing.T) {
	runner := &fakeRunner{}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.local", Runner: runner, FollowName: true}
	if _, err := m.Install(context.Background(), app(t, "site-web"), "www", nil); err != nil {
		t.Fatal(err)
	}
	m.Renamed("salon")
	runner.fail = map[string]bool{"up --detach --wait": true}
	var logs []string
	m.Relocate(context.Background(), func(format string, args ...any) { logs = append(logs, format) })
	instances, _ := m.List()
	if len(instances) != 1 || instances[0].Address != "www-maison.local" || len(logs) != 1 {
		t.Fatalf("service qui ne redémarre pas : %+v %v", instances, logs)
	}
	if rule := labelsOf(t, m.serviceDir("www"))["traefik.http.routers.pi-www.rule"]; rule != "Host(`www-maison.local`)" {
		t.Fatalf("ancienne règle pas remise : %s", rule)
	}
	runner.fail = nil
	m.Relocate(context.Background(), func(string, ...any) {})
	if instances, _ := m.List(); instances[0].Address != "www-salon.local" {
		t.Fatalf("après renommage : %+v", instances)
	}
	fixed := &Manager{Dir: m.Dir, Domain: "maison.pi-appliance.com", Runner: runner}
	runner.calls = nil
	fixed.Relocate(context.Background(), func(string, ...any) {})
	if len(runner.calls) != 0 {
		t.Fatalf("vrai domaine : rien ne bouge, appels %v", runner.calls)
	}
}
