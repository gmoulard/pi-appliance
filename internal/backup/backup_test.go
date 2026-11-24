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

package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/storage"
)

// fakeServices : des services installés sans Docker, chaque volume est un dossier.
type fakeServices struct {
	dir     string // dossier des données de la console
	volumes string // dossier des volumes
	running map[string]bool
	log     []string
	// stopped : services arrêtés pendant la copie de leurs volumes.
	stopped []string
	fail    map[string]error
}

func (f *fakeServices) List() ([]services.Instance, error) {
	entries, err := os.ReadDir(filepath.Join(f.dir, "services"))
	if err != nil {
		return []services.Instance{}, nil
	}
	var list []services.Instance
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(f.dir, "services", e.Name(), "state.json")); err == nil {
			list = append(list, services.Instance{Name: e.Name(), App: "app-" + e.Name()})
		}
	}
	return list, nil
}

func (f *fakeServices) Volumes(_ context.Context, name string) ([]services.Volume, error) {
	if err := f.fail[name]; err != nil {
		return nil, err
	}
	paths, _ := filepath.Glob(filepath.Join(f.volumes, "pi-"+name+"_*"))
	var list []services.Volume
	for _, p := range paths {
		list = append(list, services.Volume{Name: filepath.Base(p), Path: p})
	}
	return list, nil
}

func (f *fakeServices) CreateVolume(_ context.Context, name, volume string) (string, error) {
	f.log = append(f.log, "create "+volume)
	path := filepath.Join(f.volumes, volume)
	return path, os.MkdirAll(path, 0o755)
}

func (f *fakeServices) Running(_ context.Context, name string) bool {
	return f.running[name]
}

func (f *fakeServices) Control(_ context.Context, name string, action services.Action) error {
	f.log = append(f.log, string(action)+" "+name)
	f.running[name] = action != services.Stop
	return nil
}

type fixture struct {
	*Manager
	fake *fakeServices
	root string
	now  time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{
		fake: &fakeServices{dir: filepath.Join(base, "data"), volumes: filepath.Join(base, "volumes"), running: map[string]bool{}, fail: map[string]error{}},
		root: filepath.Join(base, "disk", "pi"),
		now:  time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC),
	}
	f.Manager = &Manager{
		Dir:      f.fake.dir,
		Services: f.fake,
		Target:   func() (string, error) { return f.root, nil },
		FreeMB:   func(string) (int64, bool) { return 1 << 20, true },
		Now:      func() time.Time { return f.now },
		Host:     "pi",
	}
	write(t, filepath.Join(f.fake.dir, "accounts.json"), "[]")
	if err := f.Protect("mot de passe administrateur"); err != nil {
		t.Fatal(err)
	}
	f.install(t, "cloud", "data/photo.jpg", "photo v1")
	f.fake.running["cloud"] = true
	return f
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// install crée un service avec ses réglages et un fichier dans son volume « files ».
func (f *fixture) install(t *testing.T, name, file, content string) {
	t.Helper()
	write(t, filepath.Join(f.fake.dir, "services", name, "state.json"), `{"name":"`+name+`"}`)
	write(t, filepath.Join(f.fake.dir, "services", name, ".env"), "SECRET=v1")
	write(t, filepath.Join(f.fake.volumes, "pi-"+name+"_files", file), content)
}

func (f *fixture) volume(name string) string {
	return filepath.Join(f.fake.volumes, "pi-"+name+"_files")
}

