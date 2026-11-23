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

package services

// Services sur internet (spécification 7.1, 8.2 ; TODO-3.17) : un service
// s'ouvre depuis internet sous un chemin de l'adresse internet du Pi,
// https://<adresse internet>/<service>/ (choix de l'utilisateur du
// 2026-11-22, sans entrée DNS à créer). La console écrit le routeur Traefik
// (internal/console). Ici : les noms réservés à ces chemins et les
// applications qui marchent sous un chemin.

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ReservedNames : chemins déjà servis par Traefik sous l'adresse du Pi
// (console, Cockpit) : un service ne peut pas porter ce nom.
// NE PAS retirer « pi-appliance » : le service masquerait la console.
var ReservedNames = []string{"pi-appliance", "cockpit"}

// Pourquoi un service ne s'ouvre pas sous un chemin (InternetPath).
const (
	// PathReady : il s'y ouvre.
	PathReady = ""
	// PathUnsupported : l'application ne marche pas sous un chemin (liens
	// écrits pour la racine de son adresse : Nextcloud, Etherpad, DokuWiki…).
	PathUnsupported = "subpath"
	// PathUpdate : une version plus récente de l'application le permet.
	PathUpdate = "update"
	// PathReserved : son nom est un chemin réservé (ReservedNames).
	PathReserved = "reserved"
)

// pathApps : applications qui marchent sous un chemin. « relative » : liens
// relatifs, rien à faire (MkDocs, site web à liens relatifs) ; « build » :
// générateur qui construit une seconde copie du site pour ce chemin
// (SITE_PATH dans son manifeste, servie par nginx quand Traefik pose
// X-Forwarded-Prefix) ; un service installé avant doit être mis à jour.
// Un Docker Compose libre n'y est pas : rien ne dit qu'il y marcherait.
var pathApps = map[string]string{
	"site-web": "relative",
	"mkdocs":   "relative",
	"hugo":     "build",
	"jekyll":   "build",
	// Page et appels du serveur en adresses relatives (TODO-2.69).
	"wordpress-markdown": "relative",
}

// InternetPath dit si le service s'ouvre sous le chemin /<nom>/ de l'adresse
// internet du Pi, sinon pourquoi (PathUnsupported, PathUpdate, PathReserved).
func (m *Manager) InternetPath(instance Instance) string {
	if slices.Contains(ReservedNames, instance.Name) {
		return PathReserved
	}
	switch pathApps[instance.App] {
	case "relative":
		return PathReady
	case "build":
		compose, err := os.ReadFile(filepath.Join(m.serviceDir(instance.Name), composeName))
		if err != nil || !bytes.Contains(compose, []byte("SITE_PATH")) {
			return PathUpdate
		}
		return PathReady
	}
	return PathUnsupported
}

// RouterMiddlewares : middlewares Traefik du routeur d'un service (page
// d'erreur, puis niveau d'accès), les mêmes sous l'adresse internet du Pi
// que sur son routeur Docker.
func RouterMiddlewares(app, access string) []string {
	return strings.Split(routerMiddlewares(app, access), ",")
}
