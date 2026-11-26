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
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHomeIPv6Prefix(t *testing.T) {
	for address, want := range map[string]string{
		"2a01:cb00:1234:5600:1:2:3:4/64": "2a01:cb00:1234:5600::/64",
		"2a01:e0a:12:3400::5/56":         "2a01:e0a:12:3400::/56",
		"2a01:e0a:12::5/48":              "2a01:e0a:12::/48",
		"2a01:cb00:1234:5600::10/128":    "",
		"2a01:cb00::1/32":                "",
		"fd00::5/64":                     "",
		"fe80::1/64":                     "",
		"::1/128":                        "",
		"192.168.1.20/24":                "",
		"::ffff:192.168.1.20/120":        "",
	} {
		ip, ipNet, err := net.ParseCIDR(address)
		if err != nil {
			t.Fatal(err)
		}
		ipNet.IP = ip
		prefix, ok := homeIPv6Prefix(ipNet)
		if got := map[bool]string{true: prefix.String(), false: ""}[ok]; got != want {
			t.Errorf("%s : %q, attendu %q", address, got, want)
		}
	}
}

func TestRefreshHomeIPv6(t *testing.T) {
	dir := t.TempDir()
	prefixes := []netip.Prefix{netip.MustParsePrefix("2a01:cb00:1234:5600::/64")}
	firewall := &firewallRecorder{runner: &fakeRunner{}}
	m := &Manager{Dir: dir, Runner: firewall.runner, Firewall: firewall.load, TraefikHomeFile: filepath.Join(dir, "traefik", "home.yml"),
		ReadHomeIPv6: func() []netip.Prefix { return prefixes }}
	ctx := context.Background()
	if _, err := m.Install(ctx, homeOnlyApp(t), "partage", nil); err != nil {
		t.Fatal(err)
	}
	if changed, err := m.RefreshHomeIPv6(ctx); err != nil || !changed {
		t.Fatalf("première lecture : %v %v", changed, err)
	}
	accepted := func(address string) bool { return m.InHomeIPv6(netip.MustParseAddr(address)) }
	for address, want := range map[string]bool{
		"2a01:cb00:1234:5600::42": true, "2a01:cb00:1234:5600:ffff::1": true,
		"2a01:cb00:1234:5601::42": false, "2a01:cb00:9999::1": false, "2001:db8::1": false, "192.168.1.20": false, "8.8.8.8": false,
	} {
		if accepted(address) != want {
			t.Errorf("%s : %v", address, !want)
		}
	}
	home, err := os.ReadFile(m.TraefikHomeFile)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		HTTP struct {
			Routers     map[string]struct{ Rule, Service string }
			Middlewares map[string]struct {
				IPAllowList struct {
					SourceRange []string `yaml:"sourceRange"`
				} `yaml:"ipAllowList"`
			}
		} `yaml:"http"`
	}
	if err := yaml.Unmarshal(home, &parsed); err != nil {
		t.Fatalf("home.yml illisible : %v\n%s", err, home)
	}
	ranges := parsed.HTTP.Middlewares["pi-appliance-home"].IPAllowList.SourceRange
	if len(ranges) != len(homeRanges4)+len(homeRanges6)+1 || ranges[len(ranges)-1] != "2a01:cb00:1234:5600::/64" {
		t.Fatalf("plages de pi-appliance-home : %v", ranges)
	}
	for _, router := range []string{"pi-appliance-console-home6", "pi-appliance-cockpit-home6"} {
		if !strings.Contains(parsed.HTTP.Routers[router].Rule, "ClientIP(`2a01:cb00:1234:5600::/64`)") {
			t.Fatalf("routeur %s : %+v", router, parsed.HTTP.Routers[router])
		}
	}
	if !strings.HasPrefix(parsed.HTTP.Routers["pi-appliance-console-home6"].Rule, "PathPrefix(`/pi-appliance`) && (") {
		t.Fatalf("routeur de la console : %s", parsed.HTTP.Routers["pi-appliance-console-home6"].Rule)
	}
	last := firewall.loads[len(firewall.loads)-1]
	if !strings.Contains(last, "ip6 saddr != { ::1/128, fc00::/7, fe80::/10, 2a01:cb00:1234:5600::/64 }") {
		t.Fatalf("pare-feu :\n%s", last)
	}
	loads := len(firewall.loads)
	if changed, err := m.RefreshHomeIPv6(ctx); err != nil || changed || len(firewall.loads) != loads {
		t.Fatalf("sans changement : %v %v", changed, err)
	}
	// La box change de préfixe : l'ancien ne vaut plus rien.
	prefixes = []netip.Prefix{netip.MustParsePrefix("2a01:cb00:4321:8800::/64")}
	if changed, err := m.RefreshHomeIPv6(ctx); err != nil || !changed {
		t.Fatalf("nouveau préfixe : %v %v", changed, err)
	}
	if accepted("2a01:cb00:1234:5600::42") || !accepted("2a01:cb00:4321:8800::42") {
		t.Fatal("ancien préfixe encore accepté")
	}
	home, _ = os.ReadFile(m.TraefikHomeFile)
	last = firewall.loads[len(firewall.loads)-1]
	if strings.Contains(string(home), "5600") || strings.Contains(last, "5600") || !strings.Contains(last, "2a01:cb00:4321:8800::/64") {
		t.Fatalf("ancien préfixe :\n%s\n%s", home, last)
	}
	// Plus d'IPv6 globale : retour au fichier livré.
	prefixes = nil
	if _, err := m.RefreshHomeIPv6(ctx); err != nil {
		t.Fatal(err)
	}
	if home, _ = os.ReadFile(m.TraefikHomeFile); string(home) != traefikHome(nil) || strings.Contains(string(home), "routers") {
		t.Fatalf("sans préfixe :\n%s", home)
	}
}
