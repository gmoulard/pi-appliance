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

package addresses

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Échanges entre la console et le service d'adresses du projet (spécification 10.2) :
//
//	GET    /api/names/{name}  libre ou non (public, aussi pour le site)
//	GET    /api/mine          adresse de l'appliance qui signe
//	POST   /api/names         réserver {name, target}
//	PUT    /api/names/{name}  nouvelle cible {target}, vaut aussi signe de vie
//	DELETE /api/names/{name}  libérer
//
// Toutes les demandes sauf la première sont signées par la clé de l'appliance (Key.Sign).

// DefaultServiceURL : service d'adresses du projet, sur le Pi du projet.
const DefaultServiceURL = "https://adresses.pi-appliance.com"

// Durées et limites (spécification 7.1, section 15).
const (
	// Expiry : une adresse sans signe de vie depuis ce délai est libérée.
	Expiry = 90 * 24 * time.Hour
	// Cooldown : délai avant qu'un nom libéré puisse être repris par une autre appliance.
	Cooldown = 30 * 24 * time.Hour
	// Heartbeat : la console donne signe de vie au moins à ce rythme.
	Heartbeat = 24 * time.Hour
	// ReservationsPerIP : nouvelles réservations par adresse IP et par jour.
	ReservationsPerIP = 3
	// ReservationsPerDay : nouvelles réservations par jour, toutes appliances confondues.
	ReservationsPerDay = 200
	// TargetChangesPerDay : changements de cible par adresse et par jour.
	TargetChangesPerDay = 10
	// ChecksPerMinute : questions « ce nom est-il libre ? » par adresse IP et par minute.
	ChecksPerMinute = 30
)

// Availability : réponse de GET /api/names/{name}.
type Availability struct {
	Name      string `json:"name"`
	Hostname  string `json:"hostname,omitempty"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Request : corps de POST /api/names et de PUT /api/names/{name}.
type Request struct {
	Name   string `json:"name,omitempty"`
	Target string `json:"target"`
}

// Record : adresse réservée, telle que le service la renvoie à son appliance.
type Record struct {
	Name     string    `json:"name"`
	Hostname string    `json:"hostname"`
	Target   string    `json:"target"`
	Reserved time.Time `json:"reserved"`
	LastSeen time.Time `json:"last_seen"`
}

// ErrorBody : corps d'une réponse en erreur, message en langage courant.
type ErrorBody struct {
	Error string `json:"error"`
}

var targetPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.duckdns\.org$`)

// CheckTarget vérifie la cible d'un alias : un nom DuckDNS (spécification 7.1).
// Le relais du projet (TODO-3.9) s'ajoutera ici.
func CheckTarget(target string) (string, error) {
	target = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(target)), ".")
	if !targetPattern.MatchString(target) {
		return "", fmt.Errorf("%w : l'adresse doit pointer vers un nom DuckDNS (<nom>.duckdns.org)", ErrInvalid)
	}
	return target, nil
}
