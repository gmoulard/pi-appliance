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

package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// EraseTags efface la clé tags des instance.json (TODO-5.28) et rend les
// services dont les étiquettes ont été effacées.
func (m *Manager) EraseTags() ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(m.Dir, "services", "*", stateName))
	if err != nil {
		return nil, err
	}
	erased := []string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return erased, err
		}
		state := map[string]json.RawMessage{}
		if err := json.Unmarshal(data, &state); err != nil {
			return erased, fmt.Errorf("%s : %w", path, err)
		}
		if _, ok := state["tags"]; !ok {
			continue
		}
		delete(state, "tags")
		info, err := os.Stat(path)
		if err != nil {
			return erased, err
		}
		if err := writeJSON(path, state, info.Mode().Perm()); err != nil {
			return erased, err
		}
		erased = append(erased, filepath.Base(filepath.Dir(path)))
	}
	return erased, nil
}