func TestBackupAndRestore(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if runtime.GOOS != "windows" {
		if err := os.Symlink("photo.jpg", filepath.Join(f.volume("cloud"), "data", "lien")); err != nil {
			t.Fatal(err)
		}
		os.Chmod(filepath.Join(f.volume("cloud"), "data", "photo.jpg"), 0o640)
	}
	write(t, filepath.Join(f.volume("cloud"), "vide", ".keep"), "")
	snapshot, err := f.Backup(ctx, Manual)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.fake.log, []string{"stop cloud", "start cloud"}) {
		t.Fatalf("le service doit être arrêté pendant la copie puis redémarré : %v", f.fake.log)
	}
	if last := f.Last(); last == nil || !last.OK {
		t.Fatalf("résultat : %+v", last)
	}
	// Ni la clé ni l'enveloppe en clair dans la sauvegarde : seule l'enveloppe du manifest.
	assertNoPlaintext(t, filepath.Join(f.root, snapshot.ID), "photo v1", "SECRET=v1")
	if _, err := os.Stat(filepath.Join(f.root, snapshot.ID, "services", "cloud.enc")); err != nil {
		t.Fatal(err)
	}

	// Les données et les réglages changent, puis on restaure.
	write(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg"), "photo v2")
	write(t, filepath.Join(f.volume("cloud"), "nouveau.txt"), "à effacer")
	write(t, filepath.Join(f.fake.dir, "services", "cloud", ".env"), "SECRET=v2")
	f.now = f.now.Add(time.Hour)
	f.fake.log = nil
	if err := f.Restore(ctx, snapshot.ID, "cloud", ""); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("données restaurées : %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.volume("cloud"), "nouveau.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("un fichier créé après la sauvegarde doit disparaître")
	}
	if _, err := os.Stat(filepath.Join(f.volume("cloud"), "vide", ".keep")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.fake.dir, "services", "cloud", ".env")); got != "SECRET=v1" {
		t.Fatalf("réglages restaurés : %q", got)
	}
	if runtime.GOOS != "windows" {
		if link, err := os.Readlink(filepath.Join(f.volume("cloud"), "data", "lien")); err != nil || link != "photo.jpg" {
			t.Fatalf("lien : %q %v", link, err)
		}
		if info, _ := os.Stat(filepath.Join(f.volume("cloud"), "data", "photo.jpg")); info.Mode().Perm() != 0o640 {
			t.Fatalf("droits : %v", info.Mode())
		}
	}
	// Copie d'avant restauration (pendant sa copie, arrêt puis redémarrage),
	// puis arrêt, restauration et redémarrage.
	if want := []string{"stop cloud", "start cloud", "stop cloud", "start cloud"}; !slices.Equal(f.fake.log, want) {
		t.Fatalf("étapes : %v, attendu %v", f.fake.log, want)
	}
	snapshots, err := f.Snapshots()
	if err != nil || len(snapshots) != 2 || snapshots[0].Kind != BeforeRestore {
		t.Fatalf("copie d'avant restauration attendue : %+v %v", snapshots, err)
	}
	// La copie d'avant restauration contient la version 2 : on peut revenir en arrière.
	if err := f.Restore(ctx, snapshots[0].ID, "cloud", ""); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v2" {
		t.Fatalf("retour en arrière : %q", got)
	}
}

func assertNoPlaintext(t *testing.T, dir string, secrets ...string) {
	t.Helper()
	filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, _ := os.ReadFile(path)
		for _, secret := range secrets {
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("%s contient %q en clair", path, secret)
			}
		}
		return nil
	})
}

