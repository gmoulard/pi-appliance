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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gmoulard/pi-appliance/internal/system"
)

// Copie des données d'un service avant sa mise à jour (spécification 6.2) :
// chaque volume Docker du service est copié dans un volume de secours, sur le
// même disque, service arrêté ; le retour arrière les remet en place.

// ErrNoSpace : pas assez de place pour copier les données avant la mise à jour.
var ErrNoSpace = errors.New("pas assez de place")

// rollbackLabel marque les volumes de secours, pour les retrouver.
const rollbackLabel = "pi-appliance.rollback"

// Copier copie le contenu du dossier from dans le dossier to, vide.
type Copier interface {
	Copy(ctx context.Context, from, to string) error
}

// HostCopy copie avec « cp -a » : droits, propriétaires, liens et attributs
// gardés (la console tourne en root, section 10).
type HostCopy struct{}

func (HostCopy) Copy(ctx context.Context, from, to string) error {
	output, err := exec.CommandContext(ctx, "cp", "-a", from+"/.", to).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", trimOutput(output, err))
	}
	return nil
}

// volumeCopy : un volume du service et son volume de secours.
type volumeCopy struct {
	volume, backup string
	source, target string // dossiers des deux volumes sur le Pi
}

func (m *Manager) copier() Copier {
	if m.Copier != nil {
		return m.Copier
	}
	return HostCopy{}
}

// serviceVolumes : volumes créés par Docker Compose pour le service, avec leur dossier.
func (m *Manager) serviceVolumes(ctx context.Context, name string) ([]volumeCopy, error) {
	dir := m.serviceDir(name)
	output, err := m.Runner.Run(ctx, dir, "volume", "ls", "--quiet", "--filter", "label=com.docker.compose.project=pi-"+name)
	if err != nil {
		return nil, fmt.Errorf("les données de %s sont illisibles : %s", name, trimOutput(output, err))
	}
	var copies []volumeCopy
	for _, volume := range strings.Fields(string(output)) {
		source, err := m.mountpoint(ctx, dir, volume)
		if err != nil {
			return nil, err
		}
		copies = append(copies, volumeCopy{volume: volume, backup: "pi-rollback-" + volume, source: source})
	}
	return copies, nil
}

func (m *Manager) mountpoint(ctx context.Context, dir, volume string) (string, error) {
	output, err := m.Runner.Run(ctx, dir, "volume", "inspect", "--format", "{{.Mountpoint}}", volume)
	path := string(bytes.TrimSpace(output))
	if err != nil || !filepath.IsAbs(path) {
		return "", fmt.Errorf("le volume %s est introuvable : %s", volume, trimOutput(output, err))
	}
	return path, nil
}

// checkSpace refuse la mise à jour si le disque n'a pas la place de copier
// les données (plus 10 %), avant de rien toucher.
func (m *Manager) checkSpace(name string, copies []volumeCopy) error {
	if len(copies) == 0 {
		return nil
	}
	var size int64
	for _, c := range copies {
		size += dirSize(c.source)
	}
	needMB := (size + size/10) >> 20
	freeMB := m.FreeMB
	if freeMB == nil {
		freeMB = systemFreeMB
	}
	free, ok := freeMB(copies[0].source)
	if ok && needMB > free {
		return fmt.Errorf("%w : copier les données de %s avant la mise à jour demande %d Mo, il en reste %d sur le disque. Faites de la place (désinstallez un service, ou branchez un disque USB), puis réessayez", ErrNoSpace, name, needMB, free)
	}
	return nil
}

func systemFreeMB(dir string) (int64, bool) {
	if gauge := system.DiskAt(dir); gauge != nil {
		return gauge.FreeMB, true
	}
	return 0, false
}

// backup copie chaque volume dans son volume de secours ; le service doit être arrêté.
func (m *Manager) backup(ctx context.Context, name string, copies []volumeCopy) error {
	dir := m.serviceDir(name)
	for i := range copies {
		c := &copies[i]
		if output, err := m.Runner.Run(ctx, dir, "volume", "create", "--label", rollbackLabel+"="+name, c.backup); err != nil {
			m.dropBackups(ctx, name, copies[:i])
			return fmt.Errorf("copie des données impossible : %s", trimOutput(output, err))
		}
		target, err := m.mountpoint(ctx, dir, c.backup)
		if err == nil {
			// Un reste d'une mise à jour précédente est effacé.
			err = clearDir(target)
		}
		if err == nil {
			err = m.copier().Copy(ctx, c.source, target)
		}
		if err != nil {
			m.dropBackups(ctx, name, copies[:i+1])
			return fmt.Errorf("copie des données impossible : %v", err)
		}
		c.target = target
	}
	return nil
}

// restore remet les données copiées avant la mise à jour ; le service doit être arrêté.
func (m *Manager) restore(ctx context.Context, copies []volumeCopy) error {
	for _, c := range copies {
		if err := clearDir(c.source); err != nil {
			return fmt.Errorf("données de %s : %v", c.volume, err)
		}
		if err := m.copier().Copy(ctx, c.target, c.source); err != nil {
			return fmt.Errorf("données de %s : %v", c.volume, err)
		}
	}
	return nil
}

func (m *Manager) dropBackups(ctx context.Context, name string, copies []volumeCopy) {
	for _, c := range copies {
		m.Runner.Run(ctx, m.serviceDir(name), "volume", "rm", c.backup)
	}
}

// clearDir vide un dossier sans le supprimer (c'est le volume lui-même).
func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func dirSize(dir string) int64 {
	var size int64
	filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}
