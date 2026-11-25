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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// Fichiers écrits par le constructeur des générateurs de sites statiques
// dans le dossier du site : état (première ligne), date (deuxième ligne) et
// journal de la dernière construction.
const (
	buildStateName = ".pi-appliance-build"
	BuildLogName   = ".pi-appliance-build.log"
)

// Build : dernière construction d'un site statique. State vaut building, ok
// ou failed.
type Build struct {
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

var buildStates = map[string]bool{"building": true, "ok": true, "failed": true}

// siteFiles : le manifeste installé déclare un dossier de fichiers (files).
func (m *Manager) siteFiles(name, appID string) bool {
	if !marketplace.ValidLabel(name) || appID == CustomApp {
		return false
	}
	data, err := os.ReadFile(filepath.Join(m.serviceDir(name), composeName))
	if err != nil {
		return false
	}
	app, err := marketplace.Parse(appID, data)
	return err == nil && app.SiteFiles
}

func readBuild(dir string) *Build {
	data, err := os.ReadFile(filepath.Join(dir, buildStateName))
	if err != nil || len(data) > 256 {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !buildStates[strings.TrimSpace(lines[0])] {
		return nil
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(lines[1]))
	if err != nil {
		return nil
	}
	return &Build{State: strings.TrimSpace(lines[0]), At: at}
}
