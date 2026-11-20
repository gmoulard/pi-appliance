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

package addresses

import (
	"errors"
	"testing"
)

func TestCheck(t *testing.T) {
	for _, name := range []string{"maison", "famille-dupont", "pi42", "abc", "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5"} {
		if err := Check(name); err != nil {
			t.Errorf("Check(%q) = %v, attendu accepté", name, err)
		}
	}
	for _, name := range []string{
		"", "ab", "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p", "Maison", "élodie", "ma maison", "ma_maison", "ma.maison",
		"-maison", "maison-", "xn--maison", "www", "relais", "mail", "mac",
		"google-home", "mon-raspberry", "f-u-c-k", "paypal2",
	} {
		if err := Check(name); !errors.Is(err, ErrInvalid) {
			t.Errorf("Check(%q) = %v, attendu ErrInvalid", name, err)
		}
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		" Maison ":                 "maison",
		"maison.pi-appliance.com":  "maison",
		"MAISON.PI-APPLIANCE.COM.": "maison",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, attendu %q", in, got, want)
		}
	}
	if got := Hostname("maison"); got != "maison.pi-appliance.com" {
		t.Errorf("Hostname = %q", got)
	}
}
