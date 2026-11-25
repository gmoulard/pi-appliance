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

package auth

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Appareils appairés (spécification 8.1, application mobile) : un téléphone
// garde une clé ECDSA P-256 secrète, non exportable ; le Pi ne garde que sa
// clé publique. L'appairage se fait avec un code à usage unique montré dans le
// code QR de « Mon Pi » à l'administrateur connecté ; ensuite le téléphone
// ouvre une session en signant un message daté, sans mot de passe. La session
// vaut une connexion complète, double authentification comprise : le code a
// été montré dans une session qui l'avait déjà passée. Un appareil reste
// appairé jusqu'à ce qu'il soit retiré, que son compte soit supprimé ou que le
// Pi soit remis à zéro.

const (
	// PairingCodeLifetime : durée d'un code d'appairage.
	PairingCodeLifetime = 10 * time.Minute
	// pairingCodeReuse : un code qui a encore au moins cette durée est remontré
	// tel quel (la carte se met à jour chaque minute) ; sinon il est remplacé.
	pairingCodeReuse = 5 * time.Minute
	// DeviceClockSkew : écart permis entre l'heure du téléphone et celle du Pi.
	DeviceClockSkew = 5 * time.Minute
	DeviceNameMax   = 64
	maxDevices      = 200
)

var (
	ErrPairingCode     = errors.New("code d'appairage inconnu, expiré ou déjà utilisé")
	ErrDeviceUnknown   = errors.New("appareil inconnu ou retiré")
	ErrDeviceClock     = errors.New("heure de l'appareil trop éloignée de celle du Pi")
	ErrDeviceSignature = errors.New("signature ou message de l'appareil refusé")
	ErrTooManyDevices  = errors.New("trop d'appareils appairés")
)

// DeviceKey : clé publique P-256 d'un appareil, coordonnées en base64url sans « = ».
type DeviceKey struct {
	X string `json:"x"`
	Y string `json:"y"`
}

type Device struct {
	// ID : empreinte RFC 7638 de la clé (SHA-256, base64url sans « = »).
	ID       string    `json:"id"`
	Key      DeviceKey `json:"key"`
	Name     string    `json:"name"`
	Owner    string    `json:"owner"`
	PairedAt time.Time `json:"paired_at"`
	LastUsed time.Time `json:"last_used"`
}

// DevicePublic : un appareil vu par l'interface.
type DevicePublic struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	PairedAt time.Time `json:"paired_at"`
	LastUsed time.Time `json:"last_used"`
}

func (d Device) Public() DevicePublic {
	return DevicePublic{ID: d.ID, Name: d.Name, PairedAt: d.PairedAt, LastUsed: d.LastUsed}
}

type pairingCode struct {
	owner   string
	expires time.Time
}

func (s *Store) devicesPath() string {
	return filepath.Join(filepath.Dir(s.path), "devices.json")
}

func (s *Store) loadDevices() error {
	data, err := os.ReadFile(s.devicesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var devices []Device
	if err := json.Unmarshal(data, &devices); err != nil {
		return fmt.Errorf("%s : %w", s.devicesPath(), err)
	}
	for _, device := range devices {
		s.devices[device.ID] = device
	}
	return nil
}

func (s *Store) saveDevices() error {
	devices := make([]Device, 0, len(s.devices))
	for _, device := range s.devices {
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })
	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.devicesPath() + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.devicesPath())
}

