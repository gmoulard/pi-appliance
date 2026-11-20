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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

const (
	NextcloudApp = "nextcloud"
	// nextcloudSitesMount : dossier des services vu par le conteneur Nextcloud.
	nextcloudSitesMount = "/srv/pi-appliance/services"
	nextcloudLinksName  = "nextcloud-sites.json"
	// NE PAS changer : utilisateur www-data des images Nextcloud et php:apache
	// (Debian), qui doit pouvoir écrire dans le dossier du site.
	siteOwner = 33
)

type nextcloudLink struct {
	Site    string `json:"site"`
	MountID int    `json:"mount_id"`
}

var mountIDPattern = regexp.MustCompile(`\d+`)

// NextcloudFolder : nom du dossier du site dans Nextcloud.
func NextcloudFolder(site string) string {
	return "Site " + site
}

// prepareSiteDir rend le dossier du site joignable et modifiable par
// Nextcloud (www-data) sans ouvrir le reste du dossier du service : les
// dossiers parents ne se listent pas et leurs fichiers restent en 600.
func (m *Manager) prepareSiteDir(name string) error {
	dir := filepath.Join(m.serviceDir(name), siteDir)
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return err
	}
	for _, path := range []string{filepath.Join(m.Dir, "services"), m.serviceDir(name)} {
		if err := os.Chmod(path, 0o711); err != nil {
			return err
		}
	}
	if err := os.Chmod(dir, 0o775); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		return os.Chown(dir, siteOwner, siteOwner)
	}
	return nil
}

// WakeNextcloud demande de relier tout de suite les sites à Nextcloud.
func (m *Manager) WakeNextcloud() {
	m.wakeOnce.Do(func() { m.wake = make(chan struct{}, 1) })
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// RunNextcloud relie les sites à Nextcloud au démarrage, à chaque
// installation ou désinstallation, et réessaie régulièrement : Nextcloud met
// plusieurs minutes à s'installer au premier démarrage.
func (m *Manager) RunNextcloud(ctx context.Context, every time.Duration, logf func(string, ...any)) {
	m.wakeOnce.Do(func() { m.wake = make(chan struct{}, 1) })
	for {
		if err := m.LinkNextcloud(ctx); err != nil {
			logf("Nextcloud : %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		case <-m.wake:
		}
	}
}

// LinkNextcloud ajoute dans chaque Nextcloud installé un dossier « Site
// <nom> » par site web (stockage externe local, réservé au groupe admin de
// Nextcloud) et retire ceux des sites désinstallés. Rien n'est lancé dans
// Nextcloud quand tout est déjà à jour.
func (m *Manager) LinkNextcloud(ctx context.Context) error {
	m.linkMu.Lock()
	defer m.linkMu.Unlock()
	instances, err := m.List()
	if err != nil {
		return err
	}
	sites := map[string]bool{}
	for _, instance := range instances {
		if instance.SiteFiles {
			sites[instance.Name] = true
		}
	}
	var errs []error
	for _, instance := range instances {
		if instance.App == NextcloudApp {
			if err := m.linkSites(ctx, instance.Name, sites); err != nil {
				errs = append(errs, fmt.Errorf("%s : %w", instance.Name, err))
			}
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) nextcloudLinks(cloud string) []nextcloudLink {
	var links []nextcloudLink
	data, err := os.ReadFile(filepath.Join(m.serviceDir(cloud), nextcloudLinksName))
	if err == nil && json.Unmarshal(data, &links) != nil {
		links = nil
	}
	return links
}

func (m *Manager) occ(ctx context.Context, cloud string, args ...string) ([]byte, error) {
	base := []string{"exec", "-T", "--user", "www-data", "app", "php", "occ"}
	output, err := m.compose(ctx, cloud, append(base, args...)...)
	if err != nil {
		return output, fmt.Errorf("occ %s : %s", args[0], trimOutput(output, err))
	}
	return output, nil
}

func (m *Manager) linkSites(ctx context.Context, cloud string, sites map[string]bool) error {
	links := m.nextcloudLinks(cloud)
	linked := map[string]bool{}
	pending := false
	for _, link := range links {
		linked[link.Site] = true
		pending = pending || !sites[link.Site]
	}
	for site := range sites {
		pending = pending || !linked[site]
	}
	if !pending {
		return nil
	}
	output, err := m.occ(ctx, cloud, "status", "--output=json")
	if err != nil {
		return nil
	}
	var status struct {
		Installed bool `json:"installed"`
	}
	if json.Unmarshal(output, &status) != nil || !status.Installed {
		return nil
	}
	save := func() error {
		return writeJSON(filepath.Join(m.serviceDir(cloud), nextcloudLinksName), links, 0o600)
	}
	kept := links[:0]
	for _, link := range links {
		if sites[link.Site] {
			kept = append(kept, link)
			continue
		}
		if _, err := m.occ(ctx, cloud, "files_external:delete", "--yes", strconv.Itoa(link.MountID)); err != nil {
			kept = append(kept, link)
		}
	}
	links = kept
	if err := save(); err != nil {
		return err
	}
	var missing []string
	for site := range sites {
		if !linked[site] {
			missing = append(missing, site)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if _, err := m.occ(ctx, cloud, "app:enable", "files_external"); err != nil {
		return err
	}
	for _, site := range missing {
		if err := m.prepareSiteDir(site); err != nil {
			return err
		}
		output, err := m.occ(ctx, cloud, "files_external:create", "/"+NextcloudFolder(site), "local", "null::null",
			"--config", "datadir="+nextcloudSitesMount+"/"+site+"/"+siteDir)
		if err != nil {
			return err
		}
		ids := mountIDPattern.FindAllString(string(output), -1)
		if len(ids) == 0 {
			return fmt.Errorf("numéro du dossier %s introuvable", NextcloudFolder(site))
		}
		id := ids[len(ids)-1]
		// NE PAS garder un dossier sans groupe : il serait ouvert à tous les
		// comptes de Nextcloud.
		if _, err := m.occ(ctx, cloud, "files_external:applicable", id, "--add-group=admin"); err != nil {
			m.occ(ctx, cloud, "files_external:delete", "--yes", id)
			return err
		}
		mountID, _ := strconv.Atoi(id)
		links = append(links, nextcloudLink{Site: site, MountID: mountID})
		if err := save(); err != nil {
			return err
		}
	}
	return nil
}

// linkedSites : sites reliés à un Nextcloud installé, et présence d'un Nextcloud.
func (m *Manager) linkedSites(instances []Instance) (map[string]bool, bool) {
	linked := map[string]bool{}
	cloud := false
	for _, instance := range instances {
		if instance.App != NextcloudApp {
			continue
		}
		cloud = true
		for _, link := range m.nextcloudLinks(instance.Name) {
			linked[link.Site] = true
		}
	}
	return linked, cloud
}
