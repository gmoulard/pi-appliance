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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const previousName = "compose.previous.yaml"

// ErrRolledBack : la mise à jour a échoué et le service est revenu à sa version précédente.
var ErrRolledBack = errors.New("mise à jour ratée, retour à la version précédente")

// image reprend les champs utiles de « docker compose images --format json ».
type image struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
}

func (i image) ref() string {
	return i.Repository + ":" + i.Tag
}

// keepRef est l'étiquette qui garde l'image pendant une mise à jour ; elle inclut l'étiquette
// d'origine pour que deux images d'un même dépôt ne se marchent pas dessus.
func (i image) keepRef(keep string) string {
	tag := keep + "-" + i.Tag
	if len(tag) > 128 {
		tag = tag[:128]
	}
	return i.Repository + ":" + tag
}

// UpdateAvailable dit si le manifeste du catalogue diffère de celui du service installé.
func (m *Manager) UpdateAvailable(app marketplace.App, name string) bool {
	installed, err := os.ReadFile(filepath.Join(m.serviceDir(name), composeName))
	return err == nil && !bytes.Equal(installed, app.Compose)
}

// Update met un service à jour : nouveau manifeste du catalogue, nouvelles images, redémarrage.
// Les données (volumes) sont copiées juste avant le démarrage de la nouvelle version, service
// arrêté. En cas d'échec, il revient automatiquement au manifeste, aux images et aux données
// d'avant (spécification 6.2) : une nouvelle version qui modifie les données avant d'échouer
// ne les laisse pas illisibles pour l'ancienne.
func (m *Manager) Update(ctx context.Context, app marketplace.App, name string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.installed(name); err != nil {
		return Result{}, err
	}
	dir := m.serviceDir(name)
	var instance Instance
	data, err := os.ReadFile(filepath.Join(dir, stateName))
	if err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(data, &instance); err != nil {
		return Result{}, err
	}
	if instance.App != app.ID {
		return Result{}, fmt.Errorf("%w : %s est un service %s, pas %s", ErrInvalid, name, instance.App, app.ID)
	}
	previous, err := os.ReadFile(filepath.Join(dir, composeName))
	if err != nil {
		return Result{}, err
	}
	previousEnv, err := os.ReadFile(filepath.Join(dir, envName))
	if err != nil {
		return Result{}, err
	}
	// Place pour la copie des données vérifiée avant de rien toucher.
	volumes, err := m.serviceVolumes(ctx, name)
	if err != nil {
		return Result{}, err
	}
	if err := m.checkSpace(name, volumes); err != nil {
		return Result{}, err
	}
	output, err := m.compose(ctx, name, "images", "--format", "json")
	if err != nil {
		return Result{}, fmt.Errorf("les images actuelles de %s sont illisibles : %s", name, trimOutput(output, err))
	}
	images, err := parseImages(output)
	if err != nil {
		return Result{}, fmt.Errorf("les images actuelles de %s sont illisibles : %w", name, err)
	}
	// Une étiquette propre à l'instance garde les anciennes images, même si « docker image prune » passe.
	keep := "pi-rollback-" + name
	for _, img := range images {
		if output, err := m.Runner.Run(ctx, dir, "image", "tag", img.ID, img.keepRef(keep)); err != nil {
			return Result{}, fmt.Errorf("sauvegarde de l'image %s impossible : %s", img.ref(), trimOutput(output, err))
		}
	}
	if err := os.WriteFile(filepath.Join(dir, previousName), previous, 0o600); err != nil {
		return Result{}, err
	}
	env, secrets, err := m.environment(app, dir, name, instance.Address, nil)
	if err != nil {
		return Result{}, err
	}
	if err := m.writeFiles(dir, app, name, instance.Address, env); err != nil {
		return Result{}, err
	}
	failure := ""
	// copied : données copiées, à remettre en cas d'échec (nil tant qu'elles n'ont pas bougé).
	var copied []volumeCopy
	// Téléchargement d'abord, service en marche ; puis arrêt, copie des données, démarrage.
	if output, err := m.compose(ctx, name, "pull"); err != nil {
		failure = "téléchargement de la nouvelle version impossible : " + trimOutput(output, err)
	} else if output, err := m.compose(ctx, name, "stop"); err != nil {
		failure = "arrêt du service impossible : " + trimOutput(output, err)
	} else if err := m.backup(ctx, name, volumes); err != nil {
		failure = err.Error()
	} else {
		copied = volumes
		if output, err := m.compose(ctx, name, "up", "--detach", "--wait", "--remove-orphans"); err != nil {
			failure = "la nouvelle version n'a pas démarré : " + trimOutput(output, err)
		}
	}
	if failure == "" {
		for _, img := range images {
			m.Runner.Run(context.WithoutCancel(ctx), dir, "image", "rm", img.keepRef(keep))
		}
		m.dropBackups(context.WithoutCancel(ctx), name, copied)
		os.Remove(filepath.Join(dir, previousName))
		instance.AppName = app.Name
		if err := writeJSON(filepath.Join(dir, stateName), instance, 0o600); err != nil {
			return Result{}, err
		}
		return Result{Instance: instance, Secrets: secrets}, nil
	}
	if err := m.rollback(context.WithoutCancel(ctx), name, previous, previousEnv, images, keep, copied); err != nil {
		return Result{}, fmt.Errorf("mise à jour de %s ratée (%s), et le retour à la version précédente aussi : %v", name, failure, err)
	}
	return Result{Instance: instance}, fmt.Errorf("%w : %s", ErrRolledBack, failure)
}

