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

package backup

import (
	"archive/tar"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gmoulard/pi-appliance/internal/services"
)

// Restauration de l'appliance entière sur une nouvelle carte SD
// (spécification 9.1), première étape : depuis le disque de sauvegarde, avec
// le mot de passe administrateur en vigueur à la date de la sauvegarde. La
// configuration (comptes, réglages, réglages des services) est remise, puis
// chaque service, ses données recréées, puis il est démarré. La clé des
// sauvegardes devient celle de la sauvegarde, avec son enveloppe : les
// sauvegardes d'avant restent lisibles, les suivantes continuent la série.
// L'assistant de premier démarrage qui l'appelle reste à faire (TODO-4.12).

// RestoreAppliance remet toute l'appliance telle qu'elle était dans la
// sauvegarde id. À n'appeler que sur une appliance sans compte (assistant
// de premier démarrage) : la configuration actuelle est remplacée.
func (m *Manager) RestoreAppliance(ctx context.Context, id, password string) (err error) {
	if !m.begin("restore") {
		return ErrBusy
	}
	defer m.end()
	var failed []string
	defer func() {
		outcome := Outcome{Action: "restore", OK: err == nil && len(failed) == 0, Message: "L'appliance est restaurée."}
		if err != nil {
			outcome.Message = err.Error()
			m.logf("restauration de l'appliance : %v", err)
		} else if len(failed) > 0 {
			outcome.Message = fmt.Sprintf("L'appliance est restaurée, sauf : %v.", failed)
		}
		m.record(outcome)
	}()
	root, err := m.Target()
	if err != nil {
		return err
	}
	st := &diskStore{root: root, free: m.freeMB}
	snapshot, err := m.readManifest(ctx, st, id)
	if err != nil {
		return ErrNotFound
	}
	if password == "" {
		return ErrPassword
	}
	key, err := snapshot.Envelope.Open(password)
	if err != nil {
		return err
	}
	if snapshot.Locked {
		if snapshot, err = unseal(snapshot, key); err != nil {
			return err
		}
	}
	if err := m.restoreConfig(ctx, st, id, key); err != nil {
		return fmt.Errorf("configuration illisible : %w", err)
	}
	if err := m.adoptKey(key, snapshot.Envelope); err != nil {
		return err
	}
	for _, saved := range snapshot.Services {
		if saved.Error != "" {
			continue
		}
		if err := m.restoreFiles(ctx, st, id, key, saved.Name, saved); err != nil {
			m.logf("restauration de %s : %v", saved.Name, err)
			failed = append(failed, saved.Name)
			continue
		}
		if err := m.Services.Control(context.WithoutCancel(ctx), saved.Name, services.Start); err != nil {
			m.logf("démarrage de %s : %v", saved.Name, err)
			failed = append(failed, saved.Name)
		}
	}
	return nil
}

// restoreConfig remplace le dossier des données de la console par celui de
// la sauvegarde, sauf le dossier des sauvegardes (clé, réglages, résultat).
func (m *Manager) restoreConfig(ctx context.Context, st store, id string, key []byte) error {
	staging := filepath.Clean(m.Dir) + ".restore"
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	x := newExtractor(staging)
	err := readFile(ctx, st, id, configName, key, func(header *tar.Header, r io.Reader) error {
		rel, ok := entryPath(header.Name, "config")
		// Le dossier des sauvegardes n'est jamais dans config.enc ; refusé quand même.
		if !ok || rel == "." || rel == stateDir || strings.HasPrefix(filepath.ToSlash(rel), stateDir+"/") {
			return nil
		}
		return x.add(header, rel, r)
	})
	if err == nil {
		err = x.finish()
	}
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(m.Dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == stateDir {
			continue
		}
		if err := os.RemoveAll(filepath.Join(m.Dir, entry.Name())); err != nil {
			return err
		}
	}
	restored, err := os.ReadDir(staging)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	for _, entry := range restored {
		if err := os.Rename(filepath.Join(staging, entry.Name()), filepath.Join(m.Dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

// adoptKey fait de la clé de la sauvegarde celle de l'appliance, protégée
// par la même enveloppe (le mot de passe en vigueur à sa date).
func (m *Manager) adoptKey(key []byte, envelope Envelope) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	k := m.keys()
	if err := writeFile(k.keyPath(), []byte(base64.StdEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return err
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(k.envelopePath(), append(data, '\n'), 0o600)
}
