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
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// homeOnlyApp : un service sans page web, ports TCP 445 et UDP 137 réservés à
// la maison (manifeste de test, d'après celui de WireGuard).
func homeOnlyApp(t *testing.T) marketplace.App {
	t.Helper()
	compose := string(wireGuardApp(t).Compose)
	compose = strings.Replace(compose, "    - port: 51820\n      protocol: udp", "    - port: 445\n      protocol: tcp\n    - port: 137\n      protocol: udp", 1)
	compose = strings.Replace(compose, "access: public", "access: home", 1)
	app, err := marketplace.Parse("partage", []byte(compose))
	if err != nil {
		t.Fatal(err)
	}
	if !homeOnly(app) {
		t.Fatalf("manifeste de test : %+v", app)
	}
	return app
}

// firewallRecorder : remplace « nft -f » ; garde le contenu chargé et les
// appels Docker déjà faits à ce moment-là.
type firewallRecorder struct {
	runner *fakeRunner
	loads  []string
	before []int
	err    error
}

func (f *firewallRecorder) load(_ context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f.loads = append(f.loads, string(data))
	f.before = append(f.before, len(f.runner.calls))
	return f.err
}

func TestHomePortsRules(t *testing.T) {
	empty := homePortsRules(nil)
	if !strings.Contains(empty, "destroy table inet pi_appliance_ports") || strings.Contains(empty, "chain") {
		t.Fatalf("sans port :\n%s", empty)
	}
	rules := homePortsRules([]marketplace.HostPort{{Port: 445, Protocol: "tcp"}, {Port: 139, Protocol: "tcp"}, {Port: 137, Protocol: "udp"}, {Port: 445, Protocol: "tcp"}})
	for _, want := range []string{
		"type filter hook prerouting priority -150; policy accept;",
		"ct state new fib daddr type local ip saddr != { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8 } tcp dport { 139, 445 } counter drop",
		"ct state new fib daddr type local ip6 saddr != { ::1/128, fc00::/7, fe80::/10 } tcp dport { 139, 445 } counter drop",
		"ip saddr != { 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8 } udp dport { 137 } counter drop",
	} {
		if !strings.Contains(rules, want) {
			t.Errorf("règle absente %q :\n%s", want, rules)
		}
	}
}

// Les plages de la maison sont celles du middleware pi-appliance-home@file.
func TestHomeRangesMatchTraefik(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "system", "etc", "pi-appliance", "traefik", "dynamic", "console.yml"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)pi-appliance-home:\s*ipAllowList:\s*sourceRange:\s*((?:\s*- \S+)+)`).FindSubmatch(data)
	if block == nil {
		t.Fatal("pi-appliance-home absent de console.yml")
	}
	var traefik []string
	for _, line := range strings.Split(string(block[1]), "\n") {
		if line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-")); line != "" {
			traefik = append(traefik, line)
		}
	}
	ours := append(append([]string{}, homeRanges4...), homeRanges6...)
	if strings.Join(traefik, " ") != strings.Join(ours, " ") {
		t.Fatalf("plages de Traefik %v, du pare-feu %v", traefik, ours)
	}
}

// Console fermée sur internet (spécification 8.3, TODO-3.11) : le routeur
// pi-appliance-console n'accepte que les plages de la maison, les mêmes que
// pi-appliance-home@file et le pare-feu ; une adresse d'internet n'y est pas.
func TestConsoleRangesMatchHome(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "system", "etc", "pi-appliance", "traefik", "dynamic", "console.yml"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)pi-appliance-console:\s*entryPoints:[^\n]*\n\s*rule: >-(.*?)\n\s*priority:`).FindSubmatch(data)
	if block == nil {
		t.Fatal("routeur pi-appliance-console absent de console.yml")
	}
	if !strings.Contains(string(block[1]), "PathPrefix(`/pi-appliance`) &&") {
		t.Fatalf("règle de la console sans le chemin /pi-appliance : %s", block[1])
	}
	var ranges []string
	for _, match := range regexp.MustCompile("ClientIP\\(`([^`]+)`\\)").FindAllSubmatch(block[1], -1) {
		ranges = append(ranges, string(match[1]))
	}
	ours := append(append([]string{}, homeRanges4...), homeRanges6...)
	if strings.Join(ranges, " ") != strings.Join(ours, " ") {
		t.Fatalf("plages de la console %v, de la maison %v", ranges, ours)
	}
	inside := func(address string) bool {
		ip := netip.MustParseAddr(address)
		for _, r := range ranges {
			if netip.MustParsePrefix(r).Contains(ip) {
				return true
			}
		}
		return false
	}
	// Refusées : adresses d'internet (IPv4, IPv6 globale), adresses partagées
	// de l'opérateur (CGNAT), lien local IPv4.
	for _, address := range []string{"203.0.113.7", "8.8.8.8", "100.64.0.1", "169.254.1.1", "2001:db8::1", "2a01:cb00::1"} {
		if inside(address) {
			t.Errorf("%s acceptée par la console", address)
		}
	}
	// Acceptées : la maison, la boucle locale, Docker (les appareils du VPN,
	// masqués derrière le conteneur WireGuard) et le réseau du VPN lui-même.
	for _, address := range []string{"192.168.1.20", "10.0.0.5", "172.17.0.2", "127.0.0.1", "::1", "fd00::5", "fe80::1", wireGuardNetwork.Addr().Next().Next().String()} {
		if !inside(address) {
			t.Errorf("%s refusée par la console", address)
		}
	}
}

