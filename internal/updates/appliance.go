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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Installation d'une nouvelle version de pi-appliance, à la demande de
// l'administrateur seulement : la console télécharge l'archive de mise à jour
// jointe à la version publiée (image/update-archive.sh), la vérifie avec sa
// somme de contrôle, puis passe la main à « updates.sh appliance », qui
// l'installe hors du service de la console et remet l'ancienne version si la
// nouvelle ne démarre pas.

const (
	maxArchive      = 256 << 20
	maxChecksum     = 4 << 10
	downloadTimeout = 30 * time.Minute
)

// versionPattern : numéros acceptés par updates.sh (valid_version).
var versionPattern = regexp.MustCompile(`^v[0-9][0-9A-Za-z.-]{0,62}$`)

// Download : téléchargement de la nouvelle version ; State vaut downloading
// ou failed.
type Download struct {
	State   string `json:"state"`
	Version string `json:"version"`
	Message string `json:"message,omitempty"`
}

// ArchiveName : nom de l'archive de mise à jour d'une version.
func ArchiveName(version string) string {
	return "pi-appliance-" + version + "-arm64.tar.gz"
}

func (c *Checker) installable(release *Release) bool {
	if c.Runner == nil || c.DownloadDir == "" || release == nil || !versionPattern.MatchString(release.Tag) || !release.Allowed(c.Channel()) {
		return false
	}
	_, archive := release.Asset(ArchiveName(release.Tag))
	_, checksum := release.Asset(ArchiveName(release.Tag) + ".sha256")
	return archive && checksum
}

// InstallAppliance lance l'installation de la dernière version publiée : le
// téléchargement se poursuit après la réponse et se suit dans Status
// (appliance.download, puis system.upgrade).
func (c *Checker) InstallAppliance(ctx context.Context) (string, error) {
	c.mu.Lock()
	release := c.release
	busy := c.download != nil && c.download.State == "downloading"
	c.mu.Unlock()
	if busy {
		return "", Failure{Code: "busy", Message: "La nouvelle version est déjà en cours de téléchargement."}
	}
	if release == nil || !release.Allowed(c.Channel()) || !Newer(c.Version, release.Tag) {
		return "", Failure{Code: "nothing", Message: "pi-appliance est à jour."}
	}
	if !c.installable(release) {
		return "", Failure{Code: "unavailable", Message: "Cette version ne s'installe pas depuis la console : il faut flasher la nouvelle image."}
	}
	system, err := c.SystemStatus(ctx)
	if err != nil {
		return "", err
	}
	if system.Busy() || system.Upgrading() {
		return "", Failure{Code: "busy", Message: "Une installation est déjà en cours."}
	}
	c.mu.Lock()
	if c.download != nil && c.download.State == "downloading" {
		c.mu.Unlock()
		return "", Failure{Code: "busy", Message: "La nouvelle version est déjà en cours de téléchargement."}
	}
	c.download = &Download{State: "downloading", Version: release.Tag}
	c.mu.Unlock()
	go c.install(*release)
	return release.Tag, nil
}

func (c *Checker) install(release Release) {
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	path, err := c.fetch(ctx, release)
	if err == nil {
		_, err = c.run(ctx, statusTimeout, "appliance", path, release.Tag)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.download = nil
		return
	}
	c.logf("nouvelle version de pi-appliance %s : %v", release.Tag, err)
	message := "Le téléchargement de la nouvelle version a échoué. Rien n'a été changé ; réessayez plus tard."
	if failure, ok := err.(Failure); ok {
		message = failure.Message
	}
	c.download = &Download{State: "failed", Version: release.Tag, Message: message}
}

// fetch télécharge l'archive et la vérifie avec sa somme de contrôle (SHA-256) ;
// rend son chemin dans DownloadDir.
func (c *Checker) fetch(ctx context.Context, release Release) (string, error) {
	name := ArchiveName(release.Tag)
	archive, _ := release.Asset(name)
	checksumAsset, _ := release.Asset(name + ".sha256")
	if archive.Size > maxArchive {
		return "", fmt.Errorf("archive trop grosse : %d octets", archive.Size)
	}
	var checksum strings.Builder
	if err := c.get(ctx, checksumAsset.URL, &checksum, maxChecksum); err != nil {
		return "", err
	}
	fields := strings.Fields(checksum.String())
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", fmt.Errorf("somme de contrôle illisible")
	}
	want, err := hex.DecodeString(fields[0])
	if err != nil {
		return "", fmt.Errorf("somme de contrôle illisible : %w", err)
	}
	if err := os.MkdirAll(c.DownloadDir, 0o700); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(c.DownloadDir, ".download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	hash := sha256.New()
	err = c.get(ctx, archive.URL, io.MultiWriter(temp, hash), maxArchive)
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), hex.EncodeToString(want)) {
		return "", Failure{Code: "checksum", Message: "Le fichier téléchargé est abîmé (somme de contrôle fausse). Rien n'a été changé ; réessayez plus tard."}
	}
	path := filepath.Join(c.DownloadDir, name)
	if err := os.Rename(temp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func (c *Checker) get(ctx context.Context, url string, w io.Writer, limit int64) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("adresse de téléchargement hors HTTPS : %s", url)
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "pi-appliance")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.Request.URL.Scheme != "https" {
		return fmt.Errorf("téléchargement redirigé hors HTTPS")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("téléchargement : réponse %d", response.StatusCode)
	}
	n, err := io.Copy(w, io.LimitReader(response.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("fichier trop gros (%d octets au plus)", limit)
	}
	return nil
}
