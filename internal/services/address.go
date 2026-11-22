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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxLabel : longueur maximale d'une partie d'un nom sur le réseau (RFC 1035).
const maxLabel = 63

// ServiceAddress : adresse d'un service (spécification 6.2, TODO-2.61). À la
// maison, <service>-<nom>.local, à un seul niveau : Windows et une partie
// d'Android ne trouvent pas les noms .local à deux niveaux. Pour un vrai nom
// de domaine, <service>.<domaine>.
func ServiceAddress(name, domain string) string {
	if strings.HasSuffix(domain, ".local") {
		return name + "-" + domain
	}
	return name + "." + domain
}

// checkAddress refuse une adresse dont la première partie dépasse 63 caractères.
func checkAddress(name, address string) error {
	first, _, _ := strings.Cut(address, ".")
	if len(first) > maxLabel {
		return fmt.Errorf("%w : le nom « %s » est trop long pour l'adresse %s, choisissez un nom de %d caractères au plus", ErrInvalid, name, address, len(name)-(len(first)-maxLabel))
	}
	return nil
}

// Relocate met à l'adresse de la maison actuelle (ServiceAddress) chaque
// service en .local qui a une autre adresse : installé avant les adresses à un
// seul niveau (<service>.<nom>.local) ou sous l'ancien nom du Pi. Labels
// Traefik et PI_HOST réécrits, service redémarré ; s'il ne redémarre pas,
// l'ancienne adresse est remise. Lancé au démarrage de la console (donc après
// une mise à jour) et après un changement de nom du Pi.
func (m *Manager) Relocate(ctx context.Context, logf func(string, ...any)) {
	domain := m.CurrentDomain()
	if !strings.HasSuffix(domain, ".local") {
		return
	}
	instances, err := m.List()
	if err != nil {
		logf("adresses des services : %v", err)
		return
	}
	for _, instance := range instances {
		want := ServiceAddress(instance.Name, domain)
		if instance.NoWebPage || !strings.HasSuffix(instance.Address, ".local") || instance.Address == want {
			continue
		}
		if err := m.relocate(ctx, instance.Name, want); err != nil {
			logf("adresse de %s : %v", instance.Name, err)
			continue
		}
		logf("adresse de %s : %s devient %s", instance.Name, instance.Address, want)
	}
}

func (m *Manager) relocate(ctx context.Context, name, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := checkAddress(name, address); err != nil {
		return err
	}
	dir := m.serviceDir(name)
	statePath := filepath.Join(dir, stateName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		return err
	}
	var instance Instance
	if err := json.Unmarshal(data, &instance); err != nil {
		return fmt.Errorf("%s : %w", statePath, err)
	}
	if instance.Address == address {
		return nil
	}
	envPath := filepath.Join(dir, envName)
	overridePath := filepath.Join(dir, overrideName)
	previousEnv, err := os.ReadFile(envPath)
	if err != nil {
		return err
	}
	previousOverride, err := os.ReadFile(overridePath)
	if err != nil {
		return err
	}
	env, err := readEnv(envPath)
	if err != nil {
		return err
	}
	env["PI_HOST"] = address
	override, err := withAddress(previousOverride, name, address)
	if err != nil {
		return err
	}
	restore := func() error {
		if err := os.WriteFile(envPath, previousEnv, 0o600); err != nil {
			return err
		}
		return os.WriteFile(overridePath, previousOverride, 0o600)
	}
	if err := writeEnv(dir, env); err != nil {
		return err
	}
	if err := os.WriteFile(overridePath, override, 0o600); err != nil {
		if restoreErr := restore(); restoreErr != nil {
			return fmt.Errorf("%v, et l'ancienne adresse n'a pas pu être remise : %v", err, restoreErr)
		}
		return err
	}
	if output, err := m.compose(ctx, name, "up", "--detach", "--wait"); err != nil {
		failure := trimOutput(output, err)
		if restoreErr := restore(); restoreErr != nil {
			return fmt.Errorf("%s n'a pas redémarré (%s) et l'ancienne adresse n'a pas pu être remise : %v", name, failure, restoreErr)
		}
		if output, err := m.compose(context.WithoutCancel(ctx), name, "up", "--detach", "--wait"); err != nil {
			return fmt.Errorf("%s n'a pas redémarré (%s), ni avec l'ancienne adresse : %s", name, failure, trimOutput(output, err))
		}
		return fmt.Errorf("%s n'a pas redémarré, l'ancienne adresse est remise : %s", name, failure)
	}
	instance.Address = address
	if err := writeJSON(statePath, instance, 0o600); err != nil {
		return err
	}
	// NE PAS compter sur NEXTCLOUD_TRUSTED_DOMAINS : l'image ne le lit qu'à la
	// première installation, Nextcloud refuserait la nouvelle adresse.
	if instance.App == NextcloudApp {
		if _, err := m.occ(ctx, name, "config:system:set", "trusted_domains", "1", "--value="+address); err != nil {
			return fmt.Errorf("Nextcloud n'accepte pas encore sa nouvelle adresse %s : %v", address, err)
		}
	}
	return nil
}

// withAddress remplace l'adresse de la règle du routeur Traefik du service
// dans le fichier pi-appliance.override.json.
func withAddress(override []byte, name, address string) ([]byte, error) {
	var file map[string]any
	if err := json.Unmarshal(override, &file); err != nil {
		return nil, fmt.Errorf("réglages de %s illisibles : %w", name, err)
	}
	rule := "traefik.http.routers.pi-" + name + ".rule"
	services, _ := file["services"].(map[string]any)
	found := false
	for _, value := range services {
		service, _ := value.(map[string]any)
		labels, _ := service["labels"].(map[string]any)
		if _, ok := labels[rule]; ok {
			labels[rule] = "Host(`" + address + "`)"
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("%w : la page web de %s est introuvable dans ses réglages", ErrInvalid, name)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