// PairingCode : code d'appairage du compte owner (16 octets en base64url),
// valable PairingCodeLifetime et une seule fois. Un compte n'a qu'un code à la
// fois : un nouveau remplace le précédent.
func (s *Store) PairingCode(owner string) (string, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for code, current := range s.pairings {
		if now.After(current.expires) {
			delete(s.pairings, code)
			continue
		}
		if current.owner == owner {
			if current.expires.Sub(now) >= pairingCodeReuse {
				return code, current.expires
			}
			delete(s.pairings, code)
		}
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	expires := now.Add(PairingCodeLifetime)
	s.pairings[code] = pairingCode{owner: owner, expires: expires}
	return code, expires
}

// Pair enregistre la clé publique jwk (JSON) d'un appareil pour le compte qui
// a montré le code, si message vaut « appairer|<piID>|<code> » et que
// signature (r‖s, base64url) le signe avec cette clé ; puis ouvre une session.
// Le code ne sert qu'une fois, même refusé.
func (s *Store) Pair(code, jwk, name, message, signature, piID, client string) (string, Account, error) {
	keys := []string{"client:" + client}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for _, key := range keys {
		if wait := s.blocked(key, now); wait > 0 {
			return "", Account{}, TooManyError{Wait: wait}
		}
	}
	current, ok := s.pairings[code]
	delete(s.pairings, code)
	if !ok || code == "" || now.After(current.expires) {
		s.fail(keys, now)
		return "", Account{}, ErrPairingCode
	}
	key, id, err := parseDeviceKey(jwk)
	if err != nil || piID == "" || message != "appairer|"+piID+"|"+code || !verifyDevice(key, message, signature) {
		s.fail(keys, now)
		return "", Account{}, ErrDeviceSignature
	}
	account, ok := s.accounts[current.owner]
	if !ok || !account.active(now) {
		return "", Account{}, ErrNotFound
	}
	if _, exists := s.devices[id]; !exists && len(s.devices) >= maxDevices {
		return "", Account{}, ErrTooManyDevices
	}
	previous, existed := s.devices[id]
	s.devices[id] = Device{ID: id, Key: key, Name: deviceName(name), Owner: account.Name, PairedAt: now.UTC(), LastUsed: now.UTC()}
	if err := s.saveDevices(); err != nil {
		if existed {
			s.devices[id] = previous
		} else {
			delete(s.devices, id)
		}
		return "", Account{}, err
	}
	for _, key := range keys {
		delete(s.failures, key)
	}
	return s.openSession(account.Name, now), account, nil
}

// DeviceLogin ouvre une session pour l'appareil id si message vaut
// « connexion|<piID>|<secondes Unix>|<numéro> », daté à DeviceClockSkew
// près, avec un numéro (16 octets en base64url) jamais vu, et que signature le
// signe avec la clé enregistrée.
func (s *Store) DeviceLogin(id, message, signature, piID, client string) (string, Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	device, account, err := s.checkSigned("connexion", id, message, signature, piID, client, now, true)
	if err != nil {
		return "", Account{}, err
	}
	device.LastUsed = now.UTC()
	s.devices[id] = device
	if err := s.saveDevices(); err != nil {
		return "", Account{}, err
	}
	return s.openSession(account.Name, now), account, nil
}

// DeviceRevoke retire l'appareil id à la demande du téléphone lui-même, si
// message vaut « retirer|<piID>|<secondes Unix>|<numéro> », avec les mêmes
// vérifications que DeviceLogin ; aucune session n'est ouverte. Un appareil
// inconnu ou déjà retiré n'est pas une erreur : le résultat voulu est atteint.
func (s *Store) DeviceRevoke(id, message, signature, piID, client string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if _, known := s.devices[id]; !known {
		return nil
	}
	// Un invité expiré peut encore retirer son téléphone.
	device, _, err := s.checkSigned("retirer", id, message, signature, piID, client, now, false)
	if err != nil {
		return err
	}
	delete(s.devices, id)
	if err := s.saveDevices(); err != nil {
		s.devices[id] = device
		return err
	}
	return nil
}

// checkSigned vérifie un message « <action>|<piID>|<secondes Unix>|<numéro> »
// signé par l'appareil id : appareil connu, compte actif, signature, Pi,
// heure à DeviceClockSkew près, numéro jamais vu (verrou tenu). active : le
// compte doit encore pouvoir se connecter.
func (s *Store) checkSigned(action, id, message, signature, piID, client string, now time.Time, active bool) (Device, Account, error) {
	keys := []string{"client:" + client, "device:" + id}
	for _, key := range keys {
		if wait := s.blocked(key, now); wait > 0 {
			return Device{}, Account{}, TooManyError{Wait: wait}
		}
	}
	device, ok := s.devices[id]
	if !ok {
		s.fail(keys[:1], now)
		return Device{}, Account{}, ErrDeviceUnknown
	}
	account, ok := s.accounts[device.Owner]
	if active && (!ok || !account.active(now)) {
		return Device{}, Account{}, ErrDeviceUnknown
	}
	if !verifyDevice(device.Key, message, signature) {
		s.fail(keys, now)
		return Device{}, Account{}, ErrDeviceSignature
	}
	parts := strings.Split(message, "|")
	if len(parts) != 4 || parts[0] != action || piID == "" || parts[1] != piID {
		s.fail(keys, now)
		return Device{}, Account{}, ErrDeviceSignature
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		s.fail(keys, now)
		return Device{}, Account{}, ErrDeviceSignature
	}
	if skew := now.Sub(time.Unix(seconds, 0)); skew > DeviceClockSkew || skew < -DeviceClockSkew {
		return Device{}, Account{}, ErrDeviceClock
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(nonce) != 16 {
		s.fail(keys, now)
		return Device{}, Account{}, ErrDeviceSignature
	}
	for seen, expires := range s.nonces {
		if now.After(expires) {
			delete(s.nonces, seen)
		}
	}
	if _, seen := s.nonces[parts[3]]; seen {
		s.fail(keys, now)
		return Device{}, Account{}, ErrDeviceSignature
	}
	s.nonces[parts[3]] = now.Add(2 * DeviceClockSkew)
	for _, key := range keys {
		delete(s.failures, key)
	}
	return device, account, nil
}

func (s *Store) openSession(name string, now time.Time) string {
	token := randomToken()
	s.sessions[token] = session{name: name, expires: now.Add(SessionLifetime)}
	s.cleanup(now)
	return token
}

// Devices : appareils appairés du compte, du plus récent au plus ancien.
func (s *Store) Devices(owner string) []DevicePublic {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []DevicePublic{}
	for _, device := range s.devices {
		if device.Owner == owner {
			list = append(list, device.Public())
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].PairedAt.After(list[j].PairedAt) })
	return list
}

// RenameDevice change le nom d'un appareil du compte.
func (s *Store) RenameDevice(owner, id, name string) (DevicePublic, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.devices[id]
	if !ok || device.Owner != owner {
		return DevicePublic{}, ErrDeviceUnknown
	}
	if strings.TrimSpace(name) == "" {
		return DevicePublic{}, ErrInvalid
	}
	previous := device
	device.Name = deviceName(name)
	s.devices[id] = device
	if err := s.saveDevices(); err != nil {
		s.devices[id] = previous
		return DevicePublic{}, err
	}
	return device.Public(), nil
}

// RemoveDevice retire un appareil du compte : sa clé ne vaut plus rien.
func (s *Store) RemoveDevice(owner, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	device, ok := s.devices[id]
	if !ok || device.Owner != owner {
		return ErrDeviceUnknown
	}
	delete(s.devices, id)
	if err := s.saveDevices(); err != nil {
		s.devices[id] = device
		return err
	}
	return nil
}

// removeDevicesOf retire les appareils d'un compte supprimé (verrou tenu).
func (s *Store) removeDevicesOf(owner string) error {
	changed := false
	for id, device := range s.devices {
		if device.Owner == owner {
			delete(s.devices, id)
			changed = true
		}
	}
	for code, current := range s.pairings {
		if current.owner == owner {
			delete(s.pairings, code)
		}
	}
	if !changed {
		return nil
	}
	return s.saveDevices()
}

// deviceName : nom lisible, sans caractère de contrôle, DeviceNameMax caractères au plus.
func deviceName(name string) string {
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	if !utf8.ValidString(name) {
		name = strings.ToValidUTF8(name, "")
	}
	if runes := []rune(name); len(runes) > DeviceNameMax {
		name = string(runes[:DeviceNameMax])
	}
	if name == "" {
		return "Téléphone"
	}
	return name
}

// parseDeviceKey lit une clé publique JWK P-256 et renvoie son empreinte RFC 7638.
func parseDeviceKey(raw string) (DeviceKey, string, error) {
	var jwk struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if len(raw) > 1024 || json.Unmarshal([]byte(raw), &jwk) != nil || jwk.Kty != "EC" || jwk.Crv != "P-256" {
		return DeviceKey{}, "", ErrDeviceSignature
	}
	key := DeviceKey{X: jwk.X, Y: jwk.Y}
	if _, err := key.public(); err != nil {
		return DeviceKey{}, "", err
	}
	return key, key.Thumbprint(), nil
}

// Thumbprint : empreinte RFC 7638 de la clé, SHA-256 du JSON aux membres
// obligatoires dans l'ordre alphabétique, sans espace, en base64url sans « = ».
func (k DeviceKey) Thumbprint() string {
	sum := sha256.Sum256([]byte(`{"crv":"P-256","kty":"EC","x":"` + k.X + `","y":"` + k.Y + `"}`))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// public : la clé, refusée si une coordonnée n'est pas 32 octets en base64url
// canonique ou si le point n'est pas sur la courbe.
func (k DeviceKey) public() (*ecdsa.PublicKey, error) {
	x, errX := base64.RawURLEncoding.Strict().DecodeString(k.X)
	y, errY := base64.RawURLEncoding.Strict().DecodeString(k.Y)
	if errX != nil || errY != nil || len(x) != 32 || len(y) != 32 {
		return nil, ErrDeviceSignature
	}
	point := append(append([]byte{4}, x...), y...)
	if _, err := ecdh.P256().NewPublicKey(point); err != nil {
		return nil, ErrDeviceSignature
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
}

// verifyDevice : signature ECDSA P-256 / SHA-256 de message au format de
// WebCrypto, r‖s sur 64 octets en base64url sans « = ».
func verifyDevice(key DeviceKey, message, signature string) bool {
	public, err := key.public()
	if err != nil {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(signature, "="))
	if err != nil || len(raw) != 64 {
		return false
	}
	digest := sha256.Sum256([]byte(message))
	return ecdsa.Verify(public, digest[:], new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:]))
}
