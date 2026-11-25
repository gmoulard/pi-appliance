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
// X-Forwarded-Prefix) ; « base » : application réglée sur le chemin /<nom>/
// pour ses deux adresses (chemin de base dans son manifeste, pathMarkers) :
// Traefik ne retire pas le chemin (KeepsPath). Pour « build » et « base », un
// service installé avant doit être mis à jour.
// Un Docker Compose libre n'y est que si l'administrateur dit qu'il y marche
// (Instance.SubPath, chemin gardé : InstanceKeepsPath).
var pathApps = map[string]string{
	"site-web": "relative",
	"mkdocs":   "relative",
	"hugo":     "build",
	"jekyll":   "build",
	// Adresse du site (WordPress) ou chemin de base (DokuWiki) pris dans
	// X-Forwarded-Prefix (demande de l'utilisateur du 2026-11-25).
	"wordpress": "build",
	"dokuwiki":  "build",
	// Page et appels du serveur en adresses relatives (TODO-2.69).
	"wordpress-markdown": "relative",
	// baseURL /<nom>/ ; à la maison, l'application renvoie / vers /<nom>/.
	"filebrowser": "base",
}

// pathMarkers : texte du fichier Compose installé qui dit qu'un service
// « build » ou « base » a le réglage du chemin (sinon : PathUpdate).
var pathMarkers = map[string]string{
	"hugo":        "SITE_PATH",
	"jekyll":      "SITE_PATH",
	"wordpress":   "HTTP_X_FORWARDED_PREFIX",
	"dokuwiki":    "HTTP_X_FORWARDED_PREFIX",
	"filebrowser": "baseURL: /${PI_INSTANCE}/",
}

// InternetPath dit si le service s'ouvre sous le chemin /<nom>/ de l'adresse
// internet du Pi, sinon pourquoi (PathUnsupported, PathUpdate, PathReserved).
func (m *Manager) InternetPath(instance Instance) string {
	if slices.Contains(ReservedNames, instance.Name) {
		return PathReserved
	}
	if instance.App == CustomApp && instance.SubPath {
		return PathReady
	}
	switch pathApps[instance.App] {
	case "relative":
		return PathReady
	case "build", "base":
		compose, err := os.ReadFile(filepath.Join(m.serviceDir(instance.Name), composeName))
		if err != nil || !bytes.Contains(compose, []byte(pathMarkers[instance.App])) {
			return PathUpdate
		}
		return PathReady
	}
	return PathUnsupported
}

// KeepsPath dit si l'application attend le chemin /<nom>/ dans l'adresse
// (mode « base ») : son routeur sous l'adresse internet du Pi ne le retire pas.
func KeepsPath(app string) bool {
	return pathApps[app] == "base"
}

// InstanceKeepsPath : comme KeepsPath, et pour un Docker Compose libre
// déclaré capable de marcher sous /<nom>/ (SubPath, TODO-2.72).
func InstanceKeepsPath(instance Instance) bool {
	return KeepsPath(instance.App) || (instance.App == CustomApp && instance.SubPath)
}

// RouterMiddlewares : middlewares Traefik du routeur d'un service (page
// d'erreur, puis niveau d'accès), les mêmes sous l'adresse internet du Pi
// que sur son routeur Docker.
func RouterMiddlewares(app, access string) []string {
	return strings.Split(routerMiddlewares(app, access), ",")
}
