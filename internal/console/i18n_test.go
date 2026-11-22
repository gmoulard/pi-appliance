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
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Traductions de la console (spécification 5.3) : chaque langue traduit toutes
// les phrases de l'interface, sans phrase inconnue, en gardant les {0}, {1}…

var (
	callPattern        = regexp.MustCompile(`\bt\("((?:[^"\\]|\\.)*)"`)
	commentPattern     = regexp.MustCompile(`(?s)<!--.*?-->`)
	textPattern        = regexp.MustCompile(`>([^<]+)<`)
	attributePattern   = regexp.MustCompile(`\b(?:placeholder|aria-label|title|alt)="([^"]+)"`)
	letterPattern      = regexp.MustCompile(`\pL`)
	placeholderPattern = regexp.MustCompile(`\{\d+\}`)
	languagePattern    = regexp.MustCompile(`\{ code: "([a-z]{2})"`)
	tablePattern       = regexp.MustCompile(`(?s)piTranslations\("([a-z]{2})", (\{.*\})\);\s*$`)
)

func readWeb(t *testing.T, name string) string {
	t.Helper()
	data, err := fs.ReadFile(webFiles, "web/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// sourceKeys : phrases françaises de app.js (t("…")) et textes fixes de index.html.
func sourceKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	add := func(key string) {
		if !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	for _, match := range callPattern.FindAllStringSubmatch(readWeb(t, "app.js"), -1) {
		key, err := strconv.Unquote(`"` + match[1] + `"`)
		if err != nil {
			t.Fatalf("phrase illisible %q : %v", match[1], err)
		}
		add(key)
	}
	html := commentPattern.ReplaceAllString(readWeb(t, "index.html"), "")
	body := html[strings.Index(html, "<body"):]
	for _, match := range textPattern.FindAllStringSubmatch(body, -1) {
		if text := strings.TrimSpace(match[1]); letterPattern.MatchString(text) && !slices.Contains([]string{"pi-appliance", "pi", "-appliance"}, text) {
			add(text)
		}
	}
	for _, match := range attributePattern.FindAllStringSubmatch(body, -1) {
		add(strings.TrimSpace(match[1]))
	}
	// Pages servies par Go (pages.go) : mêmes fichiers de traduction.
	for _, key := range pageKeys {
		add(key)
	}
	return keys
}

var pageCallPattern = regexp.MustCompile(`\bpageT\(\w+, "((?:[^"\\]|\\.)*)"`)

// TestPageKeys : chaque phrase écrite dans un appel pageT(…, "…") est dans pageKeys.
func TestPageKeys(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range pageCallPattern.FindAllStringSubmatch(string(data), -1) {
			key, err := strconv.Unquote(`"` + match[1] + `"`)
			if err != nil {
				t.Fatal(err)
			}
			found++
			if !slices.Contains(pageKeys, key) {
				t.Errorf("%s : %q absent de pageKeys", file, key)
			}
		}
	}
	if found < 10 {
		t.Fatalf("%d appels pageT seulement : extraction cassée ?", found)
	}
}

func placeholders(text string) []string {
	found := placeholderPattern.FindAllString(text, -1)
	slices.Sort(found)
	return found
}

func TestTranslations(t *testing.T) {
	keys := sourceKeys(t)
	if len(keys) < 150 {
		t.Fatalf("%d phrases seulement : extraction cassée ?", len(keys))
	}
	languages := languagePattern.FindAllStringSubmatch(readWeb(t, "i18n.js"), -1)
	if len(languages) != 12 {
		t.Fatalf("%d langues dans i18n.js, 12 attendues (spécification 5.3)", len(languages))
	}
	for _, language := range languages {
		code := language[1]
		if code == "fr" {
			continue
		}
		t.Run(code, func(t *testing.T) {
			match := tablePattern.FindStringSubmatch(readWeb(t, "i18n/"+code+".js"))
			if match == nil || match[1] != code {
				t.Fatalf("i18n/%s.js : piTranslations(%q, {…}); attendu", code, code)
			}
			var table map[string]string
			if err := json.Unmarshal([]byte(match[2]), &table); err != nil {
				t.Fatal(err)
			}
			for _, key := range keys {
				translation, ok := table[key]
				switch {
				case !ok:
					t.Errorf("pas de traduction pour %q", key)
				case strings.TrimSpace(translation) == "":
					t.Errorf("traduction vide pour %q", key)
				case !slices.Equal(placeholders(key), placeholders(translation)):
					t.Errorf("%q : %v au lieu de %v dans %q", key, placeholders(translation), placeholders(key), translation)
				case strings.Count(key, "\n") != strings.Count(translation, "\n"):
					t.Errorf("%q : retours à la ligne différents dans %q", key, translation)
				}
			}
			for key := range table {
				if !slices.Contains(keys, key) {
					t.Errorf("phrase inconnue de la console : %q", key)
				}
			}
		})
	}
}

func TestTranslationFilesServed(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/pi-appliance/i18n.js", "/pi-appliance/i18n/ar.js"} {
		if response := f.do(http.MethodGet, path, "", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
			t.Fatalf("%s : code %d, %s", path, response.Code, response.Header().Get("Content-Type"))
		}
	}
}
