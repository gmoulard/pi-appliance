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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Docker Compose libre lu à une adresse (spécification 6.4) : la console
// télécharge le fichier et le rend pour remplir le formulaire, que
// l'administrateur relit avant d'installer. Une page GitHub (blob) est lue
// dans sa version brute ; un dossier GitHub (tree) donne le premier fichier
// Compose qu'il contient.

// ComposeFetchRequest : adresse d'un fichier Docker Compose.
type ComposeFetchRequest struct {
	URL string `json:"url"`
}

// maxComposeSize : taille maximale d'un fichier Docker Compose téléchargé.
const maxComposeSize = 256 << 10

// composeFileNames : noms cherchés dans un dossier, dans l'ordre de Docker Compose.
var composeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// composeFetchClient : client des téléchargements (remplacé dans les tests).
var composeFetchClient = &http.Client{Timeout: 20 * time.Second}

var errComposeNotFound = errors.New("introuvable")

// composeCandidates : adresses à essayer, dans l'ordre, pour une adresse donnée.
func composeCandidates(raw string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("« %s » n'est pas une adresse web (https://…)", raw)
	}
	if u.Host == "github.com" || u.Host == "www.github.com" {
		// /<propriétaire>/<dépôt>/(blob|tree)/<branche>/<chemin>
		parts := strings.SplitN(strings.Trim(u.Path, "/"), "/", 5)
		if len(parts) >= 4 && (parts[2] == "blob" || parts[2] == "tree") {
			base := "https://raw.githubusercontent.com/" + parts[0] + "/" + parts[1] + "/" + parts[3]
			path := ""
			if len(parts) == 5 {
				path = "/" + strings.Trim(parts[4], "/")
			}
			if parts[2] == "blob" {
				return []string{base + path}, nil
			}
			candidates := make([]string, 0, len(composeFileNames))
			for _, name := range composeFileNames {
				candidates = append(candidates, base+path+"/"+name)
			}
			return candidates, nil
		}
	}
	return []string{u.String()}, nil
}

func fetchComposeFile(ctx context.Context, address string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	response, err := composeFetchClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("adresse injoignable : %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", errComposeNotFound
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("l'adresse répond « %s »", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxComposeSize+1))
	if err != nil {
		return "", fmt.Errorf("téléchargement interrompu : %v", err)
	}
	if len(body) > maxComposeSize {
		return "", fmt.Errorf("fichier trop gros (plus de %d Ko) pour un fichier Docker Compose", maxComposeSize>>10)
	}
	text := string(body)
	if strings.HasPrefix(strings.TrimSpace(strings.ToLower(text)), "<!doctype html") || strings.HasPrefix(strings.TrimSpace(strings.ToLower(text)), "<html") {
		return "", errors.New("cette adresse donne une page web, pas un fichier Docker Compose : donnez l'adresse du fichier lui-même")
	}
	return text, nil
}

func (c *console) fetchCompose(w http.ResponseWriter, r *http.Request) {
	var request ComposeFetchRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	candidates, err := composeCandidates(request.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error()+".")
		return
	}
	for _, candidate := range candidates {
		text, err := fetchComposeFile(r.Context(), candidate)
		if errors.Is(err, errComposeNotFound) {
			continue
		}
		if err != nil {
			writeError(w, http.StatusBadGateway, "Fichier non lu : "+err.Error()+".")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": candidate, "compose": text})
		return
	}
	writeError(w, http.StatusNotFound, "Aucun fichier Docker Compose à cette adresse.")
}
