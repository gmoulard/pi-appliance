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
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Archives : tar compressé (gzip rapide, le Pi n'est pas un serveur) puis
// chiffré. Droits, propriétaires, dates et liens symboliques sont gardés ;
// les fichiers spéciaux (tubes, sockets, périphériques) sont ignorés.

// tree : un dossier à archiver sous un préfixe de l'archive.
type tree struct {
	dir    string
	prefix string
	// skip ignore un chemin relatif (avec des « / »), dossier compris.
	skip func(rel string) bool
}

// writeArchive écrit les dossiers dans une archive chiffrée vers w.
func writeArchive(w io.Writer, key []byte, trees []tree) error {
	encrypted, err := encrypt(w, key)
	if err != nil {
		return err
	}
	compressed, err := gzip.NewWriterLevel(encrypted, gzip.BestSpeed)
	if err != nil {
		return err
	}
	archive := tar.NewWriter(compressed)
	for _, t := range trees {
		if err := addTree(archive, t); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	if err := compressed.Close(); err != nil {
		return err
	}
	return encrypted.Close()
}

func addTree(archive *tar.Writer, t tree) error {
	return filepath.WalkDir(t.dir, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(t.dir, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && t.skip != nil && t.skip(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if !mode.IsRegular() && !mode.IsDir() && mode&fs.ModeSymlink == 0 {
			return nil
		}
		link := ""
		if mode&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(file); err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = path.Join(t.prefix, rel)
		if mode.IsDir() {
			header.Name += "/"
		}
		header.Format = tar.FormatPAX
		// Les noms d'utilisateurs n'existent pas forcément sur l'autre Pi : les numéros suffisent.
		header.Uname, header.Gname = "", ""
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if !mode.IsRegular() {
			return nil
		}
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		// La taille annoncée est celle du début : un fichier qui grandit
		// pendant la copie est coupé, plutôt que de casser l'archive.
		if _, err := io.CopyN(archive, f, header.Size); err != nil {
			return fmt.Errorf("%s : %w", file, err)
		}
		return nil
	})
}

// readArchive déchiffre une archive et rend chaque entrée à visit.
func readArchive(r io.Reader, key []byte, visit func(*tar.Header, io.Reader) error) error {
	plain, err := decrypt(r, key)
	if err != nil {
		return err
	}
	compressed, err := gzip.NewReader(plain)
	if err != nil {
		return corrupt(err)
	}
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return corrupt(err)
		}
		if err := visit(header, archive); err != nil {
			return corrupt(err)
		}
	}
	// Lire jusqu'au bout : l'authentification du dernier bloc est vérifiée là.
	if _, err := io.Copy(io.Discard, plain); err != nil {
		return corrupt(err)
	}
	return nil
}

func corrupt(err error) error {
	if errors.Is(err, ErrCorrupt) {
		return ErrCorrupt
	}
	return err
}

// entryPath : chemin relatif sûr d'une entrée sous prefix ; faux si l'entrée
// n'est pas sous prefix ou sort du dossier.
func entryPath(name, prefix string) (string, bool) {
	name = strings.TrimSuffix(name, "/")
	if name == prefix {
		return ".", true
	}
	rel, ok := strings.CutPrefix(name, prefix+"/")
	if !ok || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", false
	}
	return filepath.FromSlash(rel), true
}

// extractor écrit des entrées d'archive dans un dossier ; les droits des
// dossiers sont posés à la fin (un dossier en lecture seule bloquerait la suite).
type extractor struct {
	dir  string
	dirs []*tar.Header
	root bool
}

func newExtractor(dir string) *extractor {
	return &extractor{dir: dir, root: os.Geteuid() == 0}
}

func (x *extractor) add(header *tar.Header, rel string, r io.Reader) error {
	target := filepath.Join(x.dir, rel)
	switch header.Typeflag {
	case tar.TypeDir:
		if err := mkdirInside(x.dir, rel); err != nil {
			return err
		}
		h := *header
		h.Name = rel
		x.dirs = append(x.dirs, &h)
		return nil
	case tar.TypeReg:
		if err := mkdirInside(x.dir, filepath.Dir(rel)); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, r)
		if err == nil {
			err = x.owner(f.Chown, header)
		}
		if err == nil {
			err = f.Chmod(header.FileInfo().Mode().Perm() | header.FileInfo().Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky))
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		return os.Chtimes(target, header.ModTime, header.ModTime)
	case tar.TypeSymlink:
		if err := mkdirInside(x.dir, filepath.Dir(rel)); err != nil {
			return err
		}
		if err := os.Symlink(header.Linkname, target); err != nil {
			return err
		}
		return x.owner(func(uid, gid int) error { return os.Lchown(target, uid, gid) }, header)
	}
	return nil
}

// owner rend le propriétaire d'origine quand la console tourne en root.
func (x *extractor) owner(chown func(uid, gid int) error, header *tar.Header) error {
	if !x.root {
		return nil
	}
	return chown(header.Uid, header.Gid)
}

// finish pose droits, propriétaires et dates des dossiers, du plus profond au moins profond.
func (x *extractor) finish() error {
	for i := len(x.dirs) - 1; i >= 0; i-- {
		h := x.dirs[i]
		target := filepath.Join(x.dir, h.Name)
		if err := x.owner(func(uid, gid int) error { return os.Lchown(target, uid, gid) }, h); err != nil {
			return err
		}
		if err := os.Chmod(target, h.FileInfo().Mode().Perm()); err != nil {
			return err
		}
		mtime := h.ModTime
		if mtime.IsZero() {
			mtime = time.Now()
		}
		if err := os.Chtimes(target, mtime, mtime); err != nil {
			return err
		}
	}
	return nil
}

// mkdirInside crée le dossier rel sous root, morceau par morceau, en
// refusant de passer par un lien symbolique (il pourrait sortir de root).
func mkdirInside(root, rel string) error {
	dir := root
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "." || part == "" {
			continue
		}
		dir = filepath.Join(dir, part)
		info, err := os.Lstat(dir)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(dir, 0o700); err != nil {
				return err
			}
		case err != nil:
			return err
		case !info.IsDir():
			return fmt.Errorf("%s n'est pas un dossier", dir)
		}
	}
	return nil
}
