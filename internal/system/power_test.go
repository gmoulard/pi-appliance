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

package system

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseThrottled(t *testing.T) {
	cases := map[string]Power{
		"0\n":                 {},
		"50000\n":             {UndervoltageSinceBoot: true, ThrottledSinceBoot: true},
		"50005":               {UndervoltageNow: true, UndervoltageSinceBoot: true, ThrottledNow: true, ThrottledSinceBoot: true},
		"throttled=0x10000\n": {UndervoltageSinceBoot: true},
		"1":                   {UndervoltageNow: true, UndervoltageSinceBoot: true},
	}
	for text, want := range cases {
		power := parseThrottled(strings.NewReader(text))
		if power == nil || *power != want {
			t.Fatalf("%q : %+v", text, power)
		}
	}
	for _, text := range []string{"", "bientôt", "0x"} {
		if power := parseThrottled(strings.NewReader(text)); power != nil {
			t.Fatalf("%q : %+v", text, *power)
		}
	}
}

func TestReadPower(t *testing.T) {
	saved := throttledGlob
	defer func() { throttledGlob = saved }()
	dir := t.TempDir()
	throttledGlob = filepath.Join(dir, "soc*", "*firmware", "get_throttled")
	if power := readPower(); power != nil {
		t.Fatalf("sans fichier : %+v", power)
	}
	firmware := filepath.Join(dir, "soc", "soc:firmware")
	if err := os.MkdirAll(firmware, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(firmware, "get_throttled"), []byte("10000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if power := readPower(); power == nil || !power.UndervoltageSinceBoot || power.UndervoltageNow {
		t.Fatalf("%+v", power)
	}
}
