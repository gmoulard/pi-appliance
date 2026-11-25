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
	"archive/tar"
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/services"
)

// Restauration d'un fichier ou d'un dossier en parcourant une sauvegarde
// (spécification 9.1) : la console lit l'archive d'un service entrée par
// entrée (sans rien écrire) pour en dresser la liste, puis rend un fichier
// à télécharger (un dossier : fichier .zip), ou le remet à sa place dans
// les données du service, service arrêté le temps de l'écrire.

// Entry : un fichier ou un dossier d'une sauvegarde. Path commence par le
// nom du volume (pi-<service>_<volume>).
type Entry struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// maxIndexEntries : au-delà, la liste est coupée (mémoire du Pi).
const maxIndexEntries = 300_000

// ErrPreparing : la liste des fichiers se prépare, redemander plus tard.
var ErrPreparing = errors.New("lecture de la sauvegarde en cours")

// fileIndex : la liste des fichiers de la dernière sauvegarde parcourue.
type fileIndex struct {
	id, service string
	mu          sync.Mutex
	entries     []Entry // triées par chemin
	count       int
	done        bool
	truncated   bool
	err         error
}

// Listing : le contenu d'un dossier d'une sauvegarde.
type Listing struct {
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"`
}

// entryKey : le chemin d'une entrée, sans « / » final ni « ./ ».
func entryKey(name string) (string, bool) {
	name = strings.TrimSuffix(path.Clean("/"+name), "/")
	name = strings.TrimPrefix(name, "/")
	if name == "" || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", false
	}
	return name, true
}

// cleanPath vérifie un chemin demandé par la console ; "" : la racine.
func cleanPath(p string) (string, bool) {
	if p == "" || p == "/" {
		return "", true
	}
	return entryKey(p)
}

// serviceArchive : destination, manifest et clé de l'archive d'un service.
func (m *Manager) serviceArchive(ctx context.Context, id, service string) (store, ServiceCopy, []byte, error) {
	if !idPattern.MatchString(id) || !validName(service) {
		return nil, ServiceCopy{}, nil, ErrInvalid
	}
	st, snapshot, err := m.find(ctx, id)
	if err != nil {
		return nil, ServiceCopy{}, nil, err
	}
	key, ok := m.knownKey(snapshot.Envelope)
	if !ok {
		return nil, ServiceCopy{}, nil, ErrPassword
	}
	if snapshot.Locked {
		if snapshot, err = unseal(snapshot, key); err != nil {
			return nil, ServiceCopy{}, nil, err
		}
	}
	saved, ok := snapshot.service(service)
	if !ok || len(saved.Volumes) == 0 {
		return nil, ServiceCopy{}, nil, fmt.Errorf("%w : %s n'a pas de données dans cette sauvegarde", ErrNotFound, service)
	}
	return st, saved, key, nil
}

// Unlock ouvre une sauvegarde faite sur une autre carte SD par le mot de
// passe administrateur en vigueur à sa date (gardé une heure en mémoire).
func (m *Manager) Unlock(ctx context.Context, id, password string) error {
	_, snapshot, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	_, err = m.snapshotKey(snapshot, password)
	return err
}

// Files rend le contenu du dossier dir de l'archive d'un service ; la
// première fois, la liste se prépare en arrière-plan (ErrPreparing, avec
// le nombre de fichiers déjà lus).
func (m *Manager) Files(ctx context.Context, id, service, dir string) (Listing, int, error) {
	dir, ok := cleanPath(dir)
	if !ok {
		return Listing{}, 0, ErrInvalid
	}
	m.cacheMu.Lock()
	index := m.index
	m.cacheMu.Unlock()
	if index != nil && (index.id != id || index.service != service || index.failed()) {
		index = nil
	}
	if index == nil {
		st, saved, key, err := m.serviceArchive(ctx, id, service)
		if err != nil {
			return Listing{}, 0, err
		}
		index = &fileIndex{id: id, service: service}
		m.cacheMu.Lock()
		m.index = index
		m.cacheMu.Unlock()
		go index.build(st, saved, key)
	}
	index.mu.Lock()
	defer index.mu.Unlock()
	if !index.done {
		return Listing{}, index.count, ErrPreparing
	}
	if index.err != nil {
		return Listing{}, 0, index.err
	}
	listing := Listing{Entries: []Entry{}, Truncated: index.truncated}
	for _, e := range index.entries {
		if path.Dir(e.Path) == dir || (dir == "" && !strings.Contains(e.Path, "/")) {
			listing.Entries = append(listing.Entries, e)
		}
	}
	if dir != "" && len(listing.Entries) == 0 && !slices.ContainsFunc(index.entries, func(e Entry) bool { return e.Path == dir && e.Dir }) {
		return Listing{}, 0, ErrNotFound
	}
	return listing, index.count, nil
}

