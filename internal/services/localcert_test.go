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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCertificate = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

func TestLocalCertificate(t *testing.T) {
	runner := &occRunner{installed: true}
	dir := t.TempDir()
	certificate := filepath.Join(dir, "tls", "local.crt")
	os.MkdirAll(filepath.Dir(certificate), 0o700)
	os.WriteFile(certificate, []byte(testCertificate), 0o644)
	os.WriteFile(filepath.Join(dir, "tls", "local.key"), []byte("-----BEGIN PRIVATE KEY-----\n"), 0o600)
	m := &Manager{Dir: filepath.Join(dir, "data"), Domain: "salon.local", Runner: runner, LocalCertificate: certificate}
	ctx := context.Background()
	if _, err := m.Install(ctx, nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	override := readOverride(t, m, "cloud")
	copied := filepath.Join(m.serviceDir("cloud"), localCertificateDir)
	if !strings.Contains(override, `"`+copied+`:/usr/local/share/pi-appliance:ro"`) || !strings.Contains(override, `"salon.local:host-gateway"`) {
		t.Fatalf("override : %s", override)
	}
	if strings.Contains(override, filepath.Dir(certificate)) {
		t.Fatalf("dossier de la clé du Pi monté : %s", override)
	}
	entries, _ := os.ReadDir(copied)
	if len(entries) != 1 || entries[0].Name() != localCertificateFile {
		t.Fatalf("copie : %v", entries)
	}
	if info, _ := os.Stat(filepath.Join(copied, localCertificateFile)); info.Mode().Perm() != 0o644 {
		t.Fatalf("droits de la copie : %v", info.Mode())
	}
	instance := instanceNamed(t, m, "cloud")
	if instance.LocalCertificate != "/usr/local/share/pi-appliance/local.crt" {
		t.Fatalf("instance : %+v", instance)
	}
	runner.occCalls()
	if err := m.trustLocalCertificate(ctx, instance); err != nil {
		t.Fatal(err)
	}
	if calls := runner.occCalls(); len(calls) != 1 || calls[0] != "security:certificates:import /usr/local/share/pi-appliance/local.crt" {
		t.Fatalf("import : %v", calls)
	}
	if err := m.trustLocalCertificate(ctx, instance); err != nil || len(runner.occCalls()) != 0 {
		t.Fatalf("import refait sans changement : %v", err)
	}
	// Certificat renouvelé (nom du Pi changé) : nouvelle copie, nouvel import.
	os.WriteFile(certificate, []byte(testCertificate+"\n"), 0o644)
	if err := m.trustLocalCertificate(ctx, instance); err != nil || len(runner.occCalls()) != 1 {
		t.Fatalf("certificat renouvelé : %v", err)
	}
	// Une clé privée à la place du certificat n'est jamais copiée.
	os.WriteFile(certificate, []byte(testCertificate+"-----BEGIN PRIVATE KEY-----\n"), 0o644)
	if err := m.trustLocalCertificate(ctx, instance); err == nil {
		t.Fatal("clé privée copiée")
	}
	if data, _ := os.ReadFile(filepath.Join(copied, localCertificateFile)); strings.Contains(string(data), "PRIVATE") {
		t.Fatal("clé privée dans le service")
	}
}

func TestLocalCertificateOff(t *testing.T) {
	m := &Manager{Dir: t.TempDir(), Domain: "salon.local", Runner: &occRunner{installed: true}}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if override := readOverride(t, m, "cloud"); strings.Contains(override, "host-gateway") || strings.Contains(override, ":ro") {
		t.Fatalf("sans certificat local : %s", override)
	}
	if m.trustLocalCertificate(context.Background(), instanceNamed(t, m, "cloud")) != nil {
		t.Fatal("rien à faire")
	}
}