// rollback revient au manifeste, aux images et, si elles ont été copiées, aux données d'avant.
// En cas d'échec, images, manifeste et données de secours sont gardés.
func (m *Manager) rollback(ctx context.Context, name string, compose, env []byte, images []image, keep string, copied []volumeCopy) error {
	dir := m.serviceDir(name)
	if err := os.WriteFile(filepath.Join(dir, composeName), compose, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, envName), env, 0o600); err != nil {
		return err
	}
	var problems []string
	for _, img := range images {
		if output, err := m.Runner.Run(ctx, dir, "image", "tag", img.keepRef(keep), img.ref()); err != nil {
			problems = append(problems, fmt.Sprintf("image %s : %s", img.ref(), trimOutput(output, err)))
		}
	}
	if len(copied) > 0 {
		// Plus aucun conteneur ne doit écrire pendant la remise des données.
		if output, err := m.compose(ctx, name, "down", "--remove-orphans"); err != nil {
			return fmt.Errorf("arrêt de la nouvelle version impossible : %s", trimOutput(output, err))
		}
		if err := m.restore(ctx, copied); err != nil {
			return err
		}
	}
	if output, err := m.compose(ctx, name, "up", "--detach", "--wait", "--remove-orphans"); err != nil {
		problems = append(problems, trimOutput(output, err))
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, " ; "))
	}
	for _, img := range images {
		m.Runner.Run(ctx, dir, "image", "rm", img.keepRef(keep))
	}
	m.dropBackups(ctx, name, copied)
	return os.Remove(filepath.Join(dir, previousName))
}

func parseImages(output []byte) ([]image, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return nil, nil
	}
	var images []image
	if output[0] == '[' {
		if err := json.Unmarshal(output, &images); err != nil {
			return nil, err
		}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(output))
		for decoder.More() {
			var img image
			if err := decoder.Decode(&img); err != nil {
				return nil, err
			}
			images = append(images, img)
		}
	}
	seen := map[string]bool{}
	unique := images[:0]
	for _, img := range images {
		if img.ID == "" || img.Repository == "" || img.Tag == "" || seen[img.ref()] {
			continue
		}
		seen[img.ref()] = true
		unique = append(unique, img)
	}
	return unique, nil
}
