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

package diagnostic

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultDuration : la page de diagnostic reste ouverte 24 heures après la
// lecture du fichier diagnostic.
const DefaultDuration = 24 * time.Hour

// Window : le fichier diagnostic, déposé sur la partition de démarrage de la
// carte SD (comme reset, spécification 8.5), ouvre la page de diagnostic sans
// compte pendant Duration. Il est lu au démarrage de la console, puis
// supprimé : la fin de l'ouverture est gardée dans State.
type Window struct {
	File     string
	State    string
	Duration time.Duration
	Now      func() time.Time
}

func (w *Window) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Consume lit le fichier diagnostic : s'il existe, la page est ouverte jusqu'à
// maintenant + Duration, puis le fichier est supprimé.
func (w *Window) Consume() (bool, error) {
	if w.File == "" {
		return false, nil
	}
	if _, err := os.Stat(w.File); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	duration := w.Duration
	if duration <= 0 {
		duration = DefaultDuration
	}
	until := w.now().Add(duration).UTC().Format(time.RFC3339)
	if err := os.MkdirAll(filepath.Dir(w.State), 0o700); err != nil {
		return false, err
	}
	// NE PAS supprimer le fichier diagnostic avant d'avoir écrit State : une
	// coupure entre les deux perdrait l'ouverture.
	if err := os.WriteFile(w.State+".tmp", []byte(until+"\n"), 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(w.State+".tmp", w.State); err != nil {
		return false, err
	}
	return true, os.Remove(w.File)
}

// Until rend la fin de l'ouverture, si la page est ouverte par le fichier.
func (w *Window) Until() (time.Time, bool) {
	if w == nil || w.State == "" {
		return time.Time{}, false
	}
	data, err := os.ReadFile(w.State)
	if err != nil {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	if err != nil || !w.now().Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// Close ferme la page avant la fin des 24 heures.
func (w *Window) Close() error {
	if w == nil || w.State == "" {
		return nil
	}
	if err := os.Remove(w.State); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("fermeture du diagnostic : %w", err)
	}
	return nil
}
