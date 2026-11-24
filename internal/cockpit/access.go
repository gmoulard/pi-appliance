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

package cockpit

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Qui peut ouvrir Cockpit (TODO-3.4, décision de l'utilisateur du 2026-11-24).
// Par défaut, les administrateurs de la console, depuis la maison. Un
// administrateur peut l'ouvrir aussi depuis internet (seulement si la console
// y est ouverte, spécification 8.3), sans double authentification exigée, et
// aussi aux utilisateurs de la console (pas aux invités), qui ont alors tous
// les droits sur le Pi (sudo), comme les administrateurs.

// Access : réglage gardé sur le Pi (<données>/cockpit-access.json).
type Access struct {
	// Internet : Cockpit ouvert aussi depuis internet.
	Internet bool `json:"internet"`
	// Users : Cockpit ouvert aussi aux utilisateurs de la console.
	Users     bool      `json:"users"`
	ChangedBy string    `json:"changed_by,omitempty"`
	ChangedAt time.Time `json:"changed_at,omitzero"`
}

// AccessFile lit et écrit le réglage ; nil ou sans chemin : réglage par
// défaut (administrateurs, maison seulement), qui ne s'enregistre pas.
type AccessFile struct {
	Path string
	Logf func(format string, args ...any)

	mu sync.Mutex
}

// ErrNoAccessFile : pas de réglage possible ici (pas de chemin).
var ErrNoAccessFile = errors.New("réglage de l'accès à Cockpit indisponible")

// Get rend le réglage ; un fichier absent ou illisible vaut le réglage par défaut.
func (f *AccessFile) Get() Access {
	var access Access
	if f == nil || f.Path == "" {
		return access
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := os.ReadFile(f.Path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) && f.Logf != nil {
			f.Logf("Cockpit, accès : %v", err)
		}
		return access
	}
	if err := json.Unmarshal(data, &access); err != nil {
		if f.Logf != nil {
			f.Logf("Cockpit, accès : %s illisible : %v", f.Path, err)
		}
		return Access{}
	}
	return access
}

// Set enregistre le réglage (droits 600), en remplaçant le fichier d'un coup.
func (f *AccessFile) Set(access Access) error {
	if f == nil || f.Path == "" {
		return ErrNoAccessFile
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.MarshalIndent(access, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}
