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

package addresses

import (
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// Échanges entre la console et le service d'adresses du projet (spécification 10.2) :
//
//	GET    /api/names/{name}  libre ou non (public, aussi pour le site)
//	GET    /api/mine          adresse de l'appliance qui signe
//	POST   /api/names         réserver {name, ip}
//	PUT    /api/names/{name}  adresse IP {ip}, vaut aussi signe de vie
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
	// IPCheck : la console regarde l'adresse IP publique de la box à ce rythme
	// et ne l'envoie au service que si elle a changé (décision du 2026-11-26).
	IPCheck = 5 * time.Minute
	// DailyFrom, DailySpan : envoi quotidien, signe de vie, à une heure tirée au
	// hasard entre 5 h et 8 h, heure du Pi (décision du 2026-11-26).
	DailyFrom = 5 * time.Hour
	DailySpan = 3 * time.Hour
	// ReservationsPerIP : nouvelles réservations par adresse IP et par jour.
	ReservationsPerIP = 3
	// ReservationsPerDay : nouvelles réservations par jour, toutes appliances confondues.
	ReservationsPerDay = 200
	// IPChangesPerDay : changements d'adresse IP par adresse et par jour.
	IPChangesPerDay = 10
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
	Name string `json:"name,omitempty"`
	IP   string `json:"ip"`
}

// Record : adresse réservée, telle que le service la renvoie à son appliance.
type Record struct {
	Name     string    `json:"name"`
	Hostname string    `json:"hostname"`
	IP       string    `json:"ip"`
	Reserved time.Time `json:"reserved"`
	LastSeen time.Time `json:"last_seen"`
}

// ErrorBody : corps d'une réponse en erreur, message en langage courant.
type ErrorBody struct {
	Error string `json:"error"`
}

// CheckIP vérifie l'adresse IP envoyée par l'appliance : l'adresse IPv4
// publique de sa box, écrite en enregistrement A (spécification 7.1).
func CheckIP(ip string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !address.Is4() || !address.IsGlobalUnicast() || address.IsPrivate() || isUnroutable(address) {
		return "", fmt.Errorf("%w : l'adresse IP publique de la box est attendue (IPv4)", ErrInvalid)
	}
	return address.String(), nil
}

// unroutable : plages IPv4 qui ne sont pas joignables sur internet, en plus des
// plages privées (RFC 6890) : partage d'opérateur, documentation, essais.
var unroutable = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
}

func isUnroutable(address netip.Addr) bool {
	for _, prefix := range unroutable {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
