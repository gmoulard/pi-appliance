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

package console

import (
	"regexp"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// Chaque étiquette de la marketplace a son libellé traduit dans app.js (TAG_LABELS).
func TestTagLabels(t *testing.T) {
	app := readWeb(t, "app.js")
	for _, tag := range marketplace.Tags {
		pattern := regexp.MustCompile(`(?m)^  "?` + regexp.QuoteMeta(tag.ID) + `"?: t\("` + regexp.QuoteMeta(tag.Label) + `"\),$`)
		if !pattern.MatchString(app) {
			t.Errorf("étiquette %s : « %s » absente de TAG_LABELS (app.js)", tag.ID, tag.Label)
		}
	}
}
