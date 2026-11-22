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
