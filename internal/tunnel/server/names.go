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

package server

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Registry : noms et clés lus dans le registre du service d'adresses
// (registry.json, sur le même Pi du projet), relu quand il change. Le relais
// ne l'écrit jamais.
type Registry struct {
	Path string

	mu     sync.Mutex
	stamp  time.Time
	size   int64
	checks time.Time
	byName map[string]string
	byKey  map[string]string
}

// Owner : clé de l'appliance qui a réservé name.
func (r *Registry) Owner(name string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reload()
	key, ok := r.byName[name]
	return key, ok
}

// NameOf : nom réservé par l'appliance de clé key.
func (r *Registry) NameOf(key string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reload()
	name, ok := r.byKey[key]
	return name, ok
}

// reload relit le fichier s'il a changé, au plus une fois par seconde ; un
// fichier illisible garde la dernière lecture réussie.
func (r *Registry) reload() {
	if time.Since(r.checks) < time.Second && r.byName != nil {
		return
	}
	r.checks = time.Now()
	info, err := os.Stat(r.Path)
	if err != nil || (info.ModTime().Equal(r.stamp) && info.Size() == r.size && r.byName != nil) {
		return
	}
	data, err := os.ReadFile(r.Path)
	if err != nil {
		return
	}
	var registry struct {
		Names map[string]struct {
			Key string `json:"key"`
		} `json:"names"`
	}
	if json.Unmarshal(data, &registry) != nil {
		return
	}
	byName, byKey := map[string]string{}, map[string]string{}
	for name, entry := range registry.Names {
		if entry.Key == "" {
			continue
		}
		byName[name] = entry.Key
		byKey[entry.Key] = name
	}
	r.byName, r.byKey, r.stamp, r.size = byName, byKey, info.ModTime(), info.Size()
}
