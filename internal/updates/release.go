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

package updates

import (
	"cmp"
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
	Tag        string  `json:"tag_name"`
	URL        string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
}

// Canaux de version (spécification 5.4) : stable par défaut, bêta au choix
// de l'administrateur.
const (
	Stable = "stable"
	Beta   = "beta"
)

// Preview dit si la version est une préversion : marquée comme telle sur
// GitHub, ou numérotée avec un suffixe (« 1.3.0-beta.1 »).
func (r Release) Preview() bool {
	return r.Prerelease || strings.Contains(r.Tag, "-")
}

// Allowed dit si la version peut être proposée sur ce canal : jamais de
// brouillon, et jamais de préversion sur le canal stable.
func (r Release) Allowed(channel string) bool {
	if r.Draft || r.Tag == "" {
		return false
	}
	return channel == Beta || !r.Preview()
}

// Asset : un fichier joint à une version publiée (image, archive de mise à jour…).
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Asset rend le fichier joint de ce nom.
func (r Release) Asset(name string) (Asset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return Asset{}, false
}

// Releases donne la dernière version publiée sur un canal (Stable ou Beta) ;
// nil sans erreur quand aucune version n'est encore publiée sur ce canal.
type Releases interface {
	Latest(ctx context.Context, channel string) (*Release, error)
}

// ReleaseLister donne aussi les versions publiées récemment, la plus récente
// en premier (pia update list, installation d'une version choisie).
type ReleaseLister interface {
	List(ctx context.Context) ([]Release, error)
}

// GitHub lit la dernière version publiée d'un dépôt GitHub, sans jeton : 60
// demandes par heure suffisent largement à une recherche par jour. Canal
// stable : releases/latest (ni brouillon ni préversion). Canal bêta : la plus
// récente des 30 dernières versions, préversions comprises.
type GitHub struct {
	// Repo : « propriétaire/dépôt ».
	Repo string
	// BaseURL : https://api.github.com si vide (remplacé dans les tests).
	BaseURL string
	Client  *http.Client
}

// DefaultRepo : dépôt où sont publiées les versions de pi-appliance.
const DefaultRepo = "gmoulard/pi-appliance"

func (g GitHub) Latest(ctx context.Context, channel string) (*Release, error) {
	if channel == Beta {
		var releases []Release
		found, err := g.get(ctx, "/releases?per_page=30", &releases)
		if err != nil || !found {
			return nil, err
		}
		var best *Release
		for i := range releases {
			candidate := &releases[i]
			if !candidate.Allowed(Beta) {
				continue
			}
			if _, ok := parseVersion(strings.TrimPrefix(candidate.Tag, "v")); !ok {
				continue
			}
			if best == nil || Newer(best.Tag, candidate.Tag) {
				best = candidate
			}
		}
		return best, nil
	}
	var release Release
	found, err := g.get(ctx, "/releases/latest", &release)
	if err != nil || !found {
		return nil, err
	}
	if release.Tag == "" {
		return nil, fmt.Errorf("GitHub : version sans étiquette")
	}
	if !release.Allowed(Stable) {
		return nil, nil
	}
	return &release, nil
}

// List rend les 30 dernières versions publiées (brouillons compris : l'appelant
// les écarte avec Allowed), telles que GitHub les donne.
func (g GitHub) List(ctx context.Context) ([]Release, error) {
	var releases []Release
	if _, err := g.get(ctx, "/releases?per_page=30", &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

// get lit une réponse JSON de l'API GitHub du dépôt ; faux sans erreur pour
// une réponse 404 (aucune version publiée).
func (g GitHub) get(ctx context.Context, path string, value any) (bool, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+"/repos/"+g.Repo+path, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "pi-appliance")
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GitHub : réponse %d", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(value); err != nil {
		return false, fmt.Errorf("GitHub : réponse illisible : %w", err)
	}
	return true, nil
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
		return comparePre(a.pre, b.pre) < 0
	}
}

// comparePre compare deux suffixes de préversion à la manière de SemVer :
// partie par partie, les nombres comme des nombres (« beta.10 » après
// « beta.9 »), un nombre avant un mot.
func comparePre(a, b string) int {
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(left) && i < len(right); i++ {
		x, xErr := strconv.Atoi(left[i])
		y, yErr := strconv.Atoi(right[i])
		switch {
		case xErr == nil && yErr == nil:
			if x != y {
				return cmp.Compare(x, y)
			}
		case xErr == nil:
			return -1
		case yErr == nil:
			return 1
		default:
			if c := strings.Compare(left[i], right[i]); c != 0 {
				return c
			}
		}
	}
	return cmp.Compare(len(left), len(right))
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
