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
	"context"
	"fmt"
	"strings"
)

// Accès aux données des services pour les sauvegardes (internal/backup,
// spécification 9.1).

// Volume : un volume Docker d'un service et son dossier sur le Pi.
type Volume struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Volumes rend les volumes du service, avec leur dossier.
func (m *Manager) Volumes(ctx context.Context, name string) ([]Volume, error) {
	if err := m.installed(name); err != nil {
		return nil, err
	}
	copies, err := m.serviceVolumes(ctx, name)
	if err != nil {
		return nil, err
	}
	volumes := make([]Volume, 0, len(copies))
	for _, c := range copies {
		volumes = append(volumes, Volume{Name: c.volume, Path: c.source})
	}
	return volumes, nil
}

// CreateVolume crée un volume du service (restauration d'un volume disparu),
// avec les étiquettes de Docker Compose pour qu'il le reprenne, et rend son dossier.
func (m *Manager) CreateVolume(ctx context.Context, name, volume string) (string, error) {
	project := "pi-" + name
	short, ok := strings.CutPrefix(volume, project+"_")
	if !ok || short == "" {
		return "", fmt.Errorf("%w : le volume %s n'est pas celui de %s", ErrInvalid, volume, name)
	}
	dir := m.serviceDir(name)
	if output, err := m.Runner.Run(ctx, dir, "volume", "create",
		"--label", "com.docker.compose.project="+project,
		"--label", "com.docker.compose.volume="+short, volume); err != nil {
		return "", fmt.Errorf("le volume %s n'a pas pu être créé : %s", volume, trimOutput(output, err))
	}
	return m.mountpoint(ctx, dir, volume)
}

// Running dit si une partie au moins du service est en marche.
func (m *Manager) Running(ctx context.Context, name string) bool {
	status, err := m.Status(ctx, name)
	return err == nil && status.Color != Red
}
