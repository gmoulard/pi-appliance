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

// VPN WireGuard (spécification 6.3) : la console tire les clés sur le Pi,
// écrit la configuration du serveur (wg0.conf) et, pour chaque appareil, une
// configuration montrée une seule fois en texte et en QR code. Les AllowedIPs
// de l'appareil se limitent au réseau de la maison : sa navigation sur
// internet ne passe pas par la maison. La clé privée de l'appareil n'est pas
// gardée sur le Pi.

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gmoulard/pi-appliance/marketplace"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	// WireGuardApp est l'identifiant du manifeste marketplace/wireguard.
	WireGuardApp  = "wireguard"
	wireGuardDir  = "wireguard"
	wireGuardConf = "wg0.conf"
	wireGuardName = "wireguard.json"
	// Port UDP du manifeste, ouvert sur le Pi et à rediriger sur la box.
	WireGuardPort = 51820
)

// Réseau du VPN : le Pi est .1, les appareils .2 à .254.
var wireGuardNetwork = netip.MustParsePrefix("10.13.13.0/24")

// Device est un appareil autorisé à se connecter au VPN.
type Device struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	PublicKey string    `json:"public_key"`
	CreatedAt time.Time `json:"created_at"`
}

type wireGuardState struct {
	PrivateKey string         `json:"private_key"`
	Devices    []storedDevice `json:"devices"`
}

// storedDevice garde la clé partagée, que Device ne montre pas.
type storedDevice struct {
	Device
	PresharedKey string `json:"preshared_key"`
}

// WireGuardStatus est ce que l'écran du VPN affiche.
type WireGuardStatus struct {
	Endpoint     string   `json:"endpoint"`
	Port         int      `json:"port"`
	HomeNetworks []string `json:"home_networks"`
	// EndpointLocal : l'adresse du Pi n'est pas une adresse internet, le
	// téléphone ne le trouvera pas depuis l'extérieur.
	EndpointLocal bool     `json:"endpoint_local"`
	Devices       []Device `json:"devices"`
}

// NewDevice est la configuration d'un appareil, montrée une seule fois.
type NewDevice struct {
	Device Device `json:"device"`
	Config string `json:"config"`
	// QRCode : image PNG en data URL, à scanner avec l'application WireGuard.
	QRCode string `json:"qr_code"`
}

// HomeNetworks donne les réseaux de la maison (IPv4 privés des interfaces du
// Pi, sans Docker, le VPN ni le Wi-Fi de configuration).
func HomeNetworks() []netip.Prefix {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var networks []netip.Prefix
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || skippedInterface(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ipNet, ok := address.(*net.IPNet)
			if !ok {
				continue
			}
			prefix, ok := homePrefix(ipNet)
			if ok && !containsPrefix(networks, prefix) {
				networks = append(networks, prefix)
			}
		}
	}
	return networks
}

func skippedInterface(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "wg", "tun", "uap", "ap"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// accessPoint est le Wi-Fi de configuration (pare-feu ap-firewall.nft).
var accessPoint = netip.MustParsePrefix("10.42.0.0/24")

func homePrefix(ipNet *net.IPNet) (netip.Prefix, bool) {
	ip, ok := netip.AddrFromSlice(ipNet.IP)
	if !ok {
		return netip.Prefix{}, false
	}
	ip = ip.Unmap()
	bits, _ := ipNet.Mask.Size()
	if !ip.Is4() || !ip.IsPrivate() || bits < 8 || bits > 30 {
		return netip.Prefix{}, false
	}
	prefix := netip.PrefixFrom(ip, bits).Masked()
	if prefix.Overlaps(accessPoint) || prefix.Overlaps(wireGuardNetwork) || prefix.Overlaps(netip.MustParsePrefix("172.16.0.0/12")) {
		return netip.Prefix{}, false
	}
	return prefix, true
}

func containsPrefix(list []netip.Prefix, prefix netip.Prefix) bool {
	for _, p := range list {
		if p == prefix {
			return true
		}
	}
	return false
}

func (m *Manager) homeNetworks() []netip.Prefix {
	if m.HomeNetworks != nil {
		return m.HomeNetworks()
	}
	return HomeNetworks()
}

// prepareWireGuard tire la clé du Pi au premier passage (gardée si on
// réinstalle avec les données) et écrit wg0.conf avant le démarrage.
func (m *Manager) prepareWireGuard(dir string) error {
	state, err := readWireGuard(dir)
	if errors.Is(err, os.ErrNotExist) {
		key, keyErr := wireGuardKey()
		if keyErr != nil {
			return keyErr
		}
		state = wireGuardState{PrivateKey: key}
		err = writeJSON(filepath.Join(dir, wireGuardName), state, 0o600)
	}
	if err != nil {
		return err
	}
	return m.writeWireGuardConf(dir, state)
}

func readWireGuard(dir string) (wireGuardState, error) {
	var state wireGuardState
	data, err := os.ReadFile(filepath.Join(dir, wireGuardName))
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("%s : %w", wireGuardName, err)
	}
	return state, nil
}

