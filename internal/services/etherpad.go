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
func (m *Manager) EtherpadLogin(host string) (string, string, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	name, _, _ := strings.Cut(host, ".")
	instance, err := m.instance(name)
	if err != nil || instance.App != EtherpadApp || instance.Access != AccessUsers || !strings.EqualFold(instance.Address, host) {
		return "", "", false
	}
	authorization, ok := m.etherpadAuthorization(name)
	if !ok {
		return "", "", false
	}
	return name, authorization, true
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
