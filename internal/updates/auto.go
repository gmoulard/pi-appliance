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

// Installation automatique (spécification 9.2), arrêtée par défaut, réglée
// par deux cases indépendantes et une seule heure :
//
//   - pi-appliance (le socle) : la nouvelle version publiée sur le canal
//     choisi s'installe exactement comme avec le bouton « Installer » (même
//     vérification, même retour à l'ancienne version si la nouvelle ne
//     démarre pas). Une version dont l'installation a échoué n'est pas
//     réessayée toute seule ; un téléchargement raté (rien n'a été changé)
//     est réessayé la nuit suivante.
//   - le système (Raspberry Pi OS) : les listes des dépôts sont relues et,
//     s'il y a des paquets à mettre à jour, « updates.sh apply » est lancé
//     comme avec le bouton. Le Pi n'est pas redémarré : la console le rappelle.
//
// Quand les deux sont en marche, pi-appliance passe d'abord (quelques
// minutes) ; le système attend la fin de son installation, en regardant de
// nouveau toutes les 10 minutes pendant l'heure choisie.

// DefaultHour : heure proposée pour les installations automatiques, après
// la sauvegarde de la nuit (3 h).
const DefaultHour = 4

// autoEvery : délai entre deux regards sur l'heure de l'installation.
const autoEvery = 10 * time.Minute

// Auto : réglage des installations automatiques.
type Auto struct {
	// Appliance : nouvelles versions de pi-appliance.
	Appliance bool `json:"appliance"`
	// System : mises à jour du système, correctifs de sécurité compris.
	System bool `json:"system"`
	// Hour : heure des installations (0 à 23, heure du Pi), commune aux deux.
	Hour int `json:"hour"`
}

func (c *Checker) autoLocked() Auto {
	if c.SettingsFile == "" || c.auto == nil {
		return Auto{Hour: DefaultHour}
	}
	return *c.auto
}

// Auto rend le réglage des installations automatiques.
func (c *Checker) Auto() Auto {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.autoLocked()
}

// SetAuto change le réglage des installations automatiques et l'enregistre.
func (c *Checker) SetAuto(auto Auto) error {
	if auto.Hour < 0 || auto.Hour > 23 {
		return Failure{Code: "hour", Message: "Heure inconnue : choisissez une heure de 0 à 23."}
	}
	if c.SettingsFile == "" {
		return Failure{Code: "unavailable", Message: "L'installation automatique ne se règle pas depuis cette console."}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if auto.System && !c.autoLocked().System && c.Runner == nil {
		return Failure{Code: "unavailable", Message: "Le système ne se met pas à jour depuis cette console."}
	}
	if err := c.saveLocked(c.channelLocked(), auto); err != nil {
		return err
	}
	c.auto = &auto
	return nil
}

// autoNight dit si c'est l'heure d'une installation automatique en marche
// (enabled) et qu'elle n'a pas encore été tentée cette nuit (*day) ; la nuit
// est alors marquée comme tentée.
func (c *Checker) autoNight(enabled func(Auto) bool, day *string) bool {
	now := c.now()
	today := now.Format("2006-01-02")
	c.mu.Lock()
	defer c.mu.Unlock()
	auto := c.autoLocked()
	if c.SettingsFile == "" || !enabled(auto) || now.Hour() != auto.Hour || *day == today {
		return false
	}
	*day = today
	return true
}

// retryLater rend la nuit à une installation qui n'a pas pu commencer parce
// qu'une autre était en cours : elle sera retentée 10 minutes plus tard.
func (c *Checker) retryLater(day *string) {
	c.mu.Lock()
	*day = ""
	c.mu.Unlock()
}

// AutoInstall installe la nouvelle version de pi-appliance si son
// installation automatique est en marche, à l'heure choisie, une fois par
// nuit au plus. Rend la version dont l'installation a commencé, vide sinon.
func (c *Checker) AutoInstall(ctx context.Context) string {
	if !c.autoNight(func(a Auto) bool { return a.Appliance }, &c.autoDay) {
		return ""
	}
	// Dernière version publiée relue juste avant d'installer.
	c.Check(ctx)
	status := c.Status(ctx)
	if status.System != nil && (status.System.Busy() || status.System.Upgrading()) {
		c.retryLater(&c.autoDay)
		return ""
	}
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
	case errors.As(err, &failure) && failure.Code == "busy":
		c.retryLater(&c.autoDay)
	case errors.As(err, &failure):
		c.logf("installation automatique : %s", failure.Message)
	default:
		c.logf("installation automatique : %v", err)
	}
	return ""
}

// AutoApplySystem installe les mises à jour du système si leur installation
// automatique est en marche, à l'heure choisie, une fois par nuit au plus ;
// pendant une autre installation, elle attend et réessaie 10 minutes plus
// tard. Rend vrai quand l'installation a commencé.
func (c *Checker) AutoApplySystem(ctx context.Context) bool {
	if c.Runner == nil || !c.autoNight(func(a Auto) bool { return a.System }, &c.systemDay) {
		return false
	}
	c.mu.Lock()
	downloading := c.download != nil && c.download.State == "downloading"
	c.mu.Unlock()
	if downloading {
		c.retryLater(&c.systemDay)
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
	if system.Busy() || system.Upgrading() {
		c.retryLater(&c.systemDay)
		return false
	}
	if len(system.Packages) == 0 {
		return false
	}
	if err := c.ApplySystem(ctx); err != nil {
		c.logf("installation automatique du système : %v", err)
		return false
	}
	c.logf("installation automatique de %d mises à jour du système", len(system.Packages))
	return true
}
