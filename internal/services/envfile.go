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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const (
	adminEnvName  = "admin.env"
	adminEnvMark  = "# pi-appliance : lignes données par l'administrateur (admin.env)"
	maxAdminEnv   = 64 << 10
	maxAdminLines = 500
)

var (
	ErrEnvRestored = errors.New("fichier .env refusé")
	adminEnvLine   = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=`)
)

func consoleVariable(name string) bool {
	return name == "PI_INSTANCE" || name == "PI_HOST" || strings.HasPrefix(name, "PI_SMTP_") || strings.HasPrefix(name, "PI_DATABASE_")
}

func normalizeAdminEnv(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if len(text) > maxAdminEnv {
		return "", fmt.Errorf("%w : le fichier .env fait plus de %d Ko", ErrInvalid, maxAdminEnv>>10)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > maxAdminLines {
		return "", fmt.Errorf("%w : le fichier .env a plus de %d lignes", ErrInvalid, maxAdminLines)
	}
	seen := map[string]int{}
	empty := true
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		empty = false
		if strings.ContainsRune(line, '\r') {
			return "", fmt.Errorf("%w : ligne %d du fichier .env : retour à la ligne inattendu", ErrInvalid, i+1)
		}
		match := adminEnvLine.FindStringSubmatch(trimmed)
		if match == nil {
			return "", fmt.Errorf("%w : ligne %d du fichier .env illisible : « %s » (attendu : NOM=valeur, le nom en lettres, chiffres et _)", ErrInvalid, i+1, trimmed)
		}
		name := match[1]
		if consoleVariable(name) {
			return "", fmt.Errorf("%w : ligne %d du fichier .env : la variable %s est posée par la console, elle ne peut pas être donnée ici", ErrInvalid, i+1, name)
		}
		if first, ok := seen[name]; ok {
			return "", fmt.Errorf("%w : la variable %s est donnée deux fois dans le fichier .env (lignes %d et %d)", ErrInvalid, name, first, i+1)
		}
		seen[name] = i + 1
		lines[i] = trimmed
	}
	if empty {
		return "", nil
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func readAdminEnv(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, adminEnvName))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

func writeAdminEnv(dir, content string) error {
	path := filepath.Join(dir, adminEnvName)
	if content == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func rewriteEnv(dir string) error {
	env, err := readEnv(filepath.Join(dir, envName))
	if err != nil {
		return err
	}
	return writeEnv(dir, env)
}

func (m *Manager) EnvFile(name string) (string, error) {
	if _, err := m.customInstance(name); err != nil {
		return "", err
	}
	return readAdminEnv(m.serviceDir(name))
}

func (m *Manager) KeptEnvFile(name string) (string, error) {
	if !marketplace.ValidLabel(name) {
		return "", nil
	}
	dir := m.serviceDir(name)
	if _, err := os.Stat(filepath.Join(dir, stateName)); err == nil {
		return "", nil
	}
	return readAdminEnv(dir)
}

func (m *Manager) SetEnvFile(ctx context.Context, name, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.customInstance(name); err != nil {
		return err
	}
	content, err := normalizeAdminEnv(text)
	if err != nil {
		return err
	}
	dir := m.serviceDir(name)
	previousAdmin, err := os.ReadFile(filepath.Join(dir, adminEnvName))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	previousEnv, err := os.ReadFile(filepath.Join(dir, envName))
	if err != nil {
		return err
	}
	restore := func() {
		restoreFile(filepath.Join(dir, adminEnvName), previousAdmin)
		restoreFile(filepath.Join(dir, envName), previousEnv)
	}
	if err := writeAdminEnv(dir, content); err != nil {
		restore()
		return err
	}
	if err := rewriteEnv(dir); err != nil {
		restore()
		return err
	}
	output, err := m.compose(ctx, name, "up", "--detach", "--wait", "--remove-orphans")
	if err == nil {
		return nil
	}
	failure := trimOutput(output, err)
	restore()
	if output, err := m.compose(context.WithoutCancel(ctx), name, "up", "--detach", "--wait", "--remove-orphans"); err != nil {
		return fmt.Errorf("avec le nouveau fichier .env, le service ne démarre pas (%s), et avec l'ancien non plus : %s", failure, trimOutput(output, err))
	}
	return fmt.Errorf("%w : %s", ErrEnvRestored, failure)
}
