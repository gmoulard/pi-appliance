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
