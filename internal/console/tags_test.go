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
