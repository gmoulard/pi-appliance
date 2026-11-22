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
	"fmt"
	"path/filepath"
	"slices"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// SetTags garde les étiquettes d'un service installé (de 0 à
// marketplace.MaxTags, prises dans marketplace.Tags), pour le ranger dans la
// console (TODO-5.22). La console ne le permet que pour un service hors
// catalogue : les autres prennent les étiquettes de leur application.
func (m *Manager) SetTags(name string, tags []string) (Instance, error) {
	if len(tags) > marketplace.MaxTags {
		return Instance{}, fmt.Errorf("%w : %d étiquettes au plus", ErrInvalid, marketplace.MaxTags)
	}
	for i, tag := range tags {
		if !marketplace.KnownTag(tag) {
			return Instance{}, fmt.Errorf("%w : étiquette « %s » inconnue", ErrInvalid, tag)
		}
		if slices.Contains(tags[:i], tag) {
			return Instance{}, fmt.Errorf("%w : étiquette « %s » en double", ErrInvalid, tag)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	instance, err := m.instance(name)
	if err != nil {
		return Instance{}, err
	}
	instance.Tags = nil
	if len(tags) > 0 {
		instance.Tags = slices.Clone(tags)
	}
	if err := writeJSON(filepath.Join(m.serviceDir(name), stateName), instance, 0o600); err != nil {
		return Instance{}, err
	}
	return instance, nil
}
