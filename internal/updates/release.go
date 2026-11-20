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

package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Release : une version publiée de pi-appliance.
type Release struct {
	Tag string `json:"tag_name"`
	URL string `json:"html_url"`
}

// Releases donne la dernière version publiée ; nil sans erreur quand aucune
// version n'est encore publiée.
type Releases interface {
	Latest(ctx context.Context) (*Release, error)
}

// GitHub lit la dernière version publiée d'un dépôt GitHub (ni brouillon ni
// préversion), sans jeton : 60 demandes par heure suffisent largement à une
// recherche par jour.
type GitHub struct {
	// Repo : « propriétaire/dépôt ».
	Repo string
	// BaseURL : https://api.github.com si vide (remplacé dans les tests).
	BaseURL string
	Client  *http.Client
}

// DefaultRepo : dépôt où sont publiées les versions de pi-appliance.
const DefaultRepo = "gmoulard/pi-appliance"

func (g GitHub) Latest(ctx context.Context) (*Release, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/repos/"+g.Repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "pi-appliance")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub : réponse %d", response.StatusCode)
	}
	var release Release
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return nil, fmt.Errorf("GitHub : réponse illisible : %w", err)
	}
	if release.Tag == "" {
		return nil, fmt.Errorf("GitHub : version sans étiquette")
	}
	return &release, nil
}

// Newer dit si latest est plus récente que current. Les versions de la forme
// 1.2.3 (avec ou sans « v », avec ou sans suffixe « -beta.1 ») se comparent ;
// une console construite d'un commit (empreinte git) est signalée dès qu'elle
// diffère de la dernière version publiée ; « dev » (poste de développement)
// ne l'est jamais.
func Newer(current, latest string) bool {
	current, latest = strings.TrimPrefix(current, "v"), strings.TrimPrefix(latest, "v")
	if latest == "" || current == "" || current == "dev" || current == latest {
		return false
	}
	a, aOK := parseVersion(current)
	b, bOK := parseVersion(latest)
	if !aOK || !bOK {
		return true
	}
	for i := range a.numbers {
		if a.numbers[i] != b.numbers[i] {
			return b.numbers[i] > a.numbers[i]
		}
	}
	switch {
	case a.pre == b.pre:
		return false
	case b.pre == "":
		return true // 1.2.3 après 1.2.3-beta.1
	case a.pre == "":
		return false
	default:
		return b.pre > a.pre
	}
}

type version struct {
	numbers [3]int
	pre     string
}

func parseVersion(text string) (version, bool) {
	var v version
	text, v.pre, _ = strings.Cut(text, "-")
	parts := strings.Split(text, ".")
	// Au moins « 1.2 » : une empreinte git faite de chiffres n'est pas une version.
	if len(parts) < 2 || len(parts) > 3 {
		return v, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return v, false
		}
		v.numbers[i] = n
	}
	return v, true
}
