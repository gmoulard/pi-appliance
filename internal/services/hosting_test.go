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

// sqlRunner : faux Docker qui répond aux requêtes psql de CreateDatabase.
type sqlRunner struct {
	fakeRunner
	roles map[string]bool
}

func (r *sqlRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, err := r.fakeRunner.Run(ctx, dir, args...)
	call := strings.Join(args, " ")
	if strings.Contains(call, "psql") {
		sql := args[len(args)-1]
		switch {
		case strings.HasPrefix(sql, "SELECT"):
			kind := "role "
			if strings.Contains(sql, "FROM pg_database") {
				kind = "base "
			}
			for known := range r.roles {
				if strings.Contains(sql, "'"+strings.TrimPrefix(known, kind)+"'") && strings.HasPrefix(known, kind) {
					return []byte("1\n"), nil
				}
			}
			return nil, nil
		case strings.HasPrefix(sql, "CREATE ROLE"):
			r.roles["role "+strings.Fields(sql)[2]] = true
		case strings.HasPrefix(sql, "CREATE DATABASE"):
			r.roles["base "+strings.Fields(sql)[2]] = true
		}
	}
	return out, err
}

const nautilinkCompose = `services:
  nautilink:
    image: ghcr.io/gmoulard/nautilink-serveur:latest
    environment:
      NAUTILINK_BASE: ${PI_DATABASE_URL:-}
`

func installNautilink(t *testing.T, m *Manager) {
	t.Helper()
	if _, err := m.InstallCustom(context.Background(), CustomRequest{Name: "nautilink", Compose: []byte(nautilinkCompose), Service: "nautilink", Port: 8080}); err != nil {
		t.Fatal(err)
	}
}

func readOverride(t *testing.T, m *Manager, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), overrideName))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSubPathCustomOnly(t *testing.T) {
	m := manager(t, &fakeRunner{})
	installNautilink(t, m)
	instance, _ := m.instance("nautilink")
	if m.InternetPath(instance) != PathUnsupported || InstanceKeepsPath(instance) {
		t.Fatal("Docker Compose libre publié sans l'accord de l'administrateur")
	}
	instance, err := m.SetSubPath("nautilink", true)
	if err != nil {
		t.Fatal(err)
	}
	if m.InternetPath(instance) != PathReady || !InstanceKeepsPath(instance) {
		t.Fatalf("SubPath sans effet : %+v", instance)
	}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetSubPath("cloud", true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("service du catalogue réglé : %v", err)
	}
}

func TestMailRelayEnv(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	installNautilink(t, m)
	if _, err := m.SetMail(context.Background(), "nautilink", true); err != nil {
		t.Fatal(err)
	}
	env, _ := readEnv(filepath.Join(m.serviceDir("nautilink"), envName))
	if env["PI_SMTP_HOST"] != MailHost || env["PI_SMTP_PORT"] != "2525" {
		t.Fatalf(".env : %v", env)
	}
	if !strings.Contains(readOverride(t, m, "nautilink"), MailHost+":host-gateway") {
		t.Fatal("adresse du relais absente du conteneur")
	}
	if name, ok := m.MailAllowed("nautilink"); !ok || name != "nautilink" {
		t.Fatal("relais refusé")
	}
	if _, err := m.SetMail(context.Background(), "nautilink", false); err != nil {
		t.Fatal(err)
	}
	env, _ = readEnv(filepath.Join(m.serviceDir("nautilink"), envName))
	if env["PI_SMTP_HOST"] != "" || strings.Contains(readOverride(t, m, "nautilink"), MailHost) {
		t.Fatal("relais toujours réglé après l'arrêt")
	}
	if _, ok := m.MailAllowed("nautilink"); ok {
		t.Fatal("relais toujours ouvert")
	}
	// Le réglage reste après une réécriture complète (mise à jour).
	m.SetMail(context.Background(), "nautilink", true)
	app := marketplace.App{ID: CustomApp, Name: "Docker Compose libre", Subdomain: "nautilink", Service: "nautilink", Port: 8080, Compose: []byte(nautilinkCompose)}
	if _, err := m.Update(context.Background(), app, "nautilink"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readOverride(t, m, "nautilink"), MailHost) {
		t.Fatal("relais perdu à la mise à jour")
	}
}

