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

// Package storage pilote le disque USB des données des services par le script
// storage.sh de l'image (spécification 4) : disques branchés, déplacement des
// données de Docker sur le disque, formatage en ext4.
package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultScript est le script installé par l'image.
	DefaultScript = "/usr/local/lib/pi-appliance/storage.sh"
	// DataMount : point de montage du disque USB des données (DATA_MOUNT de storage.sh).
	DataMount = "/mnt/pi-appliance-data"
)

// ErrInvalid : demande refusée avant d'appeler storage.sh.
var ErrInvalid = errors.New("demande invalide")

// Runner lance storage.sh avec ses arguments (network.Script convient).
type Runner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

// Failure est un refus de storage.sh : code pour la console, message en langage courant.
type Failure struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (f Failure) Error() string {
	return f.Message
}

// Disk : un disque USB branché, vu par sa première partition.
type Disk struct {
	Device     string  `json:"device"`
	Model      string  `json:"model"`
	SizeMB     int64   `json:"sizeMB"`
	Partitions int     `json:"partitions"`
	Partition  *string `json:"partition"`
	FSType     string  `json:"fstype"`
	Label      string  `json:"label"`
	Mounted    bool    `json:"mounted"`
}

// Data : la partition qui porte les données des services, nil sur la carte SD.
type Data struct {
	Device     string `json:"device"`
	Mountpoint string `json:"mountpoint"`
}

// Move : dernier déplacement des données ; State vaut starting, formatting,
// copying, done ou failed.
type Move struct {
	State   string `json:"state"`
	Error   string `json:"error"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// Status : réponse de « storage.sh status ».
type Status struct {
	Disks []Disk `json:"disks"`
	Data  *Data  `json:"data"`
	Move  *Move  `json:"move"`
}

// Busy dit si un déplacement est en cours.
func (s Status) Busy() bool {
	return s.Move != nil && (s.Move.State == "starting" || s.Move.State == "formatting" || s.Move.State == "copying")
}

// Client parle à storage.sh.
type Client struct {
	Runner Runner
}

const timeout = 30 * time.Second

var devicePattern = regexp.MustCompile(`^/dev/[a-z][a-z0-9]*$`)

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := c.Runner.Run(ctx, nil, args...)
	if err != nil {
		var failure Failure
		if json.Unmarshal(out, &failure) == nil && failure.Code != "" {
			return nil, failure
		}
		return nil, fmt.Errorf("storage.sh %s : %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// Status rend les disques USB, le disque des données et le déplacement en cours.
func (c *Client) Status(ctx context.Context) (Status, error) {
	out, err := c.run(ctx, "status")
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(out, &status); err != nil {
		return Status{}, fmt.Errorf("storage.sh status : réponse illisible : %w", err)
	}
	if status.Disks == nil {
		status.Disks = []Disk{}
	}
	return status, nil
}

// Use lance le déplacement des données sur une partition ext4 d'un disque USB ;
// il se poursuit après la réponse et se suit dans Status.
func (c *Client) Use(ctx context.Context, partition string) error {
	if !devicePattern.MatchString(partition) {
		return fmt.Errorf("%w : partition inconnue", ErrInvalid)
	}
	_, err := c.run(ctx, "use", partition)
	return err
}

// Format efface un disque USB, y crée une partition ext4 et y déplace les
// données ; il se poursuit après la réponse et se suit dans Status.
func (c *Client) Format(ctx context.Context, disk string) error {
	if !devicePattern.MatchString(disk) {
		return fmt.Errorf("%w : disque inconnu", ErrInvalid)
	}
	_, err := c.run(ctx, "format", disk)
	return err
}
