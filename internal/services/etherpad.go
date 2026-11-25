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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Etherpad et ses liens de partage (spécification 6.3, 8.2). En accès
// « utilisateurs connectés », le routeur passe par le middleware
// pi-appliance-pad, qui accepte aussi un lien de partage pour un seul document,
// et Etherpad exige une connexion (REQUIRE_AUTHENTICATION) : la console la lui
// donne par l'en-tête Authorization, avec le compte interne « user ». Etherpad
// n'ouvre alors par sa connexion temps réel (socket.io) que les documents dont
// la page /p/<document> a été ouverte dans la même session : c'est lui qui
// tient la limite au document partagé, le middleware ne voyant pas les messages.
const (
	EtherpadApp       = "etherpad"
	padMiddleware     = "pi-appliance-pad@file"
	etherpadUserParam = "ETHERPAD_USER_PASSWORD"
)

// etherpadEnvironment : Etherpad exige la connexion seulement derrière pi-appliance-pad.
func etherpadEnvironment(access string) map[string]any {
	return map[string]any{"REQUIRE_AUTHENTICATION": fmt.Sprint(access == AccessUsers)}
}

// ShareReady dit si les liens de partage marchent pour ce service : un
// Etherpad en accès « utilisateurs connectés », installé ou mis à jour depuis
// l'arrivée des liens (mot de passe du compte interne présent).
func (m *Manager) ShareReady(name string) (Instance, error) {
	instance, err := m.instance(name)
	if err != nil {
		return Instance{}, err
	}
	if instance.App != EtherpadApp {
		return Instance{}, fmt.Errorf("%w : %s n'est pas Etherpad, ses documents ne se partagent pas ici", ErrInvalid, name)
	}
	if instance.Access != AccessUsers {
		return Instance{}, fmt.Errorf("%w : les liens de partage servent quand %s est réservé aux personnes qui ont un compte", ErrInvalid, name)
	}
	if _, ok := m.etherpadAuthorization(name); !ok {
		return Instance{}, fmt.Errorf("%w : mettez d'abord %s à jour pour partager ses documents", ErrInvalid, name)
	}
	return instance, nil
}

// EtherpadLogin : pour l'adresse host d'un Etherpad en accès « utilisateurs
// connectés », le nom du service et l'en-tête Authorization que Traefik
// transmet à Etherpad.
// NE PAS déduire le nom du service de la première partie de l'adresse : à la
// maison, elle vaut <service>-<nom du Pi> (ServiceAddress), pas <service>.
func (m *Manager) EtherpadLogin(host string) (string, string, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	instances, err := m.List()
	if err != nil || host == "" {
		return "", "", false
	}
	for _, instance := range instances {
		if instance.App != EtherpadApp || instance.Access != AccessUsers || !strings.EqualFold(instance.Address, host) {
			continue
		}
		authorization, ok := m.etherpadAuthorization(instance.Name)
		if !ok {
			return "", "", false
		}
		return instance.Name, authorization, true
	}
	return "", "", false
}

func (m *Manager) etherpadAuthorization(name string) (string, bool) {
	env, err := readEnv(filepath.Join(m.serviceDir(name), envName))
	if err != nil || env[etherpadUserParam] == "" {
		return "", false
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("user:"+env[etherpadUserParam])), true
}

func (m *Manager) instance(name string) (Instance, error) {
	if err := m.installed(name); err != nil {
		return Instance{}, err
	}
	path := filepath.Join(m.serviceDir(name), stateName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Instance{}, err
	}
	var instance Instance
	if err := json.Unmarshal(data, &instance); err != nil {
		return Instance{}, fmt.Errorf("%s : %w", path, err)
	}
	if !instance.NoWebPage && instance.Access == "" {
		instance.Access = AccessPublic
	}
	return instance, nil
}