// writeWireGuardConf écrit la configuration du serveur. Le pare-feu du
// conteneur ne laisse passer vers la maison que le réseau de la maison : le
// VPN ne sert pas de sortie vers internet.
func (m *Manager) writeWireGuardConf(dir string, state wireGuardState) error {
	server := wireGuardNetwork.Addr().Next()
	var conf strings.Builder
	fmt.Fprintf(&conf, "# Écrit par la console pi-appliance : ne pas modifier à la main.\n[Interface]\nAddress = %s/%d\nListenPort = %d\nPrivateKey = %s\n", server, wireGuardNetwork.Bits(), WireGuardPort, state.PrivateKey)
	var up, down []string
	for _, network := range m.homeNetworks() {
		up = append(up, fmt.Sprintf("iptables -A FORWARD -i %%i -d %s -j ACCEPT", network))
		down = append(down, fmt.Sprintf("iptables -D FORWARD -i %%i -d %s -j ACCEPT", network))
	}
	up = append(up, "iptables -A FORWARD -i %i -j DROP", "iptables -t nat -A POSTROUTING -s "+wireGuardNetwork.String()+" -o eth+ -j MASQUERADE")
	down = append(down, "iptables -D FORWARD -i %i -j DROP", "iptables -t nat -D POSTROUTING -s "+wireGuardNetwork.String()+" -o eth+ -j MASQUERADE")
	fmt.Fprintf(&conf, "PostUp = %s\nPostDown = %s\n", strings.Join(up, "; "), strings.Join(down, "; "))
	for _, device := range state.Devices {
		fmt.Fprintf(&conf, "\n[Peer]\n# %d\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s/32\n", device.ID, device.PublicKey, device.PresharedKey, device.Address)
	}
	confDir := filepath.Join(dir, wireGuardDir)
	if err := os.MkdirAll(confDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(confDir, wireGuardConf), []byte(conf.String()), 0o600)
}

// wireGuardInstance vérifie que le service existe et qu'il s'agit du VPN.
func (m *Manager) wireGuardInstance(name string) (string, error) {
	if !marketplace.ValidLabel(name) {
		return "", fmt.Errorf("%w : %s", ErrNotFound, name)
	}
	dir := m.serviceDir(name)
	data, err := os.ReadFile(filepath.Join(dir, stateName))
	if err != nil {
		return "", fmt.Errorf("%w : %s", ErrNotFound, name)
	}
	var instance Instance
	if err := json.Unmarshal(data, &instance); err != nil || instance.App != WireGuardApp {
		return "", fmt.Errorf("%w : %s n'est pas un VPN WireGuard", ErrInvalid, name)
	}
	return dir, nil
}

// WireGuard donne l'adresse du VPN, le réseau de la maison et les appareils.
func (m *Manager) WireGuard(name string) (WireGuardStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, err := m.wireGuardInstance(name)
	if err != nil {
		return WireGuardStatus{}, err
	}
	state, err := readWireGuard(dir)
	if err != nil {
		return WireGuardStatus{}, err
	}
	status := WireGuardStatus{Endpoint: m.Domain, Port: WireGuardPort, EndpointLocal: !m.usesLetsEncrypt(), HomeNetworks: []string{}, Devices: []Device{}}
	for _, network := range m.homeNetworks() {
		status.HomeNetworks = append(status.HomeNetworks, network.String())
	}
	for _, device := range state.Devices {
		status.Devices = append(status.Devices, device.Device)
	}
	return status, nil
}