// failed : la liste n'a pas pu se faire (disque débranché…) : à refaire.
func (x *fileIndex) failed() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.done && x.err != nil
}

func (x *fileIndex) build(st store, saved ServiceCopy, key []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	var entries []Entry
	truncated := false
	err := readFile(ctx, st, x.id, saved.file(), key, func(header *tar.Header, _ io.Reader) error {
		name, ok := entryKey(header.Name)
		if !ok || (header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg) {
			return nil
		}
		if len(entries) >= maxIndexEntries {
			truncated = true
			return nil
		}
		entries = append(entries, Entry{Path: name, Name: path.Base(name), Dir: header.Typeflag == tar.TypeDir, Size: header.Size, Modified: header.ModTime.UTC()})
		x.mu.Lock()
		x.count = len(entries)
		x.mu.Unlock()
		return nil
	})
	slices.SortFunc(entries, func(a, b Entry) int {
		if a.Dir != b.Dir {
			return boolInt(b.Dir) - boolInt(a.Dir)
		}
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})
	x.mu.Lock()
	defer x.mu.Unlock()
	x.entries, x.truncated, x.err, x.done = entries, truncated, err, true
}

// under : vrai si name est target ou sous target.
func under(name, target string) bool {
	return name == target || strings.HasPrefix(name, target+"/")
}

// Download écrit dans w le fichier p de l'archive d'un service, ou le
// dossier p en .zip. Les en-têtes ne partent qu'une fois l'entrée trouvée :
// introuvable, la réponse reste à faire (ErrNotFound).
func (m *Manager) Download(ctx context.Context, w http.ResponseWriter, id, service, p string) error {
	target, ok := cleanPath(p)
	if !ok || target == "" {
		return ErrInvalid
	}
	st, saved, key, err := m.serviceArchive(ctx, id, service)
	if err != nil {
		return err
	}
	var archive *zip.Writer
	sent := false
	errDone := errors.New("fin")
	err = readFile(ctx, st, id, saved.file(), key, func(header *tar.Header, r io.Reader) error {
		name, ok := entryKey(header.Name)
		if !ok || !under(name, target) {
			if archive != nil && !under(name, target) && sent {
				// Les entrées d'un dossier se suivent : le dossier est fini.
				return errDone
			}
			return nil
		}
		switch {
		case name == target && header.Typeflag == tar.TypeReg:
			attachment(w, path.Base(target), "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprint(header.Size))
			sent = true
			if _, err := io.Copy(w, r); err != nil {
				return err
			}
			return errDone
		case name == target && header.Typeflag == tar.TypeDir:
			attachment(w, path.Base(target)+".zip", "application/zip")
			sent = true
			archive = zip.NewWriter(w)
			return nil
		case archive != nil:
			rel := strings.TrimPrefix(name, target+"/")
			zipHeader := &zip.FileHeader{Name: path.Join(path.Base(target), rel), Modified: header.ModTime, Method: zip.Deflate}
			if header.Typeflag == tar.TypeDir {
				zipHeader.Name += "/"
				zipHeader.Method = zip.Store
				_, err := archive.CreateHeader(zipHeader)
				return err
			}
			if header.Typeflag != tar.TypeReg {
				return nil
			}
			zipHeader.SetMode(header.FileInfo().Mode())
			out, err := archive.CreateHeader(zipHeader)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, r)
			return err
		}
		return nil
	})
	if errors.Is(err, errDone) {
		err = nil
	}
	if archive != nil {
		err = errors.Join(err, archive.Close())
	}
	if !sent && err == nil {
		return ErrNotFound
	}
	if sent && err != nil {
		// La réponse est partie : on ne peut plus que la couper.
		m.logf("téléchargement depuis la sauvegarde %s : %v", id, err)
		panic(http.ErrAbortHandler)
	}
	return err
}

