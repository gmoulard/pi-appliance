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
		"up --detach --wait --remove-orphans --force-recreate",
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

// volumeRunner simule deux volumes du service et leurs volumes de secours, dans des dossiers.
type volumeRunner struct {
	updateRunner
	root string
}

func (v *volumeRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	switch {
	case strings.HasPrefix(call, "volume ls"):
		v.fakeRunner.Run(ctx, dir, args...)
		return []byte("pi-cloud_db\npi-cloud_app\n"), nil
	case strings.HasPrefix(call, "volume inspect"):
		v.fakeRunner.Run(ctx, dir, args...)
		path := filepath.Join(v.root, args[len(args)-1])
		return []byte(path + "\n"), os.MkdirAll(path, 0o700)
	case strings.HasPrefix(call, "volume rm"):
		v.fakeRunner.Run(ctx, dir, args...)
		return nil, os.RemoveAll(filepath.Join(v.root, args[len(args)-1]))
	case strings.Contains(call, " up --detach --wait --remove-orphans") && v.failUp > 0:
		// La nouvelle version abîme les données avant d'échouer.
		os.WriteFile(filepath.Join(v.root, "pi-cloud_db", "base"), []byte("format v31"), 0o600)
	}
	return v.updateRunner.Run(ctx, dir, args...)
}

// goCopy copie un dossier plat, à la place de « cp -a ».
type goCopy struct{}

func (goCopy) Copy(_ context.Context, from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(from, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(to, entry.Name()), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func volumeManager(t *testing.T, failUp int, freeMB int64) (*Manager, *volumeRunner) {
	t.Helper()
	runner := &volumeRunner{updateRunner: updateRunner{failUp: failUp}, root: t.TempDir()}
	m := installed(t, &runner.updateRunner)
	m.Runner = runner
	m.Copier = goCopy{}
	m.FreeMB = func(string) (int64, bool) { return freeMB, true }
	db := filepath.Join(runner.root, "pi-cloud_db")
	if err := os.MkdirAll(db, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(db, "base"), []byte("format v30"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner.calls = nil
	return m, runner
}

func TestUpdateCopiesData(t *testing.T) {
	m, runner := volumeManager(t, 0, 10000)
	app := nextcloud(t)
	app.Compose = newVersion(t, m)
	if _, err := m.Update(context.Background(), app, "cloud"); err != nil {
		t.Fatal(err)
	}
	calls := strings.Join(runner.calls, "\n")
	for _, call := range []string{"volume create --label pi-appliance.rollback=cloud pi-rollback-pi-cloud_db", "volume rm pi-rollback-pi-cloud_db", "volume rm pi-rollback-pi-cloud_app"} {
		if !strings.Contains(calls, call) {
			t.Fatalf("appel %q manquant : %v", call, runner.calls)
		}
	}
	// Ordre : téléchargement service en marche, arrêt, copie, démarrage.
	pull, stop, create, up := strings.Index(calls, " pull"), strings.Index(calls, " stop"), strings.Index(calls, "volume create"), strings.Index(calls, "--remove-orphans")
	if !(pull < stop && stop < create && create < up) {
		t.Fatalf("ordre des étapes : %v", runner.calls)
	}
}

func TestUpdateRollbackRestoresData(t *testing.T) {
	m, runner := volumeManager(t, 1, 10000)
	app := nextcloud(t)
	app.Compose = newVersion(t, m)
	if _, err := m.Update(context.Background(), app, "cloud"); !errors.Is(err, ErrRolledBack) {
		t.Fatalf("erreur %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(runner.root, "pi-cloud_db", "base"))
	if string(data) != "format v30" {
		t.Fatalf("données non rendues : %q", data)
	}
	if !runner.called("down --remove-orphans") || !runner.called("volume rm pi-rollback-pi-cloud_db") {
		t.Fatalf("appels %v", runner.calls)
	}
}

func TestUpdateRollbackFailsKeepsData(t *testing.T) {
	m, runner := volumeManager(t, 2, 10000)
	app := nextcloud(t)
	app.Compose = newVersion(t, m)
	if _, err := m.Update(context.Background(), app, "cloud"); err == nil || errors.Is(err, ErrRolledBack) {
		t.Fatalf("erreur %v", err)
	}
	if runner.called("volume rm") {
		t.Fatal("données de secours supprimées alors que le retour arrière a échoué")
	}
}

func TestUpdateNeedsSpace(t *testing.T) {
	m, runner := volumeManager(t, 0, 0)
	os.WriteFile(filepath.Join(runner.root, "pi-cloud_db", "gros"), make([]byte, 2<<20), 0o600)
	app := nextcloud(t)
	app.Compose = newVersion(t, m)
	_, err := m.Update(context.Background(), app, "cloud")
	if !errors.Is(err, ErrNoSpace) {
		t.Fatalf("erreur %v", err)
	}
	if runner.called(" pull") || runner.called("image tag") {
		t.Fatalf("mise à jour commencée sans place : %v", runner.calls)
	}
	if m.UpdateAvailable(app, "cloud") == false {
		t.Fatal("manifeste changé alors que la mise à jour a été refusée")
	}
}
