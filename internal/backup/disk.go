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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/storage"
	"github.com/gmoulard/pi-appliance/internal/system"
)

// Disque USB de sauvegarde, autre que celui des données (spécification 9.1),
// par le script backup-disk.sh de l'image : un disque déjà en ext4 est
// utilisé tel quel, sinon la console propose de le formater, avec accord.

const (
	// DefaultScript est le script installé par l'image.
	DefaultScript = "/usr/local/lib/pi-appliance/backup-disk.sh"
	// DiskMount : point de montage du disque de sauvegarde (BACKUP_MOUNT de backup-disk.sh).
	DiskMount = "/mnt/pi-appliance-backup"
)

// DiskTarget rend la destination des sauvegardes sur le disque monté sur
// mount, un dossier par appliance ; ErrNoDisk si rien n'y est monté (sinon
// les sauvegardes rempliraient la carte SD).
func DiskTarget(mount, host string) func() (string, error) {
	return func() (string, error) {
		if system.MountedDisk(mount) == nil {
			return "", ErrNoDisk
		}
		return filepath.Join(mount, "pi-appliance-backups", host), nil
	}
}

// Hostname : nom de l'appliance pour son dossier de sauvegardes.
func Hostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" || !filepath.IsLocal(host) || strings.ContainsAny(host, `/\`) {
		return "pi-appliance"
	}
	return host
}

// Target : le disque de sauvegarde en service.
type Target struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint"`
}

// DiskStatus : réponse de « backup-disk.sh status » ; Disks ne contient pas
// le disque des données.
type DiskStatus struct {
	Disks  []storage.Disk `json:"disks"`
	Target *Target        `json:"target"`
}

// Disks parle à backup-disk.sh (même Runner que storage.sh).
type Disks struct {
	Runner storage.Runner
}

var devicePattern = regexp.MustCompile(`^/dev/[a-z][a-z0-9]*$`)

func (d *Disks) run(ctx context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := d.Runner.Run(ctx, nil, args...)
	if err != nil {
		var failure storage.Failure
		if json.Unmarshal(out, &failure) == nil && failure.Code != "" {
			return nil, failure
		}
		return nil, fmt.Errorf("backup-disk.sh %s : %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func (d *Disks) Status(ctx context.Context) (DiskStatus, error) {
	out, err := d.run(ctx, 30*time.Second, "status")
	if err != nil {
		return DiskStatus{}, err
	}
	var status DiskStatus
	if err := json.Unmarshal(out, &status); err != nil {
		return DiskStatus{}, fmt.Errorf("backup-disk.sh status : réponse illisible : %w", err)
	}
	if status.Disks == nil {
		status.Disks = []storage.Disk{}
	}
	return status, nil
}

// Use monte une partition ext4 comme disque de sauvegarde.
func (d *Disks) Use(ctx context.Context, partition string) error {
	if !devicePattern.MatchString(partition) {
		return fmt.Errorf("%w : partition inconnue", ErrInvalid)
	}
	_, err := d.run(ctx, time.Minute, "use", partition)
	return err
}

// Forget démonte le disque de sauvegarde et le retire de /etc/fstab, sans
// rien effacer dessus.
func (d *Disks) Forget(ctx context.Context) error {
	_, err := d.run(ctx, time.Minute, "forget")
	return err
}

// Format efface un disque, y crée une partition ext4 et l'utilise.
func (d *Disks) Format(ctx context.Context, disk string) error {
	if !devicePattern.MatchString(disk) {
		return fmt.Errorf("%w : disque inconnu", ErrInvalid)
	}
	_, err := d.run(ctx, 10*time.Minute, "format", disk)
	return err
}
