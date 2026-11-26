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

package services

// Ports du Pi « maison seulement » (spécification 6.4, TODO-4.8) : un service
// sans page web (base ouverte sur le réseau de la maison, par exemple) n'a pas
// Traefik devant lui ; Docker publie ses host_ports sur toutes les interfaces.
// Quand son manifeste dit « access: home », la console écrit un pare-feu
// nftables (homePortsFile) qui refuse ces ports à toute adresse hors des
// plages de la maison, les mêmes que la console et que pi-appliance-home@file
// (spécification 8.3). Il est posé avant le démarrage du service, rechargé au
// démarrage du Pi par pi-appliance-firewall.service, avant Docker.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// homePortsFile : pare-feu des ports « maison seulement », dans le dossier
// des données ; chargé au démarrage par pi-appliance-firewall.service.
const homePortsFile = "home-ports.nft"

// homeRanges4 et homeRanges6 : plages de la maison (et du VPN, dont le
// conteneur WireGuard masque les appareils derrière son adresse privée).
// NE PAS les élargir sans changer aussi pi-appliance-home@file
// (traefik/dynamic/console.yml) et la console (spécification 8.3).
var (
	homeRanges4 = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8"}
	homeRanges6 = []string{"::1/128", "fc00::/7", "fe80::/10"}
)

// homeOnly : un service sans page web dont les ports ne s'ouvrent qu'à la maison.
func homeOnly(app marketplace.App) bool {
	return app.Port == 0 && app.Access == AccessHome
}

// homePortsRules : le pare-feu des ports donnés. Sans port, la table est
// seulement retirée. La règle est dans « prerouting », avant la redirection
// des ports par Docker (dstnat, -100) : elle vaut pour les ports transférés au
// conteneur comme pour ceux servis par docker-proxy (IPv6). Seules les
// nouvelles connexions vers une adresse du Pi sont regardées : les réponses
// aux connexions sortantes des conteneurs passent.
func homePortsRules(ports []marketplace.HostPort) string {
	var b strings.Builder
	b.WriteString("#!/usr/sbin/nft -f\n")
	b.WriteString("# Écrit par la console pi-appliance (internal/services/homeports.go) : ne pas modifier.\n")
	b.WriteString("# Ports des services « maison seulement » sans page web (spécification 6.4).\n")
	b.WriteString("destroy table inet pi_appliance_ports\n")
	byProtocol := map[string][]int{}
	for _, port := range ports {
		if !slices.Contains(byProtocol[port.Protocol], port.Port) {
			byProtocol[port.Protocol] = append(byProtocol[port.Protocol], port.Port)
		}
	}
	if len(byProtocol) == 0 {
		return b.String()
	}
	b.WriteString("table inet pi_appliance_ports {\n")
	b.WriteString("\tchain prerouting {\n")
	b.WriteString("\t\t# Avant la redirection des ports par Docker (-100) ; « drop » est définitif.\n")
	b.WriteString("\t\ttype filter hook prerouting priority -150; policy accept;\n")
	protocols := make([]string, 0, len(byProtocol))
	for protocol := range byProtocol {
		protocols = append(protocols, protocol)
	}
	sort.Strings(protocols)
	for _, protocol := range protocols {
		numbers := byProtocol[protocol]
		sort.Ints(numbers)
		list := make([]string, len(numbers))
		for i, number := range numbers {
			list[i] = fmt.Sprint(number)
		}
		dport := fmt.Sprintf("%s dport { %s }", protocol, strings.Join(list, ", "))
		fmt.Fprintf(&b, "\t\tct state new fib daddr type local ip saddr != { %s } %s counter drop\n", strings.Join(homeRanges4, ", "), dport)
		fmt.Fprintf(&b, "\t\tct state new fib daddr type local ip6 saddr != { %s } %s counter drop\n", strings.Join(homeRanges6, ", "), dport)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

// nftLoad : charge un fichier de règles nftables.
func nftLoad(ctx context.Context, path string) error {
	output, err := exec.CommandContext(ctx, "nft", "-f", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", trimOutput(output, err))
	}
	return nil
}

// ApplyHomePorts écrit et charge le pare-feu des ports « maison seulement »
// des services installés, plus ceux de extra (service en cours
// d'installation). Sans aucun port et sans fichier déjà écrit, rien n'est fait.
func (m *Manager) ApplyHomePorts(ctx context.Context, extra ...marketplace.HostPort) error {
	instances, err := m.List()
	if err != nil {
		return err
	}
	ports := append([]marketplace.HostPort{}, extra...)
	for _, instance := range instances {
		if instance.NoWebPage && instance.Access == AccessHome {
			ports = append(ports, instance.HostPorts...)
		}
	}
	path := filepath.Join(m.Dir, homePortsFile)
	if _, err := os.Stat(path); len(ports) == 0 && os.IsNotExist(err) {
		return nil
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(homePortsRules(ports)), 0o600); err != nil {
		return err
	}
	load := m.Firewall
	if load == nil {
		load = nftLoad
	}
	if err := load(ctx, path); err != nil {
		return fmt.Errorf("pare-feu des ports de la maison : %v", err)
	}
	return nil
}
