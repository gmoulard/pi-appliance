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
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Destinations des sauvegardes (spécification 9.1) : le disque USB de la
// maison, un stockage en ligne compatible S3, un autre pi-appliance. Elles
// gardent les mêmes fichiers, déjà chiffrés ; celles qui quittent la maison
// (sealed) reçoivent en plus un manifest dont les noms des services sont
// chiffrés, et des noms de fichiers qui ne disent rien des services.

// Places des destinations, dans l'ordre de préférence pour restaurer.
const (
	PlaceDisk   = "disk"
	PlaceOnline = "online"
	PlacePeer   = "peer"
)

// fileWriter : un fichier en cours d'écriture dans une sauvegarde ; Close le
// termine, Abort l'abandonne.
type fileWriter interface {
	io.WriteCloser
	Abort()
}

// store : une destination. Une sauvegarde n'est complète qu'une fois son
// manifest écrit par commit : avant, c'est un brouillon (partial).
type store interface {
	place() string
	// sealed : la destination est hors de la maison.
	sealed() bool
	// list rend les sauvegardes complètes et les brouillons (sauvegardes interrompues).
	list(ctx context.Context) (complete, partial []string, err error)
	create(ctx context.Context, id, file string) (fileWriter, error)
	commit(ctx context.Context, id string, manifest []byte) error
	open(ctx context.Context, id, file string) (io.ReadCloser, error)
	// remove efface une sauvegarde, complète ou non.
	remove(ctx context.Context, id string) error
	// freeMB : place libre, faux si inconnue.
	freeMB() (int64, bool)
}

// filePattern : fichiers d'une sauvegarde (noms gardés par le manifest).
var filePattern = regexp.MustCompile(`^(manifest\.json|config\.enc|services/[A-Za-z0-9][A-Za-z0-9_.-]*\.enc)$`)

func validFile(file string) bool {
	return filePattern.MatchString(file) && !strings.Contains(file, "..")
}

// diskStore : un dossier du disque USB, une sauvegarde par sous-dossier
// <id>/, écrite dans <id>.partial/ puis renommée.
type diskStore struct {
	root string
	free func(dir string) (int64, bool)
}

func (d *diskStore) place() string { return PlaceDisk }
func (d *diskStore) sealed() bool  { return false }

func (d *diskStore) list(context.Context) (complete, partial []string, err error) {
	entries, err := os.ReadDir(d.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if id, ok := strings.CutSuffix(name, partialSuffix); ok && idPattern.MatchString(id) {
			partial = append(partial, id)
		} else if idPattern.MatchString(name) && exists(filepath.Join(d.root, name, manifestName)) {
			complete = append(complete, name)
		}
	}
	return complete, partial, nil
}

func (d *diskStore) create(_ context.Context, id, file string) (fileWriter, error) {
	if !idPattern.MatchString(id) || !validFile(file) {
		return nil, ErrInvalid
	}
	path := filepath.Join(d.root, id+partialSuffix, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &diskFile{File: f}, nil
}

type diskFile struct{ *os.File }

func (f *diskFile) Close() error {
	err := f.File.Sync()
	if closeErr := f.File.Close(); err == nil {
		err = closeErr
	}
	return err
}

func (f *diskFile) Abort() {
	f.File.Close()
	os.Remove(f.File.Name())
}

func (d *diskStore) commit(_ context.Context, id string, manifest []byte) error {
	partial := filepath.Join(d.root, id+partialSuffix)
	if err := os.MkdirAll(partial, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(partial, manifestName), manifest, 0o600); err != nil {
		return err
	}
	return os.Rename(partial, filepath.Join(d.root, id))
}

func (d *diskStore) open(_ context.Context, id, file string) (io.ReadCloser, error) {
	if !idPattern.MatchString(id) || !validFile(file) {
		return nil, ErrNotFound
	}
	f, err := os.Open(filepath.Join(d.root, id, filepath.FromSlash(file)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (d *diskStore) remove(_ context.Context, id string) error {
	if !idPattern.MatchString(id) {
		return ErrInvalid
	}
	return errors.Join(os.RemoveAll(filepath.Join(d.root, id)), os.RemoveAll(filepath.Join(d.root, id+partialSuffix)))
}

func (d *diskStore) freeMB() (int64, bool) {
	if err := os.MkdirAll(d.root, 0o700); err != nil {
		return 0, false
	}
	return d.free(d.root)
}

// copyFile copie un fichier d'une sauvegarde d'une destination à l'autre, tel quel.
func copyFile(ctx context.Context, from, to store, id, file, target string) error {
	r, err := from.open(ctx, id, file)
	if err != nil {
		return err
	}
	defer r.Close()
	w, err := to.create(ctx, id, target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Abort()
		return err
	}
	return w.Close()
}
