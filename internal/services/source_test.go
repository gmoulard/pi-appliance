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
	"os"
	"path/filepath"
	"testing"
)

func TestComposeSource(t *testing.T) {
	m := manager(t, &fakeRunner{})
	request := CustomRequest{Name: "libre", Compose: []byte(simple), Service: "web", Port: 8080, ConfirmRisks: true, SourceURL: "https://raw.githubusercontent.com/exemple/depot/main/compose.yaml", SourceEdited: true}
	if _, err := m.InstallCustom(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	source, err := m.ComposeSource("libre")
	if err != nil {
		t.Fatal(err)
	}
	if source.Compose != simple || source.URL != request.SourceURL || !source.Edited || source.Service != "web" || source.Port != 8080 {
		t.Fatalf("%+v", source)
	}
	if err := os.Remove(filepath.Join(m.Dir, "services", "libre", sourceName)); err != nil {
		t.Fatal(err)
	}
	source, err = m.ComposeSource("libre")
	if err != nil || source.Compose != simple || source.URL != "" || source.Service != "web" || source.Port != 8080 {
		t.Fatalf("service installé avant source.json : %+v %v", source, err)
	}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ComposeSource("cloud"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("service du catalogue : %v", err)
	}
	if _, err := m.ComposeSource("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service absent : %v", err)
	}
}
