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
	"strconv"
	"strings"
)

const sourceName = "source.json"

// Source : le fichier Docker Compose libre tel qu'installé (TODO-2.82). URL :
// adresse où il a été lu ; Edited : modifié dans la console avant l'installation.
type Source struct {
	Compose string `json:"compose"`
	URL     string `json:"url,omitempty"`
	Edited  bool   `json:"edited,omitempty"`
	Service string `json:"service"`
	Port    int    `json:"port"`
}

type sourceFile struct {
	URL     string `json:"url,omitempty"`
	Edited  bool   `json:"edited,omitempty"`
	Service string `json:"service"`
	Port    int    `json:"port"`
}

// ComposeSource rend le fichier Docker Compose libre d'un service, l'adresse
// où il a été lu, le conteneur et le port ouverts. Un service installé avant
// source.json reprend le conteneur et le port des labels de Traefik.
func (m *Manager) ComposeSource(name string) (Source, error) {
	if _, err := m.customInstance(name); err != nil {
		return Source{}, err
	}
	dir := m.serviceDir(name)
	compose, err := os.ReadFile(filepath.Join(dir, composeName))
	if err != nil {
		return Source{}, err
	}
	var saved sourceFile
	data, err := os.ReadFile(filepath.Join(dir, sourceName))
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &saved); err != nil {
			return Source{}, fmt.Errorf("%s : %w", sourceName, err)
		}
	case errors.Is(err, os.ErrNotExist):
		saved.Service, _ = m.appService(name)
		saved.Port = m.publishedPort(name)
	default:
		return Source{}, err
	}
	return Source{Compose: string(compose), URL: saved.URL, Edited: saved.Edited, Service: saved.Service, Port: saved.Port}, nil
}

func (m *Manager) publishedPort(name string) int {
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), overrideName))
	if err != nil {
		return 0
	}
	var file struct {
		Services map[string]struct {
			Labels map[string]string `json:"labels"`
		} `json:"services"`
	}
	if json.Unmarshal(data, &file) != nil {
		return 0
	}
	for _, service := range file.Services {
		for key, value := range service.Labels {
			if strings.HasSuffix(key, ".loadbalancer.server.port") {
				port, _ := strconv.Atoi(value)
				return port
			}
		}
	}
	return 0
}

func (m *Manager) writeSource(name string, saved sourceFile) error {
	return writeJSON(filepath.Join(m.serviceDir(name), sourceName), saved, 0o600)
}