// Les règles sont acceptées par nft (vérification sans rien charger, dans un
// espace réseau à part) quand la machine le permet.
func TestHomePortsRulesNft(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nft absent")
	}
	if _, err := exec.LookPath("unshare"); err != nil || os.Geteuid() != 0 {
		t.Skip("unshare ou root absent")
	}
	path := filepath.Join(t.TempDir(), homePortsFile)
	if err := os.WriteFile(path, []byte(homePortsRules([]marketplace.HostPort{{Port: 445, Protocol: "tcp"}, {Port: 137, Protocol: "udp"}})), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("unshare", "--net", "nft", "-c", "-f", path).CombinedOutput(); err != nil {
		if strings.Contains(string(output), "Operation not permitted") {
			t.Skip("espace réseau refusé")
		}
		t.Fatalf("nft : %v\n%s", err, output)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		return
	}
	// Essai réel, par la boucle locale d'un espace réseau à part : une adresse
	// d'internet (203.0.113.7, ajoutée au Pi) n'ouvre plus le port 445, la
	// maison si ; un autre port reste ouvert à tous. Rechargement compris.
	script := `nft -f "$RULES" && nft -f "$RULES" && python3 -c "$PROBE" 445 && python3 -c "$PROBE" 8445`
	command := exec.Command("unshare", "--net", "sh", "-c", script)
	command.Env = append(os.Environ(), "RULES="+path, "PROBE="+homePortsProbe)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("essai : %v\n%s", err, output)
	}
	if got := strings.Join(strings.Fields(string(output)), " "); got != "445 maison=oui internet=non 8445 maison=oui internet=oui" {
		t.Fatalf("essai : %s", output)
	}
}

// homePortsProbe : allume la boucle locale, lui ajoute 203.0.113.7 (lo:1),
// écoute sur le port donné, puis s'y connecte depuis 127.0.0.1 et depuis
// 203.0.113.7.
const homePortsProbe = `
import fcntl, socket, struct, sys
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
flags = struct.unpack("16sH", fcntl.ioctl(s, 0x8913, struct.pack("16sH14s", b"lo", 0, bytes(14)))[:18])[1]
fcntl.ioctl(s, 0x8914, struct.pack("16sH14s", b"lo", flags | 1, bytes(14)))
fcntl.ioctl(s, 0x8916, struct.pack("16sH2s4s8s", b"lo:1", socket.AF_INET, bytes(2), socket.inet_aton("203.0.113.7"), bytes(8)))
port = int(sys.argv[1])
server = socket.socket()
server.bind(("0.0.0.0", port))
server.listen()
def reach(source):
    c = socket.socket()
    c.settimeout(1)
    c.bind((source, 0))
    try:
        c.connect((source, port))
        return "oui"
    except OSError:
        return "non"
    finally:
        c.close()
print(port, "maison=" + reach("127.0.0.1"), "internet=" + reach("203.0.113.7"))
`

func TestInstallHomeOnlyClosesPortsFirst(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	firewall := &firewallRecorder{runner: runner}
	m.Firewall = firewall.load
	result, err := m.Install(context.Background(), homeOnlyApp(t), "partage", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.Access != AccessHome || !result.Instance.NoWebPage {
		t.Fatalf("instance %+v", result.Instance)
	}
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "box") {
			t.Fatalf("redirection sur la box proposée pour un service de la maison : %v", result.Warnings)
		}
	}
	if len(firewall.loads) != 1 || !strings.Contains(firewall.loads[0], "tcp dport { 445 }") || !strings.Contains(firewall.loads[0], "udp dport { 137 }") {
		t.Fatalf("pare-feu chargé %q", firewall.loads)
	}
	for _, call := range runner.calls[:firewall.before[0]] {
		if strings.Contains(call, "compose") {
			t.Fatalf("service créé avant son pare-feu : %v", runner.calls)
		}
	}
	instances, err := m.List()
	if err != nil || len(instances) != 1 || instances[0].Access != AccessHome {
		t.Fatalf("liste %+v %v", instances, err)
	}
	// Au démarrage de la console : mêmes ports, d'après les services installés.
	if err := m.ApplyHomePorts(context.Background()); err != nil || len(firewall.loads) != 2 || firewall.loads[1] != firewall.loads[0] {
		t.Fatalf("rechargement %v %q", err, firewall.loads)
	}
	if err := m.Remove(context.Background(), "partage", false); err != nil {
		t.Fatal(err)
	}
	if len(firewall.loads) != 3 || strings.Contains(firewall.loads[2], "dport") || !strings.Contains(firewall.loads[2], "destroy table inet pi_appliance_ports") {
		t.Fatalf("après la désinstallation %q", firewall.loads)
	}
}

func TestInstallHomeOnlyRefusedWithoutFirewall(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	firewall := &firewallRecorder{runner: runner, err: errors.New("nft absent")}
	m.Firewall = firewall.load
	if _, err := m.Install(context.Background(), homeOnlyApp(t), "partage", nil); err == nil || !strings.Contains(err.Error(), "pare-feu") {
		t.Fatalf("installé sans pare-feu : %v", err)
	}
	if runner.called("compose") {
		t.Fatalf("service démarré sans pare-feu : %v", runner.calls)
	}
}

func TestWireGuardNeedsNoHomeFirewall(t *testing.T) {
	runner := &fakeRunner{}
	m := wireGuardManager(t, runner)
	firewall := &firewallRecorder{runner: runner}
	m.Firewall = firewall.load
	if _, err := m.Install(context.Background(), wireGuardApp(t), "vpn", nil); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyHomePorts(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(firewall.loads) != 0 {
		t.Fatalf("pare-feu chargé pour un service ouvert à tous : %q", firewall.loads)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, homePortsFile)); !os.IsNotExist(err) {
		t.Fatalf("fichier du pare-feu écrit : %v", err)
	}
}
