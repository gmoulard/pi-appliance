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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const (
	localCertificateDir    = "local-certificate"
	localCertificateFile   = "local.crt"
	localCertificateMarker = "local-certificate.imported"
)

func (m *Manager) wantsLocalCertificate(app marketplace.App) bool {
	return app.LocalCertificate != nil && m.LocalCertificate != ""
}

func localCertificateTarget(app marketplace.App) string {
	if app.LocalCertificate == nil {
		return ""
	}
	return path.Join(app.LocalCertificate.Path, localCertificateFile)
}

// NE PAS monter le dossier du certificat du Pi : il contient aussi sa clé privée ; seule une copie du certificat part dans le service.
func (m *Manager) copyLocalCertificate(name string) (bool, error) {
	data, err := os.ReadFile(m.LocalCertificate)
	if err != nil {
		return false, fmt.Errorf("certificat local du Pi illisible : %w", err)
	}
	if !bytes.Contains(data, []byte("-----BEGIN CERTIFICATE-----")) || bytes.Contains(data, []byte("PRIVATE KEY")) {
		return false, fmt.Errorf("certificat local du Pi invalide : %s", m.LocalCertificate)
	}
	dir := filepath.Join(m.serviceDir(name), localCertificateDir)
	target := filepath.Join(dir, localCertificateFile)
	if current, err := os.ReadFile(target); err == nil && bytes.Equal(current, data) {
		return false, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		return false, err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, target)
}

func (m *Manager) localCertificateOverride(app marketplace.App, name string, services map[string]any) {
	if !m.wantsLocalCertificate(app) {
		return
	}
	target, ok := services[app.LocalCertificate.Service].(map[string]any)
	if !ok {
		target = map[string]any{}
		services[app.LocalCertificate.Service] = target
	}
	volumes, _ := target["volumes"].([]string)
	target["volumes"] = append(volumes, filepath.Join(m.serviceDir(name), localCertificateDir)+":"+app.LocalCertificate.Path+":ro")
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(m.CurrentDomain())), ".")
	if host != "" && net.ParseIP(host) == nil {
		hosts, _ := target["extra_hosts"].([]string)
		target["extra_hosts"] = append(hosts, host+":host-gateway")
	}
}

// trustLocalCertificate : Nextcloud a son propre magasin de certificats (occ
// security:certificates:import), relu à chaque changement du certificat du Pi.
func (m *Manager) trustLocalCertificate(ctx context.Context, instance Instance) error {
	if instance.LocalCertificate == "" || m.LocalCertificate == "" {
		return nil
	}
	if _, err := m.copyLocalCertificate(instance.Name); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(m.serviceDir(instance.Name), localCertificateDir, localCertificateFile))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	marker := filepath.Join(m.serviceDir(instance.Name), localCertificateMarker)
	if done, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(done)) == digest {
		return nil
	}
	if _, err := m.occ(ctx, instance.Name, "security:certificates:import", instance.LocalCertificate); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(digest+"\n"), 0o600)
}
