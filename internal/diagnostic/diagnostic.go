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

// Package diagnostic donne l'état du système du Pi et ses journaux, sans écran
// ni ligne de commande (spécification 8.5, TODO-2.55), par le script
// diagnostic.sh de l'image ; Window ouvre la page à qui a déposé le fichier
// diagnostic sur la carte SD ; Rescue sert la page sur le port 8443 quand
// Traefik ne répond plus.
package diagnostic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DefaultScript est le script installé par l'image.
const DefaultScript = "/usr/local/lib/pi-appliance/diagnostic.sh"

// Runner lance diagnostic.sh avec ses arguments (network.Script convient).
type Runner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

// Unit : un service du système. State vaut ok, starting, restarting, failed,
// stopped ou missing.
type Unit struct {
	ID       string `json:"id"`
	Unit     string `json:"unit"`
	State    string `json:"state"`
	Restarts int    `json:"restarts"`
	Since    string `json:"since"`
}

// Traefik : son conteneur (running, restarting, exited, absent…) et la
// réponse de la console par HTTPS à travers lui.
type Traefik struct {
	Container string `json:"container"`
	Restarts  int    `json:"restarts"`
	HTTPS     bool   `json:"https"`
}

// Undervoltage : sous-tensions écrites par le noyau ; PreviousBoot nil quand
// le journal du démarrage précédent n'est pas gardé.
type Undervoltage struct {
	ThisBoot     int  `json:"thisBoot"`
	PreviousBoot *int `json:"previousBoot"`
}

// Status : réponse de « diagnostic.sh status ». Errors : dernières erreurs du
// journal de ce démarrage, secrets masqués.
type Status struct {
	At            string       `json:"at"`
	UptimeSeconds int64        `json:"uptimeSeconds"`
	Units         []Unit       `json:"units"`
	Traefik       Traefik      `json:"traefik"`
	Failed        []string     `json:"failed"`
	Errors        []string     `json:"errors"`
	Undervoltage  Undervoltage `json:"undervoltage"`
}

// Client pilote diagnostic.sh.
type Client struct {
	Runner Runner
}

// Status lit l'état du système.
func (c *Client) Status(ctx context.Context) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := c.Runner.Run(ctx, nil, "status")
	if err != nil {
		return Status{}, fmt.Errorf("diagnostic.sh status : %w", err)
	}
	var status Status
	if err := json.Unmarshal(out, &status); err != nil {
		return Status{}, fmt.Errorf("diagnostic.sh status : réponse illisible : %w", err)
	}
	return status, nil
}

// Logs rend les journaux, secrets masqués par diagnostic.sh.
func (c *Client) Logs(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := c.Runner.Run(ctx, nil, "logs")
	if err != nil {
		return nil, fmt.Errorf("diagnostic.sh logs : %w", err)
	}
	return out, nil
}
