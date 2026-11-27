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
	"slices"
)

// keptFiles : réglages d'un service gardés par « Rebuild cleanup » ; le reste
// de son dossier (dossiers montés en ./…) est de la donnée.
var keptFiles = []string{composeName, overrideName, envName, stateName, advancedName, advancedSettings, previousName, databaseFile, sourceName, adminEnvName}

func (m *Manager) customInstance(name string) (Instance, error) {
	instance, err := m.instance(name)
	if err != nil {
		return Instance{}, err
	}
	if instance.App != CustomApp {
		return Instance{}, fmt.Errorf("%w : %s ne vient pas d'un Docker Compose libre", ErrInvalid, name)
	}
	return instance, nil
}

// ErrRebuildRolledBack : le service n'a pas redémarré après le téléchargement
// et tourne de nouveau avec ses images d'avant.
var ErrRebuildRolledBack = errors.New("rebuild raté, retour aux images d'avant")

// Rebuild retélécharge toutes les images d'un Docker Compose libre, même déjà
// sur le Pi, puis recrée et relance ses conteneurs (TODO-2.81). Cleanup efface
// avant ses données : volumes, dossiers de son dossier, contenu de sa base
// PostgreSQL et contenu des dossiers du Pi montés par un chemin absolu dans son
// compose.yaml (CleanupDirs, refusé sans rien effacer pour un dossier protégé).
// Sans réseau, le téléchargement échoue avant tout arrêt ; si le service ne
// redémarre pas, il repart avec ses images d'avant.
func (m *Manager) Rebuild(ctx context.Context, name string, cleanup, dropEnv bool) (Result, error) {
	m.forgetProgress(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	instance, err := m.customInstance(name)
	if err != nil {
		return Result{}, err
	}
	m.track(name, instance.AppName)
	err = m.rebuild(ctx, instance, cleanup, dropEnv)
	m.finish(name, err)
	if err != nil {
		return Result{}, err
	}
	return Result{Instance: instance}, nil
}

func (m *Manager) rebuild(ctx context.Context, instance Instance, cleanup, dropEnv bool) error {
	name := instance.Name
	dir := m.serviceDir(name)
	if cleanup {
		if _, _, err := m.cleanupDirs(name); err != nil {
			return err
		}
	}
	output, err := m.compose(ctx, name, "images", "--format", "json")
	if err != nil {
		return fmt.Errorf("les images actuelles de %s sont illisibles, le service n'a pas été touché : %s", name, trimOutput(output, err))
	}
	images, err := parseImages(output)
	if err != nil {
		return fmt.Errorf("les images actuelles de %s sont illisibles, le service n'a pas été touché : %w", name, err)
	}
	keep := "pi-rollback-" + name
	dropKept := func() {
		for _, img := range images {
			m.Runner.Run(context.WithoutCancel(ctx), dir, "image", "rm", img.keepRef(keep))
		}
	}
	for i, img := range images {
		if output, err := m.Runner.Run(ctx, dir, "image", "tag", img.ID, img.keepRef(keep)); err != nil {
			images = images[:i]
			dropKept()
			return fmt.Errorf("l'image %s n'a pas pu être mise de côté, le service n'a pas été touché : %s", img.ref(), trimOutput(output, err))
		}
	}
	m.setStep(name, StepDownload)
	if err := m.forcePull(ctx, name); err != nil {
		dropKept()
		return fmt.Errorf("les images de %s n'ont pas pu être téléchargées, le service n'a pas été touché : vérifiez que le Pi est bien relié à internet, puis réessayez (%v)", name, err)
	}
	failure := m.restart(ctx, instance, cleanup, dropEnv)
	if failure == "" {
		dropKept()
		return nil
	}
	if len(images) == 0 {
		return fmt.Errorf("%s n'a pas pu redémarrer : %s", name, failure)
	}
	if err := m.rebuildRollback(context.WithoutCancel(ctx), name, images, keep); err != nil {
		return fmt.Errorf("%s n'a pas pu redémarrer (%s), et le retour aux images d'avant non plus : %v", name, failure, err)
	}
	dropKept()
	return fmt.Errorf("%w : %s", ErrRebuildRolledBack, failure)
}

// restart arrête le service, efface ses données si cleanup, puis le recrée et
// le relance ; rend la raison d'un échec, vide sinon.
func (m *Manager) restart(ctx context.Context, instance Instance, cleanup, dropEnv bool) string {
	name := instance.Name
	m.setStep(name, StepCreate)
	args := []string{"down"}
	if cleanup {
		args = append(args, "--volumes")
	}
	if output, err := m.compose(ctx, name, args...); err != nil {
		return "arrêt impossible : " + trimOutput(output, err)
	}
	if cleanup {
		if err := m.emptyDatabase(ctx, instance); err != nil {
			return fmt.Sprintf("la base n'a pas pu être vidée : %v", err)
		}
		if err := clearServiceData(m.serviceDir(name)); err != nil {
			return fmt.Sprintf("les données n'ont pas pu être effacées : %v", err)
		}
		if dropEnv {
			if err := writeAdminEnv(m.serviceDir(name), ""); err != nil {
				return fmt.Sprintf("le fichier .env n'a pas pu être effacé : %v", err)
			}
			if err := rewriteEnv(m.serviceDir(name)); err != nil {
				return fmt.Sprintf("le fichier .env n'a pas pu être réécrit : %v", err)
			}
		}
		// NE PAS réutiliser la liste vérifiée avant le téléchargement : un lien a pu changer depuis.
		dirs, _, err := m.cleanupDirs(name)
		if err != nil {
			return err.Error()
		}
		for _, host := range dirs {
			if err := emptyDir(host); err != nil {
				return fmt.Sprintf("le dossier %s du Pi n'a pas pu être vidé : %v", host, err)
			}
		}
	}
	for _, step := range []struct {
		name string
		args []string
	}{
		{StepCreate, []string{"create"}},
		{StepStart, []string{"start"}},
		{StepCheck, []string{"up", "--detach", "--wait"}},
	} {
		m.setStep(name, step.name)
		if output, err := m.compose(ctx, name, step.args...); err != nil {
			return trimOutput(output, err)
		}
	}
	return ""
}

// rebuildRollback remet les images d'avant sous leur étiquette et relance le
// service avec elles.
func (m *Manager) rebuildRollback(ctx context.Context, name string, images []image, keep string) error {
	dir := m.serviceDir(name)
	for _, img := range images {
		if output, err := m.Runner.Run(ctx, dir, "image", "tag", img.keepRef(keep), img.ref()); err != nil {
			return fmt.Errorf("image %s : %s", img.ref(), trimOutput(output, err))
		}
	}
	if output, err := m.compose(ctx, name, "up", "--detach", "--wait", "--force-recreate"); err != nil {
		return errors.New(trimOutput(output, err))
	}
	return nil
}

// forcePull télécharge toutes les images du service, même celles déjà sur le
// Pi ; sans ImagePuller, par « docker compose pull ».
func (m *Manager) forcePull(ctx context.Context, name string) error {
	if m.Images == nil {
		if output, err := m.compose(ctx, name, "pull"); err != nil {
			return errors.New(trimOutput(output, err))
		}
		return nil
	}
	return m.pullImages(ctx, name, true)
}

// emptyDatabase vide la base du PostgreSQL partagé d'un service, en gardant
// son compte et son mot de passe. Appelé sous m.mu.
func (m *Manager) emptyDatabase(ctx context.Context, instance Instance) error {
	if instance.Database == nil {
		return nil
	}
	if err := m.installed(instance.Database.Server); err != nil {
		return nil // serveur retiré : plus de base à vider
	}
	server, name := instance.Database.Server, instance.Database.Name
	for _, sql := range []string{
		fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", name),
		fmt.Sprintf("CREATE DATABASE %s OWNER %s", name, name),
		fmt.Sprintf("REVOKE ALL ON DATABASE %s FROM PUBLIC", name),
	} {
		if _, err := m.psql(ctx, server, sql); err != nil {
			return err
		}
	}
	return nil
}

func emptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func clearServiceData(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if slices.Contains(keptFiles, entry.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}