func TestRestoreUninstalledService(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	snapshot, err := f.Backup(ctx, Nightly)
	if err != nil {
		t.Fatal(err)
	}
	// Désinstallé, données comprises.
	os.RemoveAll(filepath.Join(f.fake.dir, "services", "cloud"))
	os.RemoveAll(f.volume("cloud"))
	f.fake.log = nil
	if err := f.Restore(ctx, snapshot.ID, "cloud", ""); err != nil {
		t.Fatal(err)
	}
	if want := []string{"create pi-cloud_files", "start cloud"}; !slices.Equal(f.fake.log, want) {
		t.Fatalf("étapes : %v, attendu %v", f.fake.log, want)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("données : %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.fake.dir, "services", "cloud", "state.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreFromAnotherKey(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	snapshot, err := f.Backup(ctx, Manual)
	if err != nil {
		t.Fatal(err)
	}
	// Nouvelle carte SD : nouvelle clé, nouveau mot de passe.
	os.RemoveAll(filepath.Join(f.fake.dir, stateDir))
	if err := f.Protect("nouveau mot de passe"); err != nil {
		t.Fatal(err)
	}
	if err := f.Restore(ctx, snapshot.ID, "cloud", ""); !errors.Is(err, ErrPassword) {
		t.Fatalf("sans mot de passe : %v", err)
	}
	if err := f.Restore(ctx, snapshot.ID, "cloud", "nouveau mot de passe"); !errors.Is(err, ErrPassword) {
		t.Fatalf("mot de passe d'aujourd'hui au lieu de celui de la sauvegarde : %v", err)
	}
	write(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg"), "photo v2")
	if err := f.Restore(ctx, snapshot.ID, "cloud", "mot de passe administrateur"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("données : %q", got)
	}
}

func TestBackupRefusals(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.FreeMB = func(string) (int64, bool) { return 0, true }
	write(t, filepath.Join(f.volume("cloud"), "gros"), strings.Repeat("x", 2<<20))
	if _, err := f.Backup(ctx, Manual); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("disque plein : %v", err)
	}
	if len(f.fake.log) != 0 {
		t.Fatalf("rien ne doit être arrêté sans place : %v", f.fake.log)
	}
	f.FreeMB = nil
	f.Target = func() (string, error) { return "", ErrNoDisk }
	if _, err := f.Backup(ctx, Manual); !errors.Is(err, ErrNoDisk) {
		t.Fatalf("sans disque : %v", err)
	}
	if last := f.Last(); last == nil || last.OK {
		t.Fatalf("échec à garder : %+v", last)
	}
	f.Target = func() (string, error) { return f.root, nil }
	os.Remove(f.keys().envelopePath())
	if _, err := f.Backup(ctx, Manual); !errors.Is(err, ErrNoKey) {
		t.Fatalf("sans enveloppe : %v", err)
	}
	if !f.begin("backup") {
		t.Fatal("verrou")
	}
	if _, err := f.Backup(ctx, Manual); !errors.Is(err, ErrBusy) {
		t.Fatalf("pendant une autre : %v", err)
	}
	f.end()
}

func TestBackupKeepsGoingWhenAServiceFails(t *testing.T) {
	f := newFixture(t)
	f.install(t, "blog", "index.html", "blog")
	f.fake.fail["blog"] = errors.New("docker ne répond pas")
	snapshot, err := f.Backup(context.Background(), Nightly)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.service("cloud"); !ok {
		t.Fatal("cloud doit être sauvegardé")
	}
	if _, ok := snapshot.service("blog"); ok {
		t.Fatal("blog n'est pas restaurable")
	}
	if last := f.Last(); last == nil || last.OK || !strings.Contains(last.Message, "blog") {
		t.Fatalf("résultat : %+v", last)
	}
}

func TestPartialBackupsAreIgnoredAndRemoved(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.root, "20261119-030000"+partialSuffix), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.root, "20261118-030000"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Backup(context.Background(), Nightly); err != nil {
		t.Fatal(err)
	}
	snapshots, _ := f.Snapshots()
	if len(snapshots) != 1 {
		t.Fatalf("un dossier sans manifest n'est pas une sauvegarde : %+v", snapshots)
	}
	if exists(filepath.Join(f.root, "20261119-030000"+partialSuffix)) {
		t.Fatal("sauvegarde interrompue à effacer")
	}
}

func TestExpired(t *testing.T) {
	now := time.Date(2026, 11, 20, 3, 0, 0, 0, time.UTC)
	var snapshots []Snapshot
	// Une sauvegarde par nuit pendant un an, et deux le dernier jour.
	for day := 0; day < 365; day++ {
		created := now.AddDate(0, 0, -day)
		snapshots = append(snapshots, Snapshot{ID: created.Format(idLayout), Created: created, Kind: Nightly})
	}
	manual := now.Add(2 * time.Hour)
	snapshots = append(snapshots, Snapshot{ID: manual.Format(idLayout), Created: manual, Kind: Manual})
	old := now.AddDate(0, 0, -8)
	snapshots = append(snapshots, Snapshot{ID: "avant-ancienne", Created: old, Kind: BeforeRestore})
	recent := now.AddDate(0, 0, -1)
	snapshots = append(snapshots, Snapshot{ID: "avant-recente", Created: recent, Kind: BeforeRestore})

	gone := map[string]bool{}
	for _, s := range expired(snapshots, DefaultSettings, manual) {
		gone[s.ID] = true
	}
	var kept []string
	for _, s := range snapshots {
		if !gone[s.ID] {
			kept = append(kept, s.ID)
		}
	}
	want := []string{
		// 7 quotidiennes (la manuelle remplace la nuit du 20).
		"20261120-050000", "20261119-030000", "20261118-030000", "20261117-030000", "20261116-030000", "20261115-030000", "20261114-030000",
		// 4 hebdomadaires : dimanche de chaque semaine ISO (la semaine en cours a déjà la manuelle).
		"20261108-030000", "20261101-030000",
		// 6 mensuelles : dernier jour de chaque mois.
		"20261031-030000", "20260930-030000", "20260831-030000", "20260731-030000", "20260630-030000",
		"avant-recente",
	}
	slices.Sort(kept)
	slices.Sort(want)
	if !slices.Equal(kept, want) {
		t.Fatalf("gardées :\n%v\nattendu :\n%v", kept, want)
	}
}

func TestRotationAfterBackup(t *testing.T) {
	f := newFixture(t)
	if err := f.SetSettings(Settings{Enabled: true, Hour: 3, Daily: 2}); err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 4; day++ {
		if _, err := f.Backup(context.Background(), Nightly); err != nil {
			t.Fatal(err)
		}
		f.now = f.now.AddDate(0, 0, 1)
	}
	snapshots, _ := f.Snapshots()
	var ids []string
	for _, s := range snapshots {
		ids = append(ids, s.ID)
	}
	if want := []string{"20261123-030000", "20261122-030000"}; !slices.Equal(ids, want) {
		t.Fatalf("après rotation : %v, attendu %v", ids, want)
	}
}

func TestSettings(t *testing.T) {
	f := newFixture(t)
	if f.Settings() != DefaultSettings {
		t.Fatalf("réglages par défaut : %+v", f.Settings())
	}
	for _, bad := range []Settings{{Hour: 24, Daily: 1}, {Hour: -1, Daily: 1}, {Hour: 3}, {Hour: 3, Daily: -1, Weekly: 2}} {
		if err := f.SetSettings(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v : %v", bad, err)
		}
	}
	custom := Settings{Enabled: false, Hour: 1, Daily: 3, Weekly: 0, Monthly: 12}
	if err := f.SetSettings(custom); err != nil || f.Settings() != custom {
		t.Fatalf("%+v %v", f.Settings(), err)
	}
}

func TestDue(t *testing.T) {
	f := newFixture(t)
	night := time.Date(2026, 11, 21, 3, 0, 0, 0, time.Local)
	if f.due(night.Add(-time.Hour)) {
		t.Fatal("pas avant l'heure")
	}
	if !f.due(night) || f.due(night.Add(time.Minute)) {
		t.Fatal("une fois par nuit")
	}
	if !f.due(night.AddDate(0, 0, 1)) {
		t.Fatal("la nuit suivante")
	}
	f.SetSettings(Settings{Enabled: false, Hour: 3, Daily: 7})
	if f.due(night.AddDate(0, 0, 2)) {
		t.Fatal("désactivée")
	}
}

func TestExtractRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	x := newExtractor(dir)
	header := &tar.Header{Typeflag: tar.TypeReg, Mode: 0o644, ModTime: time.Now()}
	for _, name := range []string{"v/../../dehors", "/etc/passwd", "v/../x"} {
		if rel, ok := entryPath(name, "v"); ok {
			t.Errorf("%s accepté comme %s", name, rel)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "lien")); err != nil {
		t.Fatal(err)
	}
	err := x.add(header, filepath.Join("lien", "fichier"), strings.NewReader("x"))
	if err == nil {
		t.Fatal("écriture à travers un lien symbolique acceptée")
	}
	if exists(filepath.Join(outside, "fichier")) {
		t.Fatal("fichier écrit hors du dossier")
	}
}

func TestArchiveRoundTrip(t *testing.T) {
	key := testKey(t)
	src := t.TempDir()
	for i := 0; i < 20; i++ {
		write(t, filepath.Join(src, fmt.Sprintf("d%d", i%3), fmt.Sprintf("f%d", i)), strings.Repeat("données ", i*1000))
	}
	var archive bytes.Buffer
	if err := writeArchive(&archive, key, []tree{{dir: src, prefix: "v"}}); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	x := newExtractor(dst)
	err := readArchive(bytes.NewReader(archive.Bytes()), key, func(h *tar.Header, r io.Reader) error {
		rel, ok := entryPath(h.Name, "v")
		if !ok || rel == "." {
			return nil
		}
		return x.add(h, rel, r)
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		name := filepath.Join(fmt.Sprintf("d%d", i%3), fmt.Sprintf("f%d", i))
		if read(t, filepath.Join(dst, name)) != read(t, filepath.Join(src, name)) {
			t.Fatalf("%s différent", name)
		}
	}
	tampered := flip(archive.Bytes(), archive.Len()-3)
	err = readArchive(bytes.NewReader(tampered), key, func(*tar.Header, io.Reader) error { return nil })
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("archive modifiée : %v", err)
	}
}

type scriptRunner struct {
	out  string
	err  error
	args []string
}

func (s *scriptRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	s.args = args
	return []byte(s.out), s.err
}

func TestDisks(t *testing.T) {
	runner := &scriptRunner{out: `{"disks":[{"device":"/dev/sdb","partition":"/dev/sdb1","fstype":"ext4"}],"target":null}`}
	disks := &Disks{Runner: runner}
	status, err := disks.Status(context.Background())
	if err != nil || len(status.Disks) != 1 || status.Target != nil {
		t.Fatalf("%+v %v", status, err)
	}
	if err := disks.Use(context.Background(), "/dev/sdb1;reboot"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("partition refusée avant le script : %v", err)
	}
	runner.out, runner.err = `{"ok":false,"error":"already","message":"Un disque de sauvegarde est déjà utilisé."}`, errors.New("exit status 1")
	err = disks.Format(context.Background(), "/dev/sdb")
	var failure storage.Failure
	if !errors.As(err, &failure) || failure.Code != "already" || !slices.Equal(runner.args, []string{"format", "/dev/sdb"}) {
		t.Fatalf("%v %v", err, runner.args)
	}
}

func TestRestoreAppliance(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	write(t, filepath.Join(f.fake.dir, "accounts.json"), `[{"name":"marie"}]`)
	snapshot, err := f.Backup(ctx, Manual)
	if err != nil {
		t.Fatal(err)
	}
	// Nouvelle carte SD : ni comptes, ni services, ni données ; seul le disque de sauvegarde reste.
	os.RemoveAll(f.fake.dir)
	os.RemoveAll(f.fake.volumes)
	write(t, filepath.Join(f.fake.dir, "accounts.json"), "[]")
	f.fake.log = nil
	if err := f.RestoreAppliance(ctx, snapshot.ID, "autre"); !errors.Is(err, ErrPassword) {
		t.Fatalf("mauvais mot de passe : %v", err)
	}
	if err := f.RestoreAppliance(ctx, snapshot.ID, "mot de passe administrateur"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.fake.dir, "accounts.json")); got != `[{"name":"marie"}]` {
		t.Fatalf("comptes : %q", got)
	}
	if got := read(t, filepath.Join(f.fake.dir, "services", "cloud", ".env")); got != "SECRET=v1" {
		t.Fatalf("réglages du service : %q", got)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("données : %q", got)
	}
	if want := []string{"create pi-cloud_files", "start cloud"}; !slices.Equal(f.fake.log, want) {
		t.Fatalf("étapes : %v, attendu %v", f.fake.log, want)
	}
	// La clé est celle de la sauvegarde : les sauvegardes d'avant se restaurent sans mot de passe.
	if err := f.Restore(ctx, snapshot.ID, "cloud", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Backup(ctx, Manual); err != nil {
		t.Fatal(err)
	}
}
