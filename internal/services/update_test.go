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
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const currentImages = `[{"ID":"sha256:aaa","ContainerName":"pi-cloud-db-1","Repository":"mariadb","Tag":"11.4"},` +
	`{"ID":"sha256:bbb","ContainerName":"pi-cloud-app-1","Repository":"nextcloud","Tag":"30-apache"},` +
	`{"ID":"sha256:bbb","ContainerName":"pi-cloud-cron-1","Repository":"nextcloud","Tag":"30-apache"}]`

// updateRunner répond à « compose images » et fait échouer les N premiers « up --remove-orphans ».
type updateRunner struct {
	fakeRunner
	failUp int
}

func (u *updateRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	output, err := u.fakeRunner.Run(ctx, dir, args...)
	call := strings.Join(args, " ")
	switch {
	case strings.Contains(call, " images --format json"):
		return []byte(currentImages), nil
	case strings.Contains(call, "--remove-orphans") && u.failUp > 0:
		u.failUp--
		return []byte("conteneur en échec"), errors.New("exit status 1")
	}
	return output, err
}

func newVersion(t *testing.T, m *Manager) []byte {
	t.Helper()
	app := nextcloud(t)
	if m.UpdateAvailable(app, "cloud") {
		t.Fatal("mise à jour signalée sans changement du catalogue")
	}
	compose := bytes.Replace(app.Compose, []byte("nextcloud:30-apache"), []byte("nextcloud:31-apache"), 1)
	return compose
}

func TestUpdate(t *testing.T) {
	runner := &updateRunner{}
	m := installed(t, runner)
	app := nextcloud(t)
	app.Compose = newVersion(t, m)
	if !m.UpdateAvailable(app, "cloud") {
		t.Fatal("mise à jour non signalée")
	}
	runner.calls = nil
	result, err := m.Update(context.Background(), app, "cloud")
	if err != nil || result.Instance.Name != "cloud" {
		t.Fatalf("mise à jour : %v %+v", err, result)
	}
	for _, call := range []string{
		"image tag sha256:aaa mariadb:pi-rollback-cloud-11.4",
		"image tag sha256:bbb nextcloud:pi-rollback-cloud-30-apache",
		" pull",
		"up --detach --wait --remove-orphans",
		"image rm nextcloud:pi-rollback-cloud-30-apache",
	} {
		if !runner.called(call) {
			t.Fatalf("appel %q manquant : %v", call, runner.calls)
		}
	}
	if tags := strings.Count(strings.Join(runner.calls, "\n"), "image tag sha256:bbb"); tags != 1 {
		t.Fatalf("image en double sauvegardée %d fois", tags)
	}
	dir := filepath.Join(m.Dir, "services", "cloud")
	installedCompose, _ := os.ReadFile(filepath.Join(dir, composeName))
	if !bytes.Contains(installedCompose, []byte("nextcloud:31-apache")) || m.UpdateAvailable(app, "cloud") {
		t.Fatal("nouveau manifeste non installé")
	}
	if _, err := os.Stat(filepath.Join(dir, previousName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ancien manifeste non supprimé après la réussite")
	}
}

func TestUpdateRollsBack(t *testing.T) {
	cases := map[string]*updateRunner{
		"téléchargement raté": {fakeRunner: fakeRunner{fail: map[string]bool{" pull": true}}},
		"démarrage raté":      {failUp: 1},
	}
	for label, runner := range cases {
		t.Run(label, func(t *testing.T) {
			m := installed(t, runner)
			dir := filepath.Join(m.Dir, "services", "cloud")
			before, _ := os.ReadFile(filepath.Join(dir, composeName))
			envBefore, _ := os.ReadFile(filepath.Join(dir, envName))
			app := nextcloud(t)
			app.Compose = newVersion(t, m)
			runner.calls = nil
			_, err := m.Update(context.Background(), app, "cloud")
			if !errors.Is(err, ErrRolledBack) {
				t.Fatalf("erreur %v", err)
			}
			for _, call := range []string{"image tag nextcloud:pi-rollback-cloud-30-apache nextcloud:30-apache", "image tag mariadb:pi-rollback-cloud-11.4 mariadb:11.4", "image rm mariadb:pi-rollback-cloud-11.4"} {
				if !runner.called(call) {
					t.Fatalf("appel %q manquant : %v", call, runner.calls)
				}
			}
			after, _ := os.ReadFile(filepath.Join(dir, composeName))
			envAfter, _ := os.ReadFile(filepath.Join(dir, envName))
			if !bytes.Equal(before, after) || !bytes.Equal(envBefore, envAfter) {
				t.Fatal("ancien manifeste ou ancien .env non restauré")
			}
			if !m.UpdateAvailable(app, "cloud") {
				t.Fatal("la mise à jour ratée doit rester proposée")
			}
		})
	}
}

func TestUpdateRollbackFails(t *testing.T) {
	runner := &updateRunner{failUp: 2}
	m := installed(t, runner)
	app := nextcloud(t)
	app.Compose = newVersion(t, m)
	_, err := m.Update(context.Background(), app, "cloud")
	if err == nil || errors.Is(err, ErrRolledBack) || !strings.Contains(err.Error(), "retour à la version précédente aussi") {
		t.Fatalf("erreur %v", err)
	}
	if runner.called("image rm") {
		t.Fatal("images de secours supprimées alors que le retour arrière a échoué")
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "services", "cloud", previousName)); err != nil {
		t.Fatal("ancien manifeste supprimé alors que le retour arrière a échoué")
	}
}

func TestUpdateErrors(t *testing.T) {
	m := installed(t, &updateRunner{})
	app := nextcloud(t)
	if _, err := m.Update(context.Background(), app, "inconnu"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service inconnu : %v", err)
	}
	app.ID = "autre"
	if _, err := m.Update(context.Background(), app, "cloud"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("autre application : %v", err)
	}
}

func TestParseImages(t *testing.T) {
	lines := `{"ID":"sha256:aaa","Repository":"mariadb","Tag":"11.4"}` + "\n" + `{"ID":"","Repository":"x","Tag":"1"}`
	images, err := parseImages([]byte(lines))
	if err != nil || len(images) != 1 || images[0].ref() != "mariadb:11.4" {
		t.Fatalf("images %+v, erreur %v", images, err)
	}
	if images, err := parseImages(nil); err != nil || len(images) != 0 {
		t.Fatalf("sortie vide : %v %v", images, err)
	}
	if long := (image{Repository: "r", Tag: strings.Repeat("t", 200)}).keepRef("pi-rollback-cloud"); len(long) > len("r:")+128 {
		t.Fatalf("étiquette trop longue : %d", len(long))
	}
}
