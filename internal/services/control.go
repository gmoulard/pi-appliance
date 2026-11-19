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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// Couleur de l'état d'un service dans la console (spécification 5.2).
type Color string

const (
	Green  Color = "green"
	Orange Color = "orange"
	Red    Color = "red"
)

type Status struct {
	Color   Color  `json:"color"`
	Message string `json:"message"`
}

type Action string

const (
	Start   Action = "start"
	Stop    Action = "stop"
	Restart Action = "restart"
)

// container reprend les champs utiles de « docker compose ps --format json ».
type container struct {
	Service string `json:"Service"`
	State   string `json:"State"`
	Health  string `json:"Health"`
}

func (m *Manager) installed(name string) error {
	if !marketplace.ValidLabel(name) {
		return fmt.Errorf("%w : %s", ErrNotFound, name)
	}
	if _, err := os.Stat(filepath.Join(m.serviceDir(name), stateName)); err != nil {
		return fmt.Errorf("%w : %s", ErrNotFound, name)
	}
	return nil
}

// Control démarre, arrête ou redémarre un service installé.
func (m *Manager) Control(ctx context.Context, name string, action Action) error {
	var args []string
	var verb string
	switch action {
	case Start:
		args, verb = []string{"up", "--detach", "--wait"}, "démarré"
	case Stop:
		args, verb = []string{"stop"}, "arrêté"
	case Restart:
		args, verb = []string{"restart"}, "redémarré"
	default:
		return fmt.Errorf("%w : action « %s » inconnue (start, stop ou restart)", ErrInvalid, action)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.installed(name); err != nil {
		return err
	}
	if output, err := m.compose(ctx, name, args...); err != nil {
		return fmt.Errorf("le service %s n'a pas pu être %s : %s", name, verb, trimOutput(output, err))
	}
	return nil
}

// Status lit l'état des conteneurs d'un service et le résume en vert, orange ou rouge.
func (m *Manager) Status(ctx context.Context, name string) (Status, error) {
	if err := m.installed(name); err != nil {
		return Status{}, err
	}
	output, err := m.compose(ctx, name, "ps", "--all", "--format", "json")
	if err != nil {
		return Status{Color: Orange, Message: "État inconnu : Docker ne répond pas."}, nil
	}
	containers, err := parsePS(output)
	if err != nil {
		return Status{Color: Orange, Message: "État inconnu : réponse de Docker illisible."}, nil
	}
	return summarize(containers), nil
}

// parsePS accepte les deux formats de Docker Compose : un tableau JSON (anciennes versions)
// ou un objet JSON par ligne (versions récentes).
func parsePS(output []byte) ([]container, error) {
	output = bytes.TrimSpace(output)
	if len(output) == 0 {
		return nil, nil
	}
	var containers []container
	if output[0] == '[' {
		err := json.Unmarshal(output, &containers)
		return containers, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for decoder.More() {
		var c container
		if err := decoder.Decode(&c); err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}
	return containers, nil
}

func summarize(containers []container) Status {
	var running, starting, unhealthy int
	for _, c := range containers {
		switch {
		case c.State == "restarting" || (c.State == "running" && c.Health == "starting"):
			starting++
		case c.State == "running" && c.Health == "unhealthy":
			unhealthy++
		case c.State == "running":
			running++
		}
	}
	total := len(containers)
	switch {
	case total == 0 || running+starting+unhealthy == 0:
		return Status{Color: Red, Message: "Arrêté."}
	case running == total:
		return Status{Color: Green, Message: "En marche."}
	case starting > 0 && running+starting == total:
		return Status{Color: Orange, Message: "Démarrage en cours…"}
	case unhealthy > 0:
		return Status{Color: Orange, Message: "En marche, mais en difficulté."}
	default:
		return Status{Color: Orange, Message: "Une partie du service est arrêtée."}
	}
}