func validDeviceName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 40 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// AddDevice ajoute un appareil, redémarre le VPN et rend sa configuration.
func (m *Manager) AddDevice(ctx context.Context, name, deviceName string) (NewDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, err := m.wireGuardInstance(name)
	if err != nil {
		return NewDevice{}, err
	}
	deviceName = strings.TrimSpace(deviceName)
	if !validDeviceName(deviceName) {
		return NewDevice{}, fmt.Errorf("%w : donnez à l'appareil un nom de 1 à 40 caractères, par exemple « Téléphone de Camille »", ErrInvalid)
	}
	homes := m.homeNetworks()
	if len(homes) == 0 {
		return NewDevice{}, fmt.Errorf("%w : le Pi ne trouve pas le réseau de la maison ; vérifiez qu'il est branché à la box ou relié à son Wi-Fi", ErrInvalid)
	}
	state, err := readWireGuard(dir)
	if err != nil {
		return NewDevice{}, err
	}
	used := map[string]bool{}
	for _, device := range state.Devices {
		if strings.EqualFold(device.Name, deviceName) {
			return NewDevice{}, fmt.Errorf("%w : un appareil s'appelle déjà « %s »", ErrExists, deviceName)
		}
		used[device.Address] = true
	}
	server := wireGuardNetwork.Addr().Next()
	var address netip.Addr
	for candidate := server.Next(); candidate.As4()[3] < 255; candidate = candidate.Next() {
		if !used[candidate.String()] {
			address = candidate
			break
		}
	}
	if !address.IsValid() {
		return NewDevice{}, fmt.Errorf("%w : le VPN a déjà 253 appareils ; supprimez-en un", ErrInvalid)
	}
	private, err := wireGuardKey()
	if err != nil {
		return NewDevice{}, err
	}
	public, err := publicKey(private)
	if err != nil {
		return NewDevice{}, err
	}
	preshared, err := randomKey()
	if err != nil {
		return NewDevice{}, err
	}
	serverPublic, err := publicKey(state.PrivateKey)
	if err != nil {
		return NewDevice{}, err
	}
	device := storedDevice{
		Device:       Device{ID: int(address.As4()[3]), Name: deviceName, Address: address.String(), PublicKey: public, CreatedAt: time.Now().UTC()},
		PresharedKey: preshared,
	}
	previous := state
	state.Devices = append(append([]storedDevice{}, state.Devices...), device)
	if err := m.applyWireGuard(ctx, name, dir, state, previous); err != nil {
		return NewDevice{}, err
	}
	allowed := []string{server.String() + "/32"}
	for _, network := range homes {
		allowed = append(allowed, network.String())
	}
	config := fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s/32\n\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nEndpoint = %s:%d\nAllowedIPs = %s\nPersistentKeepalive = 25\n",
		private, address, serverPublic, preshared, endpointHost(m.Domain), WireGuardPort, strings.Join(allowed, ", "))
	png, err := qrcode.Encode(config, qrcode.Medium, 512)
	if err != nil {
		return NewDevice{}, err
	}
	return NewDevice{Device: device.Device, Config: config, QRCode: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)}, nil
}

// endpointHost met une adresse IPv6 entre crochets.
func endpointHost(domain string) string {
	if ip, err := netip.ParseAddr(domain); err == nil && ip.Is6() {
		return "[" + domain + "]"
	}
	return domain
}

// RemoveDevice retire un appareil : il ne peut plus se connecter.
func (m *Manager) RemoveDevice(ctx context.Context, name string, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, err := m.wireGuardInstance(name)
	if err != nil {
		return err
	}
	state, err := readWireGuard(dir)
	if err != nil {
		return err
	}
	previous := state
	state.Devices = nil
	for _, device := range previous.Devices {
		if device.ID != id {
			state.Devices = append(state.Devices, device)
		}
	}
	if len(state.Devices) == len(previous.Devices) {
		return fmt.Errorf("%w : appareil %d", ErrNotFound, id)
	}
	return m.applyWireGuard(ctx, name, dir, state, previous)
}

// applyWireGuard enregistre les appareils et redémarre le VPN ; en cas
// d'échec, la liste d'avant est remise.
func (m *Manager) applyWireGuard(ctx context.Context, name, dir string, state, previous wireGuardState) error {
	save := func(s wireGuardState) error {
		if err := writeJSON(filepath.Join(dir, wireGuardName), s, 0o600); err != nil {
			return err
		}
		return m.writeWireGuardConf(dir, s)
	}
	if err := save(state); err != nil {
		return err
	}
	if output, err := m.compose(ctx, name, "restart"); err != nil {
		restoreErr := save(previous)
		if restoreErr == nil {
			_, _ = m.compose(context.WithoutCancel(ctx), name, "restart")
		}
		return fmt.Errorf("le VPN n'a pas pu redémarrer : %s", trimOutput(output, err))
	}
	return nil
}

// wireGuardKey tire une clé privée Curve25519, comme « wg genkey ».
func wireGuardKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	key[0] &= 248
	key[31] = (key[31] & 127) | 64
	return base64.StdEncoding.EncodeToString(key), nil
}

func randomKey() (string, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}

// publicKey calcule la clé publique, comme « wg pubkey ».
func publicKey(private string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(private)
	if err != nil || len(raw) != 32 {
		return "", errors.New("clé WireGuard illisible")
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}