func TestRedirectHosts(t *testing.T) {
	m := manager(t, &fakeRunner{})
	installNautilink(t, m)
	instance, err := m.SetRedirectHosts("nautilink", []string{" NautiLink.Moulard.org. ", "nautilink.moulard.org", ""})
	if err != nil {
		t.Fatal(err)
	}
	if len(instance.RedirectHosts) != 1 || instance.RedirectHosts[0] != "nautilink.moulard.org" {
		t.Fatalf("domaines : %v", instance.RedirectHosts)
	}
	for _, bad := range []string{"pas un domaine", "-a.org", "localhost", "a.b.c/x"} {
		if _, err := m.SetRedirectHosts("nautilink", []string{bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepté", bad)
		}
	}
	m.Install(context.Background(), nextcloud(t), "cloud", nil)
	if _, err := m.SetRedirectHosts("cloud", []string{"nautilink.moulard.org"}); !errors.Is(err, ErrExists) {
		t.Fatalf("domaine pris deux fois : %v", err)
	}
}

func postgres(t *testing.T) marketplace.App {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get(PostgresApp)
	if !ok {
		t.Fatal("PostgreSQL absent du catalogue")
	}
	return app
}

func TestSharedDatabase(t *testing.T) {
	runner := &sqlRunner{roles: map[string]bool{}}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner, MemoryMB: func() int { return 4096 }}
	installNautilink(t, m)
	if _, err := m.CreateDatabase(context.Background(), "nautilink"); !errors.Is(err, ErrNoPostgres) {
		t.Fatalf("base sans PostgreSQL : %v", err)
	}
	if _, err := m.Install(context.Background(), postgres(t), "base", nil); err != nil {
		t.Fatal(err)
	}
	server := readOverride(t, m, "base")
	if !strings.Contains(server, DatabaseNetwork) || !strings.Contains(server, "pi-base-db") {
		t.Fatalf("serveur hors du réseau de la base : %s", server)
	}
	instance, err := m.CreateDatabase(context.Background(), "nautilink")
	if err != nil {
		t.Fatal(err)
	}
	if instance.Database == nil || instance.Database.Name != "pi_nautilink" || instance.Database.Server != "base" {
		t.Fatalf("base : %+v", instance.Database)
	}
	if !runner.called("CREATE DATABASE pi_nautilink OWNER pi_nautilink") || !runner.called("REVOKE ALL ON DATABASE pi_nautilink FROM PUBLIC") {
		t.Fatalf("SQL : %v", runner.calls)
	}
	env, _ := readEnv(filepath.Join(m.serviceDir("nautilink"), envName))
	password, _ := m.databasePassword("nautilink")
	if len(password) < 20 || env["PI_DATABASE_URL"] != "postgres://pi_nautilink:"+password+"@pi-base-db:5432/pi_nautilink?sslmode=disable" {
		t.Fatalf(".env : %v", env)
	}
	if !strings.Contains(readOverride(t, m, "nautilink"), DatabaseNetwork) {
		t.Fatal("service hors du réseau de la base")
	}
	state, _ := os.ReadFile(filepath.Join(m.serviceDir("nautilink"), stateName))
	if strings.Contains(string(state), password) {
		t.Fatal("mot de passe dans instance.json (parti dans l'API)")
	}
	if data, _ := json.Marshal(instance); strings.Contains(string(data), password) {
		t.Fatal("mot de passe dans l'état rendu")
	}
	// Deuxième demande : même mot de passe, compte gardé.
	if _, err := m.CreateDatabase(context.Background(), "nautilink"); err != nil {
		t.Fatal(err)
	}
	if again, _ := m.databasePassword("nautilink"); again != password || !runner.called("ALTER ROLE pi_nautilink") {
		t.Fatal("deuxième demande : mot de passe changé ou compte recréé")
	}
	if err := m.Remove(context.Background(), "base", false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("PostgreSQL retiré sous ses clients : %v", err)
	}
	if err := m.Remove(context.Background(), "nautilink", false); err != nil {
		t.Fatal(err)
	}
	if !runner.called("DROP DATABASE IF EXISTS pi_nautilink WITH (FORCE)") || !runner.called("DROP ROLE IF EXISTS pi_nautilink") {
		t.Fatal("base non effacée à la désinstallation")
	}
	if err := m.Remove(context.Background(), "base", false); err != nil {
		t.Fatal(err)
	}
}

func TestServiceByIP(t *testing.T) {
	runner := &ipRunner{}
	m := &Manager{Dir: t.TempDir(), Runner: runner}
	name, err := m.ServiceByIP(context.Background(), "172.20.0.5")
	if err != nil || name != "nautilink" {
		t.Fatalf("%q, %v", name, err)
	}
	if name, _ := m.ServiceByIP(context.Background(), "172.20.0.9"); name != "" {
		t.Fatalf("adresse inconnue : %q", name)
	}
}

type ipRunner struct{}

func (ipRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if args[0] == "network" {
		return []byte(`{"abc":{"Name":"pi-nautilink-nautilink-1","IPv4Address":"172.20.0.5/16"},"def":{"Name":"traefik","IPv4Address":"172.20.0.2/16"}}`), nil
	}
	if args[len(args)-1] == "abc" {
		return []byte("pi-nautilink\n"), nil
	}
	return []byte("\n"), nil
}
