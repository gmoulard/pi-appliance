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

// Package updates propose les mises à jour dans la console (spécification
// 9.2) : correctifs de Raspberry Pi OS par le script updates.sh de l'image,
// et nouvelle version de pi-appliance publiée sur GitHub (appliance.go),
// installés à la demande de l'administrateur ; les nouvelles versions de
// pi-appliance et les mises à jour du système s'installent toutes seules la
// nuit à l'heure choisie si l'administrateur l'a voulu, chacune de son côté
// (auto.go).
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultScript est le script installé par l'image.
const DefaultScript = "/usr/local/lib/pi-appliance/updates.sh"

// Runner lance updates.sh avec ses arguments (network.Script convient).
type Runner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

// Failure est un refus de updates.sh : code pour la console, message en langage courant.
type Failure struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (f Failure) Error() string {
	return f.Message
}

// Package : un paquet du système à mettre à jour.
type Package struct {
	Name     string `json:"name"`
	From     string `json:"from"`
	To       string `json:"to"`
	Security bool   `json:"security"`
}

// Apply : dernière installation des correctifs ; State vaut starting,
// running, done ou failed.
type Apply struct {
	State   string `json:"state"`
	Percent int    `json:"percent"`
	Error   string `json:"error"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// Upgrade : dernière installation d'une version de pi-appliance par
// updates.sh ; State vaut starting, running, done ou failed. RolledBack :
// l'ancienne version a été remise après un échec.
type Upgrade struct {
	State      string `json:"state"`
	Version    string `json:"version"`
	Error      string `json:"error"`
	Message    string `json:"message"`
	RolledBack bool   `json:"rolled_back"`
	At         string `json:"at"`
}

// System : réponse de « updates.sh status ».
type System struct {
	Packages       []Package `json:"packages"`
	Security       int       `json:"security"`
	CheckedAt      *string   `json:"checked_at"`
	Apply          *Apply    `json:"apply"`
	Upgrade        *Upgrade  `json:"upgrade"`
	RebootRequired bool      `json:"reboot_required"`
}

// Upgrading dit si une version de pi-appliance est en cours d'installation.
func (s System) Upgrading() bool {
	return s.Upgrade != nil && (s.Upgrade.State == "starting" || s.Upgrade.State == "running")
}

// Busy dit si une installation est en cours.
func (s System) Busy() bool {
	return s.Apply != nil && (s.Apply.State == "starting" || s.Apply.State == "running")
}

// Appliance : version de pi-appliance en service et dernière version publiée.
type Appliance struct {
	Current string `json:"current"`
	// Channel : canal de version choisi par l'administrateur (Stable ou Beta).
	Channel   string     `json:"channel"`
	Latest    string     `json:"latest,omitempty"`
	Available bool       `json:"available"`
	URL       string     `json:"url,omitempty"`
	CheckedAt *time.Time `json:"checked_at"`
	Error     string     `json:"error,omitempty"`
	// Installable : l'archive de mise à jour est publiée et la console sait
	// l'installer ici (updates.sh).
	Installable bool `json:"installable"`
	// Download : téléchargement de la nouvelle version, avant updates.sh.
	Download *Download `json:"download,omitempty"`
}

// Status : tout ce que la console affiche des mises à jour.
type Status struct {
	// System est nil quand le système ne se met pas à jour d'ici (poste de développement).
	System      *System   `json:"system"`
	SystemError string    `json:"system_error,omitempty"`
	Appliance   Appliance `json:"appliance"`
	Checking    bool      `json:"checking"`
	// Auto : installation automatique de pi-appliance et du système.
	Auto Auto `json:"auto"`
}

// Checker cherche les mises à jour, chaque jour et à la demande.
type Checker struct {
	// Runner lance updates.sh ; nil : pas de correctifs du système.
	Runner Runner
	// Releases donne la dernière version publiée ; nil : pas de comparaison.
	Releases Releases
	// Version : version de pi-appliance en service.
	Version string
	// DownloadDir : dossier où la nouvelle version est téléchargée, lu par
	// updates.sh ($PI_STATE/upgrade) ; vide : pas d'installation d'ici.
	DownloadDir string
	// SettingsFile garde le canal choisi et l'installation automatique
	// (updates.json, droits 600) ; vide : canal stable, pas d'installation
	// automatique, non modifiables.
	SettingsFile string
	// Client télécharge les archives ; nil : délai de 10 minutes.
	Client *http.Client
	Logf   func(format string, args ...any)
	Now    func() time.Time

	mu        sync.Mutex
	appliance Appliance
	release   *Release
	download  *Download
	checking  bool
	channel   string
	auto      *Auto
	// autoDay, systemDay : jour (AAAA-MM-JJ) de la dernière tentative
	// d'installation automatique, une seule par nuit pour chacune.
	autoDay   string
	systemDay string
}

type settings struct {
	Channel string `json:"channel"`
	Auto    *bool  `json:"auto,omitempty"`
	Hour    *int   `json:"hour,omitempty"`
	// SystemAuto : mises à jour du système ; Hour vaut pour les deux.
	SystemAuto *bool `json:"system_auto,omitempty"`
}

// Load lit le canal choisi et l'installation automatique ; un fichier absent
// ou un canal inconnu vaut le canal stable, sans installation automatique.
func (c *Checker) Load() error {
	if c.SettingsFile == "" {
		return nil
	}
	data, err := os.ReadFile(c.SettingsFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved settings
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("%s : %w", c.SettingsFile, err)
	}
	c.mu.Lock()
	c.channel = ""
	if saved.Channel == Beta {
		c.channel = Beta
	}
	auto := Auto{Hour: DefaultHour}
	if saved.Auto != nil {
		auto.Appliance = *saved.Auto
	}
	if saved.SystemAuto != nil {
		auto.System = *saved.SystemAuto
	}
	if saved.Hour != nil && *saved.Hour >= 0 && *saved.Hour <= 23 {
		auto.Hour = *saved.Hour
	}
	c.auto = &auto
	c.mu.Unlock()
	return nil
}

// saveLocked enregistre les réglages (c.mu tenu).
func (c *Checker) saveLocked(channel string, auto Auto) error {
	data, err := json.Marshal(settings{Channel: channel, Auto: &auto.Appliance, SystemAuto: &auto.System, Hour: &auto.Hour})
	if err != nil {
		return err
	}
	return writeFile(c.SettingsFile, data)
}

// Channel rend le canal choisi : Stable tant que l'administrateur n'a pas
// choisi le canal bêta.
func (c *Checker) Channel() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channelLocked()
}

func (c *Checker) channelLocked() string {
	if c.channel == Beta {
		return Beta
	}
	return Stable
}

// SetChannel change de canal et l'enregistre ; la dernière version connue
// est oubliée jusqu'à la prochaine recherche, pour ne rien installer de
// l'ancien canal.
func (c *Checker) SetChannel(channel string) error {
	if channel != Stable && channel != Beta {
		return Failure{Code: "channel", Message: "Canal inconnu : choisissez « stable » ou « bêta »."}
	}
	if c.SettingsFile == "" {
		return Failure{Code: "unavailable", Message: "Le canal ne se change pas depuis cette console."}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.download != nil && c.download.State == "downloading" {
		return Failure{Code: "busy", Message: "Une nouvelle version est en cours de téléchargement : attendez la fin."}
	}
	if c.channelLocked() == channel {
		return nil
	}
	if err := c.saveLocked(channel, c.autoLocked()); err != nil {
		return err
	}
	c.channel = channel
	c.release = nil
	c.appliance.Latest, c.appliance.URL, c.appliance.Error = "", "", ""
	return nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".updates-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}

const (
	statusTimeout  = time.Minute
	refreshTimeout = 10 * time.Minute
	// Every : délai entre deux recherches automatiques.
	Every = 24 * time.Hour
)

func (c *Checker) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Checker) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := c.Runner.Run(ctx, nil, args...)
	if err != nil {
		var failure Failure
		if json.Unmarshal(out, &failure) == nil && failure.Code != "" {
			return nil, failure
		}
		return nil, fmt.Errorf("updates.sh %s : %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// SystemStatus lit les paquets du système à mettre à jour, sans rien installer.
func (c *Checker) SystemStatus(ctx context.Context) (System, error) {
	if c.Runner == nil {
		return System{}, errors.New("pas de script des mises à jour")
	}
	out, err := c.run(ctx, statusTimeout, "status")
	if err != nil {
		return System{}, err
	}
	var system System
	if err := json.Unmarshal(out, &system); err != nil {
		return System{}, fmt.Errorf("updates.sh status : réponse illisible : %w", err)
	}
	if system.Packages == nil {
		system.Packages = []Package{}
	}
	system.Security = 0
	for _, p := range system.Packages {
		if p.Security {
			system.Security++
		}
	}
	return system, nil
}

// ApplySystem lance l'installation des correctifs du système ; elle se
// poursuit après la réponse et se suit dans Status.
func (c *Checker) ApplySystem(ctx context.Context) error {
	if c.Runner == nil {
		return errors.New("pas de script des mises à jour")
	}
	_, err := c.run(ctx, statusTimeout, "apply")
	return err
}

// Status rend l'état des mises à jour : la liste des correctifs est relue,
// la dernière version publiée est celle de la dernière recherche.
func (c *Checker) Status(ctx context.Context) Status {
	c.mu.Lock()
	status := Status{Appliance: c.appliance, Checking: c.checking, Auto: c.autoLocked()}
	status.Appliance.Channel = c.channelLocked()
	release := c.release
	if c.download != nil {
		download := *c.download
		status.Appliance.Download = &download
	}
	c.mu.Unlock()
	status.Appliance.Current = c.Version
	status.Appliance.Available = Newer(c.Version, status.Appliance.Latest)
	if c.Runner != nil {
		system, err := c.SystemStatus(ctx)
		if err != nil {
			c.logf("mises à jour du système : %v", err)
			status.SystemError = "La liste des mises à jour du système est illisible pour l'instant."
		} else {
			status.System = &system
		}
	}
	status.Appliance.Installable = status.Appliance.Available && c.installable(release) &&
		status.System != nil && !status.System.Upgrading() &&
		(status.Appliance.Download == nil || status.Appliance.Download.State != "downloading")
	return status
}

// Check relit les listes de paquets des dépôts et la dernière version publiée
// de pi-appliance. Rien n'est installé. Une seule recherche à la fois : rend
// faux si une recherche est déjà en cours.
func (c *Checker) Check(ctx context.Context) bool {
	c.mu.Lock()
	if c.checking {
		c.mu.Unlock()
		return false
	}
	c.checking = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.checking = false
		c.mu.Unlock()
	}()
	if c.Runner != nil {
		if _, err := c.run(ctx, refreshTimeout, "refresh"); err != nil {
			c.logf("mises à jour du système : %v", err)
		}
	}
	for c.Releases != nil {
		channel := c.Channel()
		release, err := c.Releases.Latest(ctx, channel)
		if release != nil && !release.Allowed(channel) {
			release = nil
		}
		now := c.now()
		c.mu.Lock()
		if c.channelLocked() != channel {
			c.mu.Unlock()
			continue
		}
		c.appliance.CheckedAt = &now
		if err != nil {
			c.appliance.Error = "Impossible de savoir si une nouvelle version de pi-appliance est sortie : le site de publication ne répond pas."
		} else {
			c.appliance.Error = ""
			c.appliance.Latest, c.appliance.URL = "", ""
			c.release = release
			if release != nil {
				c.appliance.Latest, c.appliance.URL = release.Tag, release.URL
			}
		}
		c.mu.Unlock()
		if err != nil {
			c.logf("dernière version de pi-appliance : %v", err)
		}
		break
	}
	return true
}

// CheckNow lance une recherche sans l'attendre ; rend faux si une recherche
// est déjà en cours.
func (c *Checker) CheckNow() bool {
	c.mu.Lock()
	busy := c.checking
	c.mu.Unlock()
	if busy {
		return false
	}
	go c.Check(context.Background())
	return true
}

// Run cherche les mises à jour peu après le démarrage, puis chaque jour, et
// installe la nouvelle version de pi-appliance et les mises à jour du système,
// à l'heure choisie quand leur installation automatique est en marche,
// jusqu'à l'arrêt de ctx.
func (c *Checker) Run(ctx context.Context, first time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()
	ticker := time.NewTicker(autoEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			c.Check(ctx)
			timer.Reset(Every)
		case <-ticker.C:
			c.AutoInstall(ctx)
			c.AutoApplySystem(ctx)
		}
	}
}
