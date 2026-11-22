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
	"sort"
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

// Version : une version publiée, telle que pia update list la montre.
type Version struct {
	Version string `json:"version"`
	// Preview : préversion (canal bêta seulement).
	Preview bool `json:"preview"`
	// Installable : l'archive de mise à jour est publiée et la console sait
	// l'installer ici ; sinon, il faut flasher l'image de cette version.
	Installable bool `json:"installable"`
	// Current : la version en service.
	Current bool `json:"current"`
	// Newer : plus récente que la version en service.
	Newer bool `json:"newer"`
}

// Versions rend les versions publiées récemment sur le canal choisi, la plus
// récente en premier.
func (c *Checker) Versions(ctx context.Context) ([]Version, error) {
	releases, err := c.list(ctx)
	if err != nil {
		return nil, err
	}
	versions := []Version{}
	for i := range releases {
		release := &releases[i]
		versions = append(versions, Version{
			Version:     release.Tag,
			Preview:     release.Preview(),
			Installable: c.installable(release),
			Current:     strings.TrimPrefix(release.Tag, "v") == strings.TrimPrefix(c.Version, "v"),
			Newer:       Newer(c.Version, release.Tag),
		})
	}
	return versions, nil
}

// list : versions publiées permises sur le canal, numérotées, la plus récente
// en premier.
func (c *Checker) list(ctx context.Context) ([]Release, error) {
	lister, ok := c.Releases.(ReleaseLister)
	if !ok {
		return nil, Failure{Code: "unavailable", Message: "La liste des versions n'est pas disponible sur cette console."}
	}
	all, err := lister.List(ctx)
	if err != nil {
		c.logf("versions de pi-appliance : %v", err)
		return nil, Failure{Code: "offline", Message: "Impossible de lire la liste des versions : le site de publication ne répond pas."}
	}
	channel := c.Channel()
	var releases []Release
	for _, release := range all {
		if _, numbered := parseVersion(strings.TrimPrefix(release.Tag, "v")); numbered && release.Allowed(channel) {
			releases = append(releases, release)
		}
	}
	sort.SliceStable(releases, func(i, j int) bool { return Newer(releases[j].Tag, releases[i].Tag) })
	return releases, nil
}

// InstallAppliance lance l'installation de la dernière version publiée : le
// téléchargement se poursuit après la réponse et se suit dans Status
// (appliance.download, puis system.upgrade).
func (c *Checker) InstallAppliance(ctx context.Context) (string, error) {
	c.mu.Lock()
	release := c.release
	c.mu.Unlock()
	if release == nil || !release.Allowed(c.Channel()) || !Newer(c.Version, release.Tag) {
		if c.downloading() {
			return "", Failure{Code: "busy", Message: "La nouvelle version est déjà en cours de téléchargement."}
		}
		return "", Failure{Code: "nothing", Message: "pi-appliance est à jour."}
	}
	return c.start(ctx, release)
}

// InstallVersion lance l'installation d'une version publiée choisie
// (« v0.1.6 » ou « 0.1.6 »), plus récente ou plus ancienne que la version en
// service ; vide : la dernière version (InstallAppliance).
func (c *Checker) InstallVersion(ctx context.Context, tag string) (string, error) {
	if tag == "" {
		return c.InstallAppliance(ctx)
	}
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	if !versionPattern.MatchString(tag) {
		return "", Failure{Code: "version", Message: "Numéro de version illisible : " + tag + "."}
	}
	if strings.TrimPrefix(tag, "v") == strings.TrimPrefix(c.Version, "v") {
		return "", Failure{Code: "nothing", Message: "Cette version est déjà celle du Pi."}
	}
	releases, err := c.list(ctx)
	if err != nil {
		return "", err
	}
	for i := range releases {
		if releases[i].Tag == tag {
			return c.start(ctx, &releases[i])
		}
	}
	return "", Failure{Code: "unknown", Message: "La version " + tag + " n'est pas publiée sur le canal " + c.Channel() + " (voir la liste des versions)."}
}

func (c *Checker) downloading() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.download != nil && c.download.State == "downloading"
}

// start télécharge puis installe release, après la réponse.
func (c *Checker) start(ctx context.Context, release *Release) (string, error) {
	if c.downloading() {
		return "", Failure{Code: "busy", Message: "La nouvelle version est déjà en cours de téléchargement."}
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
