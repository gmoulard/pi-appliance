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

// NoBackupLabel : étiquette Docker d'un volume qui n'est jamais sauvegardé,
// posée par le manifeste (section volumes du fichier Compose) avec la valeur
// « false » : par exemple le volume de Claude Code, qui garde le jeton du
// compte Claude (spécification 6.3, 9.1). Une étiquette du fichier Compose
// plutôt qu'une clé de x-pi-appliance : les consoles déjà publiées la lisent
// sans la refuser (règle de compatibilité, spécification 6.5).
const NoBackupLabel = "pi-appliance.backup"

// Volumes rend les volumes du service à sauvegarder, avec leur dossier : sans
// ceux marqués NoBackupLabel=false.
func (m *Manager) Volumes(ctx context.Context, name string) ([]Volume, error) {
	if err := m.installed(name); err != nil {
		return nil, err
	}
	copies, err := m.serviceVolumes(ctx, name)
	if err != nil {
		return nil, err
	}
	dir := m.serviceDir(name)
	output, err := m.Runner.Run(ctx, dir, "volume", "ls", "--quiet",
		"--filter", "label=com.docker.compose.project=pi-"+name,
		"--filter", "label="+NoBackupLabel+"=false")
	if err != nil {
		return nil, fmt.Errorf("les données de %s sont illisibles : %s", name, trimOutput(output, err))
	}
	excluded := map[string]bool{}
	for _, volume := range strings.Fields(string(output)) {
		excluded[volume] = true
	}
	volumes := make([]Volume, 0, len(copies))
	for _, c := range copies {
		if !excluded[c.volume] {
			volumes = append(volumes, Volume{Name: c.volume, Path: c.source})
		}
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
