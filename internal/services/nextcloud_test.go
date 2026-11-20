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
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// occRunner : un Docker dont le Nextcloud répond aux commandes occ.
type occRunner struct {
	calls     []string
	installed bool
	fail      string
}

func (r *occRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case r.fail != "" && strings.Contains(call, r.fail):
		return []byte("erreur occ"), errors.New("exit status 1")
	case strings.Contains(call, "occ status"):
		if r.installed {
			return []byte(`{"installed":true,"version":"30.0.0"}`), nil
		}
		return []byte(`{"installed":false}`), nil
	case strings.Contains(call, "files_external:create"):
		return []byte("Storage created with id 7\n"), nil
	}
	return nil, nil
}

func (r *occRunner) occCalls() []string {
	var calls []string
	for _, call := range r.calls {
		if _, after, ok := strings.Cut(call, " php occ "); ok {
			calls = append(calls, after)
		}
	}
	r.calls = nil
	return calls
}

func instanceNamed(t *testing.T, m *Manager, name string) Instance {
	t.Helper()
	instances, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, instance := range instances {
		if instance.Name == name {
			return instance
		}
	}
	t.Fatalf("%s absent", name)
	return Instance{}
}

func TestLinkNextcloud(t *testing.T) {
	runner := &occRunner{installed: true}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner}
	ctx := context.Background()
	if _, err := m.Install(ctx, builtinApp(t, "site-web"), "www", nil); err != nil {
		t.Fatal(err)
	}
	if got := instanceNamed(t, m, "www"); got.NextcloudFolder != "" || got.NextcloudPending {
		t.Fatalf("sans Nextcloud : %+v", got)
	}
	if _, err := m.Install(ctx, nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if got := instanceNamed(t, m, "www"); !got.NextcloudPending {
		t.Fatalf("Nextcloud installé, dossier pas encore créé : %+v", got)
	}
	runner.calls = nil
	if err := m.LinkNextcloud(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"status --output=json",
		"app:enable files_external",
		"files_external:create /Site www local null::null --config datadir=/srv/pi-appliance/services/www/www",
		"files_external:applicable 7 --add-group=admin",
	}
	if got := runner.occCalls(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("occ :\n%s", strings.Join(got, "\n"))
	}
	if got := instanceNamed(t, m, "www"); got.NextcloudFolder != "Site www" || got.NextcloudPending {
		t.Fatalf("relié : %+v", got)
	}
	if err := m.LinkNextcloud(ctx); err != nil {
		t.Fatal(err)
	}
	if got := runner.occCalls(); len(got) != 0 {
		t.Fatalf("rien à faire, occ lancé : %v", got)
	}
	if err := m.Remove(ctx, "www", false); err != nil {
		t.Fatal(err)
	}
	if err := m.LinkNextcloud(ctx); err != nil {
		t.Fatal(err)
	}
	if got := runner.occCalls(); strings.Join(got, "\n") != "status --output=json\nfiles_external:delete --yes 7" {
		t.Fatalf("site retiré, occ :\n%s", strings.Join(got, "\n"))
	}
	if links := m.nextcloudLinks("cloud"); len(links) != 0 {
		t.Fatalf("liens %v", links)
	}
}

// Nextcloud encore en train de s'installer : rien n'est créé, on réessaiera.
func TestLinkNextcloudWaitsForInstall(t *testing.T) {
	runner := &occRunner{}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner}
	ctx := context.Background()
	for app, name := range map[string]string{"hugo": "blog", "nextcloud": "cloud"} {
		if _, err := m.Install(ctx, builtinApp(t, app), name, nil); err != nil {
			t.Fatal(err)
		}
	}
	runner.calls = nil
	if err := m.LinkNextcloud(ctx); err != nil {
		t.Fatal(err)
	}
	if got := runner.occCalls(); strings.Join(got, "\n") != "status --output=json" {
		t.Fatalf("occ : %v", got)
	}
	if got := instanceNamed(t, m, "blog"); !got.NextcloudPending {
		t.Fatalf("%+v", got)
	}
}

// Un dossier qui ne peut pas être réservé aux administrateurs de Nextcloud
// est supprimé aussitôt : il serait ouvert à tous les comptes.
func TestLinkNextcloudApplicableFails(t *testing.T) {
	runner := &occRunner{installed: true, fail: "files_external:applicable"}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner}
	ctx := context.Background()
	for app, name := range map[string]string{"site-web": "www", "nextcloud": "cloud"} {
		if _, err := m.Install(ctx, builtinApp(t, app), name, nil); err != nil {
			t.Fatal(err)
		}
	}
	runner.calls = nil
	if err := m.LinkNextcloud(ctx); err == nil {
		t.Fatal("erreur attendue")
	}
	got := runner.occCalls()
	if len(got) == 0 || got[len(got)-1] != "files_external:delete --yes 7" {
		t.Fatalf("occ : %v", got)
	}
	if links := m.nextcloudLinks("cloud"); len(links) != 0 {
		t.Fatalf("liens %v", links)
	}
}

// Le dossier du site est modifiable par Nextcloud, le reste du service non.
func TestSiteDirOpenToNextcloud(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("droits Unix")
	}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: &occRunner{}}
	if _, err := m.Install(context.Background(), builtinApp(t, "site-web"), "www", nil); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(m.Dir, "services"):                 0o711,
		m.serviceDir("www"):                              0o711,
		filepath.Join(m.serviceDir("www"), siteDir):      0o775,
		filepath.Join(m.serviceDir("www"), envName):      0o600,
		filepath.Join(m.serviceDir("www"), composeName):  0o600,
		filepath.Join(m.serviceDir("www"), overrideName): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Error(err)
			continue
		}
		if info.Mode().Perm() != mode {
			t.Errorf("%s : %v, attendu %v", path, info.Mode().Perm(), mode)
		}
	}
}
