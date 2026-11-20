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

package marketplace

import (
	"fmt"
	"strings"
)

// Tag : étiquette d'un service de la marketplace, pour filtrer le catalogue
// (spécification 6.5). La liste est fermée : la console traduit chaque libellé
// dans les 12 langues (TAG_LABELS de app.js).
type Tag struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// MaxTags : nombre maximal d'étiquettes par application.
const MaxTags = 4

var Tags = []Tag{
	{"site-web", "Site web"},
	{"fichiers", "Fichiers"},
	{"documents", "Documents"},
	{"collaboration", "Travail à plusieurs"},
	{"donnees", "Base de données"},
	{"administration", "Administration"},
	{"reseau", "Réseau"},
	{"maison", "Maison"},
}

func knownTag(id string) bool {
	for _, tag := range Tags {
		if tag.ID == id {
			return true
		}
	}
	return false
}

func checkTags(tags []string) []string {
	var problems []string
	if len(tags) == 0 || len(tags) > MaxTags {
		problems = append(problems, fmt.Sprintf("tags : de 1 à %d étiquettes", MaxTags))
	}
	seen := map[string]bool{}
	for _, tag := range tags {
		switch {
		case !knownTag(tag):
			problems = append(problems, fmt.Sprintf("étiquette %q inconnue", tag))
		case seen[tag]:
			problems = append(problems, fmt.Sprintf("étiquette %q en double", tag))
		}
		seen[tag] = true
	}
	return problems
}

// withoutTags retire la clé tags de x-pi-appliance du fichier Compose : les
// consoles 0.1.0 à 0.1.3 refusent cette clé, et les étiquettes voyagent dans
// le champ tags du catalogue publié (spécification 6.5).
func withoutTags(data []byte) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	var kept strings.Builder
	inMeta, skipping := false, false
	keyIndent := -1
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		blank := strings.TrimSpace(line) == ""
		if !blank && indent == 0 {
			inMeta = strings.HasPrefix(trimmed, "x-pi-appliance:")
			skipping = false
			keyIndent = -1
		} else if inMeta && !blank {
			if keyIndent < 0 {
				keyIndent = indent
			}
			if indent == keyIndent && !strings.HasPrefix(trimmed, "- ") {
				skipping = strings.HasPrefix(trimmed, "tags:")
			}
		}
		if inMeta && skipping && (blank || indent >= keyIndent) {
			continue
		}
		kept.WriteString(line)
	}
	return []byte(kept.String())
}
