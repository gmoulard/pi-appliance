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

// Package tunnel : relais du projet pour les appliances sans accès direct
// (spécification 7.3, TODO-3.9). L'appliance ouvre des connexions sortantes
// vers le relais ; le relais y fait passer les visiteurs de son adresse.
//
// Échanges, sur une connexion TLS vers relais.pi-appliance.com:443 :
//
//	POST   /api/tunnel  demande de place, signée par la clé de l'appliance
//	                    (addresses.Key.Sign) ; 200 : la connexion reste ouverte
//	                    et attend un visiteur ; 409 : liste d'attente ;
//	                    429 : quota du mois atteint ; 403 : refusé
//	DELETE /api/tunnel  rend la place (retour à l'accès direct), signée
//
// Après un 200, le relais écrit, quand un visiteur arrive, une ligne
// « VISIT <port> <adresse du visiteur> » ; l'appliance répond « OK » une fois
// sa propre connexion vers Traefik ouverte (sinon « KO »), puis les octets
// passent tels quels. Le relais ne déchiffre rien : il lit seulement le nom
// demandé (SNI en HTTPS, en-tête Host en HTTP) pour choisir l'appliance.
package tunnel

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// DefaultRelay : relais du projet, sur le Pi du projet (spécification 10.2).
const DefaultRelay = "relais.pi-appliance.com:443"

// Path : chemin des demandes de l'appliance au relais.
const Path = "/api/tunnel"

// Offre du relais (spécification 7.3) : gratuite, sans garantie.
const (
	// MonthlyBytes : 20 Go par mois et par appliance, les deux sens comptés.
	MonthlyBytes = 20_000_000_000
	// RateBits : 5 Mbit/s par appliance, dans chaque sens.
	RateBits = 5_000_000
)

// Réglages techniques du tunnel.
const (
	// IdleTimeout : une connexion en attente de visiteur est refaite à ce
	// rythme, pour ne pas être coupée en silence par une box ou un opérateur.
	IdleTimeout = 4 * time.Minute
	// AnswerTimeout : délai de la réponse « OK » de l'appliance à un visiteur.
	AnswerTimeout = 10 * time.Second
	// Retry : nouvel essai d'une appliance en liste d'attente, au quota
	// atteint ou après une erreur.
	Retry = 5 * time.Minute
)

// État d'une appliance auprès du relais, dans Status.State.
const (
	StateReady   = "ready"   // place obtenue, connexions en attente de visiteurs
	StateWaiting = "waiting" // liste d'attente, Status.Position
	StateQuota   = "quota"   // 20 Go du mois atteints, jusqu'au mois suivant
	StateRefused = "refused" // pas d'adresse pi-appliance.com, ou demande refusée
	StateOffline = "offline" // relais injoignable (état du client seulement)
)

// Status : réponse du relais à POST /api/tunnel (et état du client).
type Status struct {
	State    string `json:"state"`
	Position int    `json:"position,omitempty"`
	// Message : phrase en langage courant, en français.
	Message string `json:"message,omitempty"`
}

// Visit : visiteur annoncé par le relais.
type Visit struct {
	// Port : 80 ou 443, le port demandé par le visiteur.
	Port int
	// From : adresse du visiteur sur internet.
	From netip.AddrPort
}

// Line : ligne « VISIT » écrite par le relais.
func (v Visit) Line() string {
	return fmt.Sprintf("VISIT %d %s\n", v.Port, v.From)
}

// ParseVisit lit une ligne « VISIT », sans le saut de ligne.
func ParseVisit(line string) (Visit, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "VISIT" {
		return Visit{}, fmt.Errorf("ligne du relais illisible")
	}
	port, err := strconv.Atoi(fields[1])
	if err != nil || (port != 80 && port != 443) {
		return Visit{}, fmt.Errorf("port du relais illisible")
	}
	from, err := netip.ParseAddrPort(fields[2])
	if err != nil {
		return Visit{}, fmt.Errorf("adresse du visiteur illisible")
	}
	return Visit{Port: port, From: from}, nil
}

// ProxyHeader : en-tête PROXY protocol v1 qui donne à Traefik l'adresse du
// visiteur au lieu de celle de l'appliance elle-même.
// NE PAS envoyer une visite relayée à Traefik sans cet en-tête : sans lui, le
// visiteur d'internet aurait l'adresse locale du Pi et passerait pour la maison
// (console et services « maison seulement » ouverts, spécification 8.3).
func ProxyHeader(v Visit, local netip.AddrPort) string {
	from := v.From.Addr().Unmap()
	to := local.Addr().Unmap()
	family := "TCP4"
	if from.Is6() || to.Is6() {
		family = "TCP6"
		from, to = netip.AddrFrom16(from.As16()), netip.AddrFrom16(to.As16())
	}
	return fmt.Sprintf("PROXY %s %s %s %d %d\r\n", family, from, to, v.From.Port(), local.Port())
}
