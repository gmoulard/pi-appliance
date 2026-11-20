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
