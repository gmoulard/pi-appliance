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
// suivante. Les correctifs du système restent à installer depuis la console.

// DefaultHour : heure proposée pour l'installation automatique, après la
// sauvegarde de la nuit (3 h).
const DefaultHour = 4

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

// SetAuto change le réglage de l'installation automatique et l'enregistre.
func (c *Checker) SetAuto(auto Auto) error {
	if auto.Hour < 0 || auto.Hour > 23 {
		return Failure{Code: "hour", Message: "Heure inconnue : choisissez une heure de 0 à 23."}
	}
	if c.SettingsFile == "" {
		return Failure{Code: "unavailable", Message: "L'installation automatique ne se règle pas depuis cette console."}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.saveLocked(c.channelLocked(), auto); err != nil {
		return err
	}
	c.auto = &auto
	return nil
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
