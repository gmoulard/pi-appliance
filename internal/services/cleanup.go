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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// systemTrees : dossiers du Pi que « Rebuild cleanup » ne vide jamais, ni eux
// ni rien dedans ; les dossiers /lib* s'y ajoutent.
var systemTrees = []string{"/bin", "/boot", "/dev", "/etc", "/proc", "/root", "/run", "/sbin", "/sys", "/usr", "/var/lib/containerd", "/var/lib/docker", "/var/run"}

// sharedRoots : dossiers du Pi que « Rebuild cleanup » ne vide pas, ni leurs
// dossiers immédiats (/home/pi, /mnt/usb, /var/log…) ; plus bas, c'est permis.
var sharedRoots = []string{"/home", "/media", "/mnt", "/opt", "/srv", "/var", "/var/lib"}

// CleanupDirs : dossiers du Pi montés par un chemin absolu dans le compose.yaml
// d'un Docker Compose libre, dont « Rebuild cleanup » vide le contenu ; un
// dossier qui n'existe pas encore n'y est pas. kept : prises montées (comme
// /var/run/docker.sock pour Portainer), laissées telles quelles (décision du
// 2026-11-26). Un dossier protégé fait refuser tout le Rebuild cleanup
// (ErrInvalid), avant que rien ne soit effacé.
func (m *Manager) CleanupDirs(name string) (dirs, kept []string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.customInstance(name); err != nil {
		return nil, nil, err
	}
	return m.cleanupDirs(name)
}

func (m *Manager) cleanupDirs(name string) ([]string, []string, error) {
	own, err := composeMounts(m.serviceDir(name))
	if err != nil {
		return nil, nil, err
	}
	others, err := m.otherMounts(name)
	if err != nil {
		return nil, nil, err
	}
	dirs, kept, refused := []string{}, []string{}, []string(nil)
	for _, source := range own {
		clean := filepath.Clean(source)
		// NE PAS passer une prise par protected : /var/run/docker.sock est dans
		// un dossier système et ferait tout refuser ; elle n'est jamais vidée.
		if isSocket(clean) {
			if !slices.Contains(kept, clean) {
				kept = append(kept, clean)
			}
			continue
		}
		resolved, reason := m.cleanupRefusal(name, clean, others)
		switch {
		case reason != "":
			refused = append(refused, reason)
		case resolved != "" && !slices.Contains(dirs, resolved):
			dirs = append(dirs, resolved)
		}
	}
	if len(refused) > 0 {
		return nil, nil, fmt.Errorf("%w : Réinitialisation d'usine de %s refusée, rien n'a été effacé : %s", ErrInvalid, name, strings.Join(refused, " ; "))
	}
	sort.Strings(dirs)
	sort.Strings(kept)
	return dirs, kept, nil
}

// isSocket : path est une prise (lien suivi).
func isSocket(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0
}

