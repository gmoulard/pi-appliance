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

// Package addresses porte ce que la console et le service d'adresses du projet
// partagent sur les noms <nom>.pi-appliance.com (spécification 7.1, 10.2).
package addresses

import (
	"errors"
	"fmt"
	"strings"
)

// Zone : domaine sous lequel les appliances reçoivent leur adresse.
const Zone = "pi-appliance.com"

// Longueur d'un nom, sans la zone.
const (
	MinLength = 3
	MaxLength = 30
)

// ErrInvalid : le nom ne suit pas les règles ; le message dit pourquoi, en langage courant.
var ErrInvalid = errors.New("nom refusé")

// reserved : noms gardés par le projet, pour ses services, son site ou ses
// machines (dont les sous-domaines déjà présents dans la zone, docs/environnement.md).
var reserved = []string{
	"www", "mail", "smtp", "imap", "pop", "mx", "webmail", "autoconfig", "autodiscover",
	"relais", "relay", "envoi", "adresses", "addresses", "address", "stats", "statistiques",
	"api", "admin", "console", "pi-appliance", "piappliance", "appliance",
	"support", "aide", "help", "docs", "doc", "status", "blog", "shop", "boutique",
	"download", "telechargement", "update", "updates", "marketplace", "catalogue",
	"ns", "ns1", "ns2", "dns", "ftp", "vpn", "test", "dev", "staging", "beta",
	"localhost", "local", "root", "abuse", "postmaster", "hostmaster", "security",
	"mac", "ia4ccm", "todrop",
}

// forbidden : marques et termes injurieux refusés partout dans le nom.
// NE PAS y mettre de mots courts qui se retrouvent dans des noms ordinaires.
var forbidden = []string{
	// Marques
	"raspberry", "google", "apple", "microsoft", "amazon", "facebook", "instagram",
	"whatsapp", "paypal", "netflix", "nextcloud", "duckdns", "github",
	"freebox", "livebox", "bouygues", "sfr", "ovh", "banque", "bank",
	// Termes injurieux
	"connard", "connasse", "salope", "encule", "putain",
	"fuck", "shit", "bitch", "nazi", "hitler", "porn", "porno",
}

// Hostname renvoie l'adresse complète d'un nom valide.
func Hostname(name string) string {
	return name + "." + Zone
}

// Normalize met le nom en minuscules et retire les espaces et la zone tapés par erreur.
func Normalize(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".")
	return strings.TrimSuffix(name, "."+Zone)
}

// Check vérifie un nom déjà normalisé (Normalize) ; l'erreur enveloppe ErrInvalid.
func Check(name string) error {
	refuse := func(reason string) error { return fmt.Errorf("%w : %s", ErrInvalid, reason) }
	if name == "" {
		return refuse("choisissez un nom")
	}
	if len(name) < MinLength {
		return refuse(fmt.Sprintf("le nom doit faire au moins %d caractères", MinLength))
	}
	if len(name) > MaxLength {
		return refuse(fmt.Sprintf("le nom doit faire au plus %d caractères", MaxLength))
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return refuse("utilisez seulement des lettres sans accent, des chiffres et des tirets")
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return refuse("le nom ne peut pas commencer ni finir par un tiret")
	}
	if strings.Contains(name, "--") {
		return refuse("le nom ne peut pas contenir deux tirets de suite")
	}
	for _, r := range reserved {
		if name == r {
			return refuse("ce nom est réservé au projet")
		}
	}
	flat := strings.ReplaceAll(name, "-", "")
	for _, f := range forbidden {
		if strings.Contains(flat, f) {
			return refuse("ce nom n'est pas accepté")
		}
	}
	return nil
}
