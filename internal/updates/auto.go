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

package updates

import (
	"context"
	"errors"
	"time"
)

// Installation automatique du socle (spécification 9.2) : arrêtée par
// défaut ; quand l'administrateur la met en marche, la nouvelle version de
// pi-appliance publiée sur le canal choisi s'installe toute seule la nuit, à
// l'heure choisie, exactement comme avec le bouton « Installer » (même
// vérification, même retour à l'ancienne version si la nouvelle ne démarre
// pas). Une version dont l'installation a échoué n'est pas réessayée toute
// seule ; un téléchargement raté (rien n'a été changé) est réessayé la nuit
// suivante.
//
// Installation automatique des mises à jour du système (Raspberry Pi OS),
// réglée à part, arrêtée par défaut : à son heure, les listes des dépôts sont
// relues et, s'il y a des paquets à mettre à jour, « updates.sh apply » est
// lancé comme avec le bouton. Une installation ratée est réessayée la nuit
// suivante. Le Pi n'est pas redémarré : la console le rappelle.

// DefaultHour : heure proposée pour l'installation automatique, après la
// sauvegarde de la nuit (3 h).
const DefaultHour = 4

// DefaultSystemHour : heure proposée pour les mises à jour du système, une
// heure après celle de pi-appliance pour ne pas se gêner.
const DefaultSystemHour = 5

// autoEvery : délai entre deux regards sur l'heure de l'installation.
const autoEvery = 10 * time.Minute

// Auto : réglage de l'installation automatique des nouvelles versions.
type Auto struct {
	Enabled bool `json:"enabled"`
	// Hour : heure de l'installation (0 à 23, heure du Pi).
	Hour int `json:"hour"`
}

func (c *Checker) autoLocked() Auto {
	if c.SettingsFile == "" || c.auto == nil {
		return Auto{Hour: DefaultHour}
	}
	return *c.auto
}

// Auto rend le réglage de l'installation automatique.
func (c *Checker) Auto() Auto {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.autoLocked()
}

func (c *Checker) systemLocked() Auto {
	if c.SettingsFile == "" || c.system == nil {
		return Auto{Hour: DefaultSystemHour}
	}
	return *c.system
}

// AutoSystem rend le réglage de l'installation automatique des mises à jour
// du système.
func (c *Checker) AutoSystem() Auto {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.systemLocked()
}

func checkAuto(auto Auto, settingsFile string) error {
	if auto.Hour < 0 || auto.Hour > 23 {
		return Failure{Code: "hour", Message: "Heure inconnue : choisissez une heure de 0 à 23."}
	}
	if settingsFile == "" {
		return Failure{Code: "unavailable", Message: "L'installation automatique ne se règle pas depuis cette console."}
	}
	return nil
}

// SetAuto change le réglage de l'installation automatique de pi-appliance et
// l'enregistre ; celui du système ne change pas.
func (c *Checker) SetAuto(auto Auto) error {
	if err := checkAuto(auto, c.SettingsFile); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.saveLocked(c.channelLocked(), auto, c.systemLocked()); err != nil {
		return err
	}
	c.auto = &auto
	return nil
}

// SetAutoSystem change le réglage de l'installation automatique des mises à
// jour du système et l'enregistre ; celui de pi-appliance ne change pas.
func (c *Checker) SetAutoSystem(auto Auto) error {
	if err := checkAuto(auto, c.SettingsFile); err != nil {
		return err
	}
	if c.Runner == nil {
		return Failure{Code: "unavailable", Message: "Le système ne se met pas à jour depuis cette console."}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.saveLocked(c.channelLocked(), c.autoLocked(), auto); err != nil {
		return err
	}
	c.system = &auto
	return nil
}

// AutoApplySystem installe les mises à jour du système si leur installation
// automatique est en marche, à son heure, une fois par nuit au plus. Rend
// vrai quand l'installation a commencé.
func (c *Checker) AutoApplySystem(ctx context.Context) bool {
	now := c.now()
	day := now.Format("2006-01-02")
	c.mu.Lock()
	auto := c.systemLocked()
	if c.SettingsFile == "" || c.Runner == nil || !auto.Enabled || now.Hour() != auto.Hour || c.systemDay == day {
		c.mu.Unlock()
		return false
	}
	c.systemDay = day
	busy := c.download != nil && c.download.State == "downloading"
	c.mu.Unlock()
	if busy {
		c.logf("installation automatique du système : pi-appliance se télécharge, rien n'est lancé cette nuit")
		return false
	}
	// Listes des dépôts relues juste avant d'installer.
	if _, err := c.run(ctx, refreshTimeout, "refresh"); err != nil {
		c.logf("installation automatique du système : %v", err)
	}
	system, err := c.SystemStatus(ctx)
	if err != nil {
		c.logf("installation automatique du système : %v", err)
		return false
	}
	if len(system.Packages) == 0 {
		return false
	}
	if system.Busy() || system.Upgrading() {
		c.logf("installation automatique du système : une installation est déjà en cours")
		return false
	}
	if err := c.ApplySystem(ctx); err != nil {
		c.logf("installation automatique du système : %v", err)
		return false
	}
	c.logf("installation automatique de %d mises à jour du système", len(system.Packages))
	return true
}

// AutoInstall installe la nouvelle version de pi-appliance si l'installation
// automatique est en marche, à son heure, une fois par nuit au plus. Rend
// la version dont l'installation a commencé, vide sinon.
func (c *Checker) AutoInstall(ctx context.Context) string {
	now := c.now()
	day := now.Format("2006-01-02")
	c.mu.Lock()
	auto := c.autoLocked()
	if c.SettingsFile == "" || !auto.Enabled || now.Hour() != auto.Hour || c.autoDay == day {
		c.mu.Unlock()
		return ""
	}
	c.autoDay = day
	c.mu.Unlock()
	// Dernière version publiée relue juste avant d'installer.
	c.Check(ctx)
	status := c.Status(ctx)
	if !status.Appliance.Available || !status.Appliance.Installable {
		return ""
	}
	if upgrade := status.System.Upgrade; upgrade != nil && upgrade.State == "failed" && upgrade.Version == status.Appliance.Latest {
		c.logf("installation automatique : pi-appliance %s a déjà échoué, pas de nouvel essai", upgrade.Version)
		return ""
	}
	version, err := c.InstallAppliance(ctx)
	var failure Failure
	switch {
	case err == nil:
		c.logf("installation automatique de pi-appliance %s", version)
		return version
	case errors.As(err, &failure):
		c.logf("installation automatique : %s", failure.Message)
	default:
		c.logf("installation automatique : %v", err)
	}
	return ""
}