func attachment(w http.ResponseWriter, name, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlPathEscape(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
}

func urlPathEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || strings.IndexByte("-_.~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ErrExists : le fichier à remettre existe déjà ; Replace pour le remplacer.
var ErrExists = errors.New("ce fichier existe encore : confirmez pour le remplacer par celui de la sauvegarde")

// volumePath : le dossier actuel du volume qui porte target (premier
// morceau du chemin) et le chemin dans ce volume.
func (m *Manager) volumePath(ctx context.Context, service, target string) (string, string, error) {
	volume, rel, _ := strings.Cut(target, "/")
	current, err := m.Services.Volumes(ctx, service)
	if err != nil {
		return "", "", err
	}
	for _, v := range current {
		if v.Name == volume {
			return v.Path, rel, nil
		}
	}
	return "", "", fmt.Errorf("%w : %s n'a plus ces données ; restaurez-le en entier", ErrNotFound, service)
}

// CheckPutBack vérifie qu'un fichier peut être remis : sauvegarde lisible,
// service installé avec ce volume ; ErrExists si le fichier est encore là.
func (m *Manager) CheckPutBack(ctx context.Context, id, service, p string, replace bool) error {
	target, ok := cleanPath(p)
	if !ok || !strings.Contains(target, "/") {
		return fmt.Errorf("%w : choisissez un fichier ou un dossier", ErrInvalid)
	}
	if _, _, _, err := m.serviceArchive(ctx, id, service); err != nil {
		return err
	}
	if installed, err := m.installed(service); err != nil || !installed {
		return fmt.Errorf("%w : %s n'est plus installé ; restaurez-le en entier", ErrNotFound, service)
	}
	dir, rel, err := m.volumePath(ctx, service, target)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel))); err == nil && !replace {
		return ErrExists
	}
	return nil
}

// PutBack remet le fichier ou le dossier p de la sauvegarde id à sa place
// dans les données du service : ce qui porte le même nom est remplacé, le
// reste du dossier gardé. Le service est arrêté le temps de l'écrire.
func (m *Manager) PutBack(ctx context.Context, id, service, p string) (err error) {
	if !m.begin("restore-file") {
		return ErrBusy
	}
	defer m.end()
	target, _ := cleanPath(p)
	defer func() {
		outcome := Outcome{Action: "restore-file", Service: service, OK: err == nil, Message: fmt.Sprintf("%s est remis en place dans %s.", path.Base(target), service)}
		if err != nil {
			outcome.Message = err.Error()
			m.logf("remise en place de %s (%s) : %v", target, service, err)
		}
		m.record(outcome)
	}()
	if err := m.CheckPutBack(ctx, id, service, p, true); err != nil {
		return err
	}
	st, saved, key, err := m.serviceArchive(ctx, id, service)
	if err != nil {
		return err
	}
	volume, _, _ := strings.Cut(target, "/")
	dir, _, err := m.volumePath(ctx, service, target)
	if err != nil {
		return err
	}
	running := m.Services.Running(ctx, service)
	if running {
		if err := m.Services.Control(ctx, service, services.Stop); err != nil {
			return err
		}
		defer func() {
			start, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
			defer cancel()
			if startErr := m.Services.Control(start, service, services.Start); startErr != nil {
				err = errors.Join(err, startErr)
			}
		}()
	}
	x := newExtractor(dir)
	found := false
	err = readFile(ctx, st, id, saved.file(), key, func(header *tar.Header, r io.Reader) error {
		name, ok := entryKey(header.Name)
		if !ok {
			return nil
		}
		rel, ok := entryPath(name, volume)
		if !ok || rel == "." {
			return nil
		}
		destination := filepath.Join(dir, rel)
		info, statErr := os.Lstat(destination)
		switch {
		case under(name, target):
			found = true
			// Remplacer ce qui porte le même nom, sauf un dossier par un dossier (fusion).
			if statErr == nil && !(info.IsDir() && header.Typeflag == tar.TypeDir) {
				if err := os.RemoveAll(destination); err != nil {
					return err
				}
			}
			return x.add(header, rel, r)
		case header.Typeflag == tar.TypeDir && strings.HasPrefix(target, name+"/") && errors.Is(statErr, os.ErrNotExist):
			// Dossier parent disparu : recréé avec ses droits d'origine.
			return x.add(header, rel, r)
		}
		return nil
	})
	if err == nil {
		err = x.finish()
	}
	if err == nil && !found {
		err = ErrNotFound
	}
	return err
}