// cleanupRefusal rend le dossier à vider (lien suivi), vide s'il n'existe pas,
// ou la raison du refus.
func (m *Manager) cleanupRefusal(name, source string, others map[string]string) (string, string) {
	clean := filepath.Clean(source)
	if reason := m.protected(name, clean, others); reason != "" {
		return "", reason
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if errors.Is(err, os.ErrNotExist) {
		return "", ""
	}
	if err != nil {
		return "", fmt.Sprintf("le dossier %s du Pi est illisible (%v)", clean, err)
	}
	if resolved != clean {
		if reason := m.protected(name, resolved, others); reason != "" {
			return "", fmt.Sprintf("%s est un lien vers %s : %s", clean, resolved, reason)
		}
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Sprintf("le dossier %s du Pi est illisible (%v)", clean, err)
	}
	if !info.IsDir() {
		return "", fmt.Sprintf("%s n'est pas un dossier (c'est un fichier) : il n'est jamais effacé", clean)
	}
	return resolved, ""
}

// protected : raison pour laquelle le dossier dir (chemin absolu propre) ne
// doit pas être vidé, vide sinon.
func (m *Manager) protected(name, dir string, others map[string]string) string {
	if dir == "/" {
		return "/ est tout le système du Pi"
	}
	top := "/" + strings.SplitN(strings.TrimPrefix(dir, "/"), "/", 2)[0]
	if strings.HasPrefix(top, "/lib") {
		return fmt.Sprintf("%s est un dossier système du Pi", dir)
	}
	for _, tree := range systemTrees {
		if within(dir, tree) {
			return fmt.Sprintf("%s est un dossier système du Pi", dir)
		}
	}
	for _, root := range sharedRoots {
		if dir == root || filepath.Dir(dir) == root {
			return fmt.Sprintf("%s est un dossier commun du Pi, pas celui d'un seul service", dir)
		}
	}
	for _, data := range m.dataDirs() {
		serviceDir := filepath.Join(data, "services", name)
		if within(data, dir) || (within(dir, data) && !strings.HasPrefix(dir, serviceDir+"/")) {
			return fmt.Sprintf("%s est dans les données de pi-appliance, hors du dossier de %s", dir, name)
		}
	}
	for mount, other := range others {
		if within(dir, mount) || within(mount, dir) {
			return fmt.Sprintf("%s est aussi monté par le service %s", dir, other)
		}
	}
	return ""
}

// dataDirs : dossier des données de pi-appliance, tel quel et lien suivi.
func (m *Manager) dataDirs() []string {
	data, err := filepath.Abs(m.Dir)
	if err != nil {
		data = filepath.Clean(m.Dir)
	}
	dirs := []string{data}
	if resolved, err := filepath.EvalSymlinks(data); err == nil && resolved != data {
		dirs = append(dirs, resolved)
	}
	return dirs
}

// within : dir est base ou un dossier dedans.
func within(dir, base string) bool {
	return dir == base || strings.HasPrefix(dir, strings.TrimSuffix(base, "/")+"/")
}

// composeMounts : sources absolues des dossiers du Pi montés par le compose.yaml
// d'un dossier de service.
func composeMounts(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, composeName))
	if err != nil {
		return nil, err
	}
	var file riskFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%w : le fichier Docker Compose est illisible : %v", ErrInvalid, err)
	}
	var sources []string
	for _, service := range file.Services {
		for _, volume := range service.Volumes {
			if source, _, ok := bindSource(volume); ok && strings.HasPrefix(source, "/") {
				sources = append(sources, source)
			}
		}
	}
	sort.Strings(sources)
	return sources, nil
}

// otherMounts : dossiers du Pi montés par les autres services (compose.yaml et
// réglages avancés), chemin tel quel et lien suivi, avec le nom du service.
func (m *Manager) otherMounts(name string) (map[string]string, error) {
	instances, err := m.List()
	if err != nil {
		return nil, err
	}
	mounts := map[string]string{}
	for _, instance := range instances {
		if instance.Name == name {
			continue
		}
		dir := m.serviceDir(instance.Name)
		sources, err := composeMounts(dir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("les dossiers montés par %s sont illisibles : %w", instance.Name, err)
		}
		var settings Advanced
		if data, err := os.ReadFile(filepath.Join(dir, advancedSettings)); err == nil {
			if err := json.Unmarshal(data, &settings); err != nil {
				return nil, fmt.Errorf("les réglages avancés de %s sont illisibles : %w", instance.Name, err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, mount := range settings.Volumes {
			if strings.HasPrefix(mount.Source, "/") {
				sources = append(sources, mount.Source)
			}
		}
		for _, source := range sources {
			clean := filepath.Clean(source)
			mounts[clean] = instance.Name
			if resolved, err := filepath.EvalSymlinks(clean); err == nil {
				mounts[resolved] = instance.Name
			}
		}
	}
	return mounts, nil
}
