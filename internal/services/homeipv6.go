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

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var globalUnicast6 = netip.MustParsePrefix("2000::/3")

func ReadHomeIPv6() []netip.Prefix {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var prefixes []netip.Prefix
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || skippedInterface(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			if ipNet, ok := address.(*net.IPNet); ok {
				if prefix, ok := homeIPv6Prefix(ipNet); ok && !slices.Contains(prefixes, prefix) {
					prefixes = append(prefixes, prefix)
				}
			}
		}
	}
	return sortedPrefixes(prefixes)
}

// NE PAS accepter un préfixe plus court que /48, ni élargir au-delà du /64 une adresse en /128 (DHCPv6, décision du 2026-11-26) : ce serait ouvrir le réseau de l'opérateur.
func homeIPv6Prefix(ipNet *net.IPNet) (netip.Prefix, bool) {
	ip, ok := netip.AddrFromSlice(ipNet.IP)
	if !ok || ip.Is4In6() || !ip.Is6() || !globalUnicast6.Contains(ip) {
		return netip.Prefix{}, false
	}
	bits, size := ipNet.Mask.Size()
	if size == 128 && bits == 128 {
		bits = 64
	}
	if size != 128 || bits < 48 || bits > 64 {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(ip, bits).Masked(), true
}

func sortedPrefixes(prefixes []netip.Prefix) []netip.Prefix {
	slices.SortFunc(prefixes, func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	return prefixes
}

func (m *Manager) readHomeIPv6() []netip.Prefix {
	if m.ReadHomeIPv6 != nil {
		return sortedPrefixes(slices.Clone(m.ReadHomeIPv6()))
	}
	return ReadHomeIPv6()
}

func (m *Manager) HomeIPv6() []netip.Prefix {
	if current := m.homeIPv6.Load(); current != nil {
		return slices.Clone(*current)
	}
	return nil
}

func (m *Manager) InHomeIPv6(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, prefix := range m.HomeIPv6() {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func homeRanges6With(prefixes []netip.Prefix) []string {
	ranges := slices.Clone(homeRanges6)
	for _, prefix := range prefixes {
		ranges = append(ranges, prefix.String())
	}
	return ranges
}

func (m *Manager) RefreshHomeIPv6(ctx context.Context) (bool, error) {
	prefixes := m.readHomeIPv6()
	previous := m.homeIPv6.Load()
	if previous != nil && slices.Equal(*previous, prefixes) {
		return false, nil
	}
	m.homeIPv6.Store(&prefixes)
	var errs []error
	if err := m.writeTraefikHome(prefixes); err != nil {
		errs = append(errs, fmt.Errorf("Traefik, plages de la maison : %w", err))
	}
	if err := m.ApplyHomePorts(ctx); err != nil {
		errs = append(errs, err)
	}
	return true, errors.Join(errs...)
}

func (m *Manager) RunHomeIPv6(ctx context.Context, every time.Duration, logf func(string, ...any)) {
	for {
		if changed, err := m.RefreshHomeIPv6(ctx); err != nil {
			logf("préfixe IPv6 de la maison : %v", err)
		} else if changed {
			logf("préfixe IPv6 de la maison : %v", m.HomeIPv6())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

const traefikHomeHeader = `---
# This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
# Copyright (c) 2026 Guillaume Moulard.
#
# Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
# 4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
# of the LICENSE file at the root of the project: this file may be shared and
# integrated into another project unmodified, keeping this notice and a link to
# the project; modified versions may only be published to propose them to the
# official repository. Commercial use requires a separate licence from the
# author: guillaume@moulard.org.
#
# See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.

# Réécrit par la console (internal/services/homeipv6.go) : ne pas modifier.
# Plages de la maison (spécification 8.3) : celles de dynamic/console.yml et,
# en plus, le préfixe IPv6 global du réseau de la maison lu sur le Pi.

`

func traefikHome(prefixes []netip.Prefix) string {
	var b strings.Builder
	b.WriteString(traefikHomeHeader)
	b.WriteString("http:\n")
	if len(prefixes) > 0 {
		clients := make([]string, len(prefixes))
		for i, prefix := range prefixes {
			clients[i] = "ClientIP(`" + prefix.String() + "`)"
		}
		match := "(" + strings.Join(clients, " || ") + ")"
		b.WriteString("  routers:\n")
		b.WriteString("    pi-appliance-console-home6:\n")
		b.WriteString("      entryPoints: [websecure]\n")
		fmt.Fprintf(&b, "      rule: \"PathPrefix(`/pi-appliance`) && %s\"\n", match)
		b.WriteString("      priority: 2\n")
		b.WriteString("      service: pi-appliance-console@file\n")
		b.WriteString("      tls: {}\n")
		b.WriteString("    pi-appliance-cockpit-home6:\n")
		b.WriteString("      entryPoints: [websecure]\n")
		fmt.Fprintf(&b, "      rule: \"PathPrefix(`/cockpit`) && %s\"\n", match)
		b.WriteString("      priority: 2\n")
		b.WriteString("      middlewares: [pi-appliance-errors@file, pi-appliance-cockpit@file]\n")
		b.WriteString("      service: pi-appliance-cockpit@file\n")
		b.WriteString("      tls: {}\n")
	}
	b.WriteString("  middlewares:\n")
	b.WriteString("    pi-appliance-home:\n")
	b.WriteString("      ipAllowList:\n")
	b.WriteString("        sourceRange:\n")
	for _, r := range append(slices.Clone(homeRanges4), homeRanges6With(prefixes)...) {
		fmt.Fprintf(&b, "          - %s\n", r)
	}
	return b.String()
}

func (m *Manager) writeTraefikHome(prefixes []netip.Prefix) error {
	if m.TraefikHomeFile == "" {
		return nil
	}
	content := traefikHome(prefixes)
	if current, err := os.ReadFile(m.TraefikHomeFile); err == nil && string(current) == content {
		return nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.TraefikHomeFile), 0o755); err != nil {
		return err
	}
	temporary := m.TraefikHomeFile + ".tmp"
	if err := os.WriteFile(temporary, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, m.TraefikHomeFile)
}
