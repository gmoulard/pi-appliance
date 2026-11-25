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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Réglages avancés d'un service installé (spécification 6.4) : variables,
// ports et volumes d'un de ses conteneurs, dans un fichier Compose de plus,
// lu après le manifeste et les labels Traefik.

const (
	advancedName     = "pi-appliance.advanced.json"
	advancedSettings = "advanced.json"
	maxVariables     = 100
)

// ErrSettingsRestored : les nouveaux réglages avancés empêchent le service de
// démarrer ; les anciens sont remis.
var ErrSettingsRestored = errors.New("réglages refusés")

// Port : un port du Pi qui mène directement à un port du conteneur, sans Traefik.
type Port struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol"`
}

// Mount : un dossier du Pi (chemin absolu) ou un volume nommé, monté dans le conteneur.
type Mount struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	ReadOnly bool   `json:"read_only"`
}

// Advanced : réglages avancés d'un service, appliqués au conteneur Service.
type Advanced struct {
	Service     string            `json:"service"`
	Environment map[string]string `json:"environment"`
	Ports       []Port            `json:"ports"`
	Volumes     []Mount           `json:"volumes"`
}

var (
	variableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	volumeName   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,62}$`)
	// reservedPorts : Traefik (80, 443) et la console (8080).
	reservedPorts = map[int]bool{80: true, 443: true, 8080: true}
)

func (a Advanced) empty() bool {
	return len(a.Environment) == 0 && len(a.Ports) == 0 && len(a.Volumes) == 0
}

// validate refuse ce que Docker Compose ne saurait pas lire et rend les
// options dangereuses (dossiers du Pi montés).
func (a *Advanced) validate(services []string) ([]Risk, error) {
	if a.empty() {
		return nil, nil
	}
	known := false
	for _, name := range services {
		known = known || name == a.Service
	}
	if !known {
		return nil, fmt.Errorf("%w : le conteneur « %s » n'existe pas dans ce service (%s)", ErrInvalid, a.Service, strings.Join(services, ", "))
	}
	if len(a.Environment) > maxVariables {
		return nil, fmt.Errorf("%w : %d variables au plus", ErrInvalid, maxVariables)
	}
	for key, value := range a.Environment {
		if !variableName.MatchString(key) {
			return nil, fmt.Errorf("%w : le nom de variable « %s » ne convient pas (lettres, chiffres et _)", ErrInvalid, key)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%w : la variable %s ne doit pas contenir de retour à la ligne", ErrInvalid, key)
		}
	}
	seen := map[string]bool{}
	for i := range a.Ports {
		p := &a.Ports[i]
		if p.Protocol == "" {
			p.Protocol = "tcp"
		}
		if p.Protocol != "tcp" && p.Protocol != "udp" {
			return nil, fmt.Errorf("%w : protocole « %s » inconnu (tcp ou udp)", ErrInvalid, p.Protocol)
		}
		if p.Host < 1 || p.Host > 65535 || p.Container < 1 || p.Container > 65535 {
			return nil, fmt.Errorf("%w : un port va de 1 à 65535", ErrInvalid)
		}
		if reservedPorts[p.Host] {
			return nil, fmt.Errorf("%w : le port %d du Pi est réservé (80 et 443 : Traefik, 8080 : la console)", ErrInvalid, p.Host)
		}
		key := strconv.Itoa(p.Host) + "/" + p.Protocol
		if seen[key] {
			return nil, fmt.Errorf("%w : le port %s du Pi est donné deux fois", ErrInvalid, key)
		}
		seen[key] = true
	}
	var risks []Risk
	for _, mount := range a.Volumes {
		if !strings.HasPrefix(mount.Target, "/") || strings.ContainsAny(mount.Target, ":,\r\n") {
			return nil, fmt.Errorf("%w : le dossier du conteneur « %s » doit être un chemin absolu", ErrInvalid, mount.Target)
		}
		switch {
		case strings.HasPrefix(mount.Source, "/"):
			if strings.ContainsAny(mount.Source, ":,\r\n") || path.Clean(mount.Source) != mount.Source {
				return nil, fmt.Errorf("%w : le dossier du Pi « %s » ne convient pas", ErrInvalid, mount.Source)
			}
			risks = append(risks, mountRisk(a.Service, mount.Source, mount.ReadOnly))
		case volumeName.MatchString(mount.Source):
		default:
			return nil, fmt.Errorf("%w : « %s » n'est ni un dossier du Pi (chemin absolu) ni un nom de volume", ErrInvalid, mount.Source)
		}
	}
	return risks, nil
}

// composeOverride : le fichier Compose des réglages avancés.
func (a Advanced) composeOverride() map[string]any {
	service := map[string]any{}
	if len(a.Environment) > 0 {
		service["environment"] = a.Environment
	}
	if len(a.Ports) > 0 {
		ports := make([]string, 0, len(a.Ports))
		for _, p := range a.Ports {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", p.Host, p.Container, p.Protocol))
		}
		service["ports"] = ports
	}
	named := map[string]any{}
	if len(a.Volumes) > 0 {
		volumes := make([]string, 0, len(a.Volumes))
		for _, mount := range a.Volumes {
			entry := mount.Source + ":" + mount.Target
			if mount.ReadOnly {
				entry += ":ro"
			}
			volumes = append(volumes, entry)
			if !strings.HasPrefix(mount.Source, "/") {
				named[mount.Source] = map[string]any{}
			}
		}
		service["volumes"] = volumes
	}
	file := map[string]any{"services": map[string]any{a.Service: service}}
	if len(named) > 0 {
		file["volumes"] = named
	}
	return file
}

// composeServices : noms des conteneurs du manifeste installé.
func (m *Manager) composeServices(name string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), composeName))
	if err != nil {
		return nil, err
	}
	names, _, err := AnalyzeCompose(data)
	return names, err
}

// AdvancedSettings rend les réglages avancés d'un service et les noms de ses conteneurs.
func (m *Manager) AdvancedSettings(name string) (Advanced, []string, error) {
	if err := m.installed(name); err != nil {
		return Advanced{}, nil, err
	}
	services, err := m.composeServices(name)
	if err != nil {
		return Advanced{}, nil, err
	}
	settings := Advanced{Environment: map[string]string{}, Ports: []Port{}, Volumes: []Mount{}}
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), advancedSettings))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Advanced{}, nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return Advanced{}, nil, err
		}
	}
	if !contains(services, settings.Service) && len(services) > 0 {
		settings.Service = services[0]
		if app, ok := m.appService(name); ok && contains(services, app) {
			settings.Service = app
		}
	}
	return settings, services, nil
}

// appService : conteneur publié par Traefik, d'après les labels posés à l'installation.
func (m *Manager) appService(name string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), overrideName))
	if err != nil {
		return "", false
	}
	var file struct {
		Services map[string]any `json:"services"`
	}
	if json.Unmarshal(data, &file) != nil || len(file.Services) == 0 {
		return "", false
	}
	names := make([]string, 0, len(file.Services))
	for service := range file.Services {
		names = append(names, service)
	}
	sort.Strings(names)
	return names[0], true
}

// SetAdvanced applique de nouveaux réglages avancés et redémarre le service ;
// s'il ne redémarre pas, les réglages d'avant sont remis. Les options
// dangereuses doivent être confirmées (confirm).
func (m *Manager) SetAdvanced(ctx context.Context, name string, settings Advanced, confirm bool) ([]Risk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.installed(name); err != nil {
		return nil, err
	}
	services, err := m.composeServices(name)
	if err != nil {
		return nil, err
	}
	risks, err := settings.validate(services)
	if err != nil {
		return nil, err
	}
	if len(risks) > 0 && !confirm {
		return nil, RisksError{Risks: risks}
	}
	dir := m.serviceDir(name)
	previous, _ := os.ReadFile(filepath.Join(dir, advancedName))
	previousSettings, _ := os.ReadFile(filepath.Join(dir, advancedSettings))
	if err := m.writeAdvanced(dir, settings); err != nil {
		return nil, err
	}
	output, err := m.compose(ctx, name, "up", "--detach", "--wait", "--remove-orphans")
	if err == nil {
		return risks, nil
	}
	failure := trimOutput(output, err)
	restoreFile(filepath.Join(dir, advancedName), previous)
	restoreFile(filepath.Join(dir, advancedSettings), previousSettings)
	if output, err := m.compose(context.WithoutCancel(ctx), name, "up", "--detach", "--wait", "--remove-orphans"); err != nil {
		return nil, fmt.Errorf("les nouveaux réglages n'ont pas marché (%s), et le retour aux anciens non plus : %s", failure, trimOutput(output, err))
	}
	return nil, fmt.Errorf("%w : %s", ErrSettingsRestored, failure)
}

func (m *Manager) writeAdvanced(dir string, settings Advanced) error {
	if settings.empty() {
		os.Remove(filepath.Join(dir, advancedName))
		err := os.Remove(filepath.Join(dir, advancedSettings))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := writeJSON(filepath.Join(dir, advancedSettings), settings, 0o600); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, advancedName), settings.composeOverride(), 0o600)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// restoreFile remet un fichier dans son état d'avant ; nil : il n'existait pas.
func restoreFile(path string, data []byte) {
	if data == nil {
		os.Remove(path)
		return
	}
	os.WriteFile(path, data, 0o600)
}
