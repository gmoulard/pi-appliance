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

package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// files attend que la liste des fichiers soit prête.
func (f *fixture) files(t *testing.T, id, service, dir string) Listing {
	t.Helper()
	for i := 0; i < 500; i++ {
		listing, _, err := f.Files(context.Background(), id, service, dir)
		if errors.Is(err, ErrPreparing) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		return listing
	}
	t.Fatal("liste jamais prête")
	return Listing{}
}

func names(listing Listing) []string {
	var list []string
	for _, e := range listing.Entries {
		list = append(list, e.Name)
	}
	return list
}

func TestBrowseAndDownload(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.volume("cloud"), "data", "notes", "liste.txt"), "lait, pain")
	write(t, filepath.Join(f.volume("cloud"), "lisez-moi.txt"), "bonjour")
	snapshot, err := f.Backup(context.Background(), Manual)
	if err != nil {
		t.Fatal(err)
	}
	// Première demande : la liste se prépare en arrière-plan ; sur une si
	// petite archive, elle peut déjà être prête (pas d'erreur).
	if _, _, err := f.Files(context.Background(), snapshot.ID, "cloud", ""); err != nil && !errors.Is(err, ErrPreparing) {
		t.Fatalf("première demande : %v", err)
	}
	if got := names(f.files(t, snapshot.ID, "cloud", "")); !slices.Equal(got, []string{"pi-cloud_files"}) {
		t.Fatalf("racine : %v", got)
	}
	// Dossiers d'abord, puis les fichiers.
	if got := names(f.files(t, snapshot.ID, "cloud", "pi-cloud_files")); !slices.Equal(got, []string{"data", "lisez-moi.txt"}) {
		t.Fatalf("volume : %v", got)
	}
	listing := f.files(t, snapshot.ID, "cloud", "pi-cloud_files/data")
	if got := names(listing); !slices.Equal(got, []string{"notes", "photo.jpg"}) || listing.Entries[1].Size != int64(len("photo v1")) {
		t.Fatalf("data : %+v", listing)
	}
	for _, bad := range []string{"../x", "pi-cloud_files/inconnu"} {
		if _, _, err := f.Files(context.Background(), snapshot.ID, "cloud", bad); err == nil {
			t.Errorf("%s accepté", bad)
		}
	}
	if _, _, err := f.Files(context.Background(), snapshot.ID, "inconnu", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service inconnu : %v", err)
	}

	// Un fichier.
	recorder := httptest.NewRecorder()
	if err := f.Download(context.Background(), recorder, snapshot.ID, "cloud", "pi-cloud_files/data/photo.jpg"); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.String() != "photo v1" || recorder.Header().Get("Content-Disposition") != "attachment; filename*=UTF-8''photo.jpg" {
		t.Fatalf("fichier : %q %v", recorder.Body.String(), recorder.Header())
	}
	// Un dossier : un .zip.
	recorder = httptest.NewRecorder()
	if err := f.Download(context.Background(), recorder, snapshot.ID, "cloud", "pi-cloud_files/data"); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		r, _ := file.Open()
		data, _ := io.ReadAll(r)
		contents[file.Name] = string(data)
	}
	if len(contents) != 2 || contents["data/photo.jpg"] != "photo v1" || contents["data/notes/liste.txt"] != "lait, pain" {
		t.Fatalf("zip : %v", contents)
	}
	// Introuvable : rien n'est encore parti, l'erreur peut être répondue.
	recorder = httptest.NewRecorder()
	if err := f.Download(context.Background(), recorder, snapshot.ID, "cloud", "pi-cloud_files/absent"); !errors.Is(err, ErrNotFound) || recorder.Body.Len() != 0 {
		t.Fatalf("absent : %v %q", err, recorder.Body.String())
	}
}

func TestPutBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	write(t, filepath.Join(f.volume("cloud"), "data", "notes", "liste.txt"), "lait, pain")
	os.Chmod(filepath.Join(f.volume("cloud"), "data", "notes"), 0o750)
	snapshot, err := f.Backup(ctx, Manual)
	if err != nil {
		t.Fatal(err)
	}
	// Le dossier notes est effacé, la photo modifiée, un fichier ajouté.
	os.RemoveAll(filepath.Join(f.volume("cloud"), "data", "notes"))
	write(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg"), "photo v2")
	write(t, filepath.Join(f.volume("cloud"), "data", "nouveau.txt"), "à garder")
	f.fake.log = nil

	if err := f.CheckPutBack(ctx, snapshot.ID, "cloud", "pi-cloud_files/data/notes/liste.txt", false); err != nil {
		t.Fatal(err)
	}
	if err := f.PutBack(ctx, snapshot.ID, "cloud", "pi-cloud_files/data/notes/liste.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "notes", "liste.txt")); got != "lait, pain" {
		t.Fatalf("fichier remis : %q", got)
	}
	// Le dossier parent disparu revient avec ses droits d'origine.
	if info, err := os.Stat(filepath.Join(f.volume("cloud"), "data", "notes")); err != nil || info.Mode().Perm() != 0o750 {
		t.Fatalf("dossier parent : %v %v", info, err)
	}
	if !slices.Equal(f.fake.log, []string{"stop cloud", "start cloud"}) {
		t.Fatalf("service arrêté le temps d'écrire : %v", f.fake.log)
	}
	// La photo existe encore : il faut confirmer.
	if err := f.CheckPutBack(ctx, snapshot.ID, "cloud", "pi-cloud_files/data/photo.jpg", false); !errors.Is(err, ErrExists) {
		t.Fatalf("remplacement sans accord : %v", err)
	}
	// Un dossier entier : ce qui porte le même nom est remplacé, le reste gardé.
	if err := f.PutBack(ctx, snapshot.ID, "cloud", "pi-cloud_files/data"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "photo.jpg")); got != "photo v1" {
		t.Fatalf("photo : %q", got)
	}
	if got := read(t, filepath.Join(f.volume("cloud"), "data", "nouveau.txt")); got != "à garder" {
		t.Fatalf("fichier ajouté depuis : %q", got)
	}
	if last := f.Last(); last == nil || !last.OK || last.Action != "restore-file" {
		t.Fatalf("résultat : %+v", last)
	}
	for _, bad := range []string{"pi-cloud_files", "../x", ""} {
		if err := f.CheckPutBack(ctx, snapshot.ID, "cloud", bad, true); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q : %v", bad, err)
		}
	}
	// Service désinstallé : à restaurer en entier.
	os.RemoveAll(filepath.Join(f.fake.dir, "services", "cloud"))
	if err := f.CheckPutBack(ctx, snapshot.ID, "cloud", "pi-cloud_files/data/photo.jpg", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service désinstallé : %v", err)
	}
}

func TestBrowseNeedsPasswordFromAnotherCard(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	snapshot, err := f.Backup(ctx, Manual)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(f.fake.dir, stateDir))
	if err := f.Protect("nouveau"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Files(ctx, snapshot.ID, "cloud", ""); !errors.Is(err, ErrPassword) {
		t.Fatalf("sans mot de passe : %v", err)
	}
	if err := f.Unlock(ctx, snapshot.ID, "nouveau"); !errors.Is(err, ErrPassword) {
		t.Fatalf("mauvais mot de passe : %v", err)
	}
	if err := f.Unlock(ctx, snapshot.ID, "mot de passe administrateur"); err != nil {
		t.Fatal(err)
	}
	if got := names(f.files(t, snapshot.ID, "cloud", "pi-cloud_files/data")); !slices.Equal(got, []string{"photo.jpg"}) {
		t.Fatalf("après ouverture : %v", got)
	}
	recorder := httptest.NewRecorder()
	if err := f.Download(ctx, recorder, snapshot.ID, "cloud", "pi-cloud_files/data/photo.jpg"); err != nil || recorder.Code != http.StatusOK {
		t.Fatalf("%v %d", err, recorder.Code)
	}
}
