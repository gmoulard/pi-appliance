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
	"sort"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/services"
)

type Whole struct {
	Host     string    `json:"host"`
	ID       string    `json:"id"`
	Created  time.Time `json:"created"`
	Kind     Kind      `json:"kind"`
	SizeMB   int64     `json:"size_mb"`
	Services []string  `json:"services"`
}

func (m *Manager) ApplianceBackups(ctx context.Context) ([]Whole, error) {
	root, err := m.Target()
	if err != nil {
		return nil, err
	}
	base := filepath.Dir(root)
	entries, err := os.ReadDir(base)
	if errors.Is(err, os.ErrNotExist) {
		return []Whole{}, nil
	}
	if err != nil {
		return nil, err
	}
	list := []Whole{}
	for _, entry := range entries {
		if !entry.IsDir() || !validName(entry.Name()) {
			continue
		}
		st := &diskStore{root: filepath.Join(base, entry.Name()), free: m.freeMB}
		snapshots, err := m.storeSnapshots(ctx, st)
		if err != nil {
			m.logf("sauvegardes de %s : %v", entry.Name(), err)
			continue
		}
		for _, s := range snapshots {
			whole := Whole{Host: entry.Name(), ID: s.ID, Created: s.Created, Kind: s.Kind, SizeMB: s.SizeMB, Services: []string{}}
			for _, c := range s.Services {
				if c.Error == "" {
					whole.Services = append(whole.Services, c.Name)
				}
			}
			list = append(list, whole)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Created.After(list[j].Created) })
	return list, nil
}

func (m *Manager) applianceSnapshot(ctx context.Context, host, id, password string) (store, Snapshot, []byte, error) {
	if !idPattern.MatchString(id) || (host != "" && !validName(host)) {
		return nil, Snapshot{}, nil, ErrInvalid
	}
	root, err := m.Target()
	if err != nil {
		return nil, Snapshot{}, nil, err
	}
	if host != "" {
		root = filepath.Join(filepath.Dir(root), host)
	}
	st := &diskStore{root: root, free: m.freeMB}
	snapshot, err := m.readManifest(ctx, st, id)
	if err != nil {
		return nil, Snapshot{}, nil, ErrNotFound
	}
	if password == "" {
		return nil, Snapshot{}, nil, ErrPassword
	}
	key, err := snapshot.Envelope.Open(password)
	if err != nil {
		return nil, Snapshot{}, nil, err
	}
	if snapshot.Locked {
		if snapshot, err = unseal(snapshot, key); err != nil {
			return nil, Snapshot{}, nil, err
		}
	}
	return st, snapshot, key, nil
}

func (m *Manager) CheckAppliance(ctx context.Context, host, id, password string) error {
	if m.Running() != "" {
		return ErrBusy
	}
	_, _, _, err := m.applianceSnapshot(ctx, host, id, password)
	return err
}

func (m *Manager) RestoreAppliance(ctx context.Context, host, id, password string) (err error) {
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
	st, snapshot, key, err := m.applianceSnapshot(ctx, host, id, password)
	if err != nil {
		return err
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
