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

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// Qui ouvre la page d'un service (spécification 8.2).
const (
	// AccessPublic : tout le monde, depuis internet.
	AccessPublic = "public"
	// AccessUsers : les personnes connectées à la console (forward auth).
	AccessUsers = "users"
	// AccessHome : les appareils de la maison (et du VPN) seulement.
	AccessHome = "home"
	// AccessAdmins : les administrateurs de la console, depuis la maison (et
	// le VPN). Réservé aux applications qui ont la main sur tout le Pi
	// (Portainer) : posé par leur manifeste, jamais choisi ni changé.
	AccessAdmins = "admins"
)

// Middlewares Traefik définis dans system/etc/pi-appliance/traefik/dynamic/console.yml.
const (
	authMiddleware   = "pi-appliance-auth@file"
	homeMiddleware   = "pi-appliance-home@file"
	adminsMiddleware = homeMiddleware + ",pi-appliance-admins@file"
	// errorsMiddleware : page « service indisponible » de la console quand le
	// service ne répond pas (502 à 504), en premier sur chaque routeur.
	errorsMiddleware = "pi-appliance-errors@file"
)

// routerMiddlewares : middlewares du routeur Traefik d'un service, la page
// d'erreur puis celui de son niveau d'accès.
func routerMiddlewares(app, access string) string {
	if middleware := accessMiddleware(app, access); middleware != "" {
		return errorsMiddleware + "," + middleware
	}
	return errorsMiddleware
}

// ValidAccess dit si un niveau d'accès peut être choisi par l'administrateur
// (AccessAdmins ne vient que du manifeste).
func ValidAccess(access string) bool {
	return access == AccessPublic || access == AccessUsers || access == AccessHome
}

// accessMiddleware : middleware Traefik du niveau d'accès ; vide : public.
// Etherpad a le sien, qui accepte aussi ses liens de partage (etherpad.go).
func accessMiddleware(app, access string) string {
	switch access {
	case AccessUsers:
		if app == EtherpadApp {
			return padMiddleware
		}
		return authMiddleware
	case AccessHome:
		return homeMiddleware
	case AccessAdmins:
		return adminsMiddleware
	}
	return ""
}

// webAccess : niveau d'accès gardé à l'installation ; vide pour un service
// sans page web, public quand rien n'est choisi (Docker Compose libre).
func webAccess(app marketplace.App) string {
	if app.Port == 0 {
		return ""
	}
	if app.Access == "" {
		return AccessPublic
	}
	return app.Access
}

// SetAccess change qui ouvre la page d'un service : les labels Traefik sont
// réécrits et le service redémarré ; s'il ne redémarre pas, l'ancien niveau
// est remis. Un service réservé aux administrateurs (AccessAdmins) le reste.
func (m *Manager) SetAccess(ctx context.Context, name, access string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !ValidAccess(access) {
		return Instance{}, fmt.Errorf("%w : niveau d'accès « %s » inconnu (public, users ou home)", ErrInvalid, access)
	}
	if err := m.installed(name); err != nil {
		return Instance{}, err
	}
	dir := m.serviceDir(name)
	statePath := filepath.Join(dir, stateName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		return Instance{}, err
	}
	var instance Instance
	if err := json.Unmarshal(data, &instance); err != nil {
		return Instance{}, fmt.Errorf("%s : %w", statePath, err)
	}
	if instance.NoWebPage {
		return Instance{}, fmt.Errorf("%w : %s n'a pas de page web, son accès ne se règle pas ici", ErrInvalid, name)
	}
	if instance.Access == AccessAdmins {
		return Instance{}, fmt.Errorf("%w : %s a la main sur tout le Pi, il reste réservé aux administrateurs, à la maison", ErrInvalid, name)
	}
	overridePath := filepath.Join(dir, overrideName)
	previous, err := os.ReadFile(overridePath)
	if err != nil {
		return Instance{}, err
	}
	app := instance.App
	// NE PAS exiger la connexion d'Etherpad sans le mot de passe de son compte
	// interne (installé avant les liens de partage) : plus personne n'entrerait.
	if _, ok := m.etherpadAuthorization(name); app == EtherpadApp && !ok {
		app = ""
	}
	updated, err := withAccess(previous, name, app, access)
	if err != nil {
		return Instance{}, err
	}
	if err := os.WriteFile(overridePath, updated, 0o600); err != nil {
		return Instance{}, err
	}
	// Les labels ne changent qu'en recréant le conteneur.
	if output, err := m.compose(ctx, name, "up", "--detach", "--wait"); err != nil {
		failure := trimOutput(output, err)
		if writeErr := os.WriteFile(overridePath, previous, 0o600); writeErr != nil {
			return Instance{}, fmt.Errorf("%s n'a pas redémarré (%s) et l'ancien réglage n'a pas pu être remis : %v", name, failure, writeErr)
		}
		if output, err := m.compose(context.WithoutCancel(ctx), name, "up", "--detach", "--wait"); err != nil {
			return Instance{}, fmt.Errorf("%s n'a pas redémarré (%s), ni avec l'ancien réglage : %s", name, failure, trimOutput(output, err))
		}
		return Instance{}, fmt.Errorf("%s n'a pas redémarré, l'ancien réglage est remis : %s", name, failure)
	}
	instance.Access = access
	if err := writeJSON(statePath, instance, 0o600); err != nil {
		return Instance{}, err
	}
	return instance, nil
}

// withAccess pose la page d'erreur et le middleware d'accès (aucun pour public) sur le
// routeur Traefik du service dans le fichier pi-appliance.override.json.
func withAccess(override []byte, name, app, access string) ([]byte, error) {
	var file map[string]any
	if err := json.Unmarshal(override, &file); err != nil {
		return nil, fmt.Errorf("réglages de %s illisibles : %w", name, err)
	}
	router := "traefik.http.routers.pi-" + name
	services, _ := file["services"].(map[string]any)
	found := false
	for _, value := range services {
		service, _ := value.(map[string]any)
		labels, _ := service["labels"].(map[string]any)
		if _, ok := labels[router+".rule"]; !ok {
			continue
		}
		found = true
		labels[router+".middlewares"] = routerMiddlewares(app, access)
		if app == EtherpadApp {
			service["environment"] = etherpadEnvironment(access)
		}
	}
	if !found {
		return nil, fmt.Errorf("%w : la page web de %s est introuvable dans ses réglages", ErrInvalid, name)
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// fullControl : options qui donnent tous les droits sur le Pi, comme le compte
// root (custom.go) : accès à Docker, conteneur privilégié, programmes du Pi
// visibles (pid: host, qui permet d'arrêter tout programme du Pi et, avec un
// conteneur privilégié, d'y lancer des commandes : Claude Code). Les droits
// ajoutés limités du VPN (cap_add) n'en sont pas.
var fullControl = map[string]bool{"docker-socket": true, "privileged": true, "host-pid": true}

// MarketplaceRisks : options d'une application de la marketplace qui donnent
// tous les droits sur le Pi (accès à Docker, conteneur privilégié, pid: host), à faire
// confirmer par l'administrateur avant de l'installer. Une application qui en
// a doit être réservée aux administrateurs, à la maison (access: admins) :
// sinon elle est refusée.
func MarketplaceRisks(app marketplace.App) ([]Risk, error) {
	_, risks, err := AnalyzeCompose(app.Compose)
	if err != nil {
		return nil, err
	}
	var full []Risk
	for _, risk := range risks {
		if fullControl[risk.Code] {
			full = append(full, risk)
		}
	}
	if len(full) > 0 && app.Access != AccessAdmins {
		return nil, fmt.Errorf("%w : %s a la main sur tout le Pi, son manifeste doit le réserver aux administrateurs, à la maison (access: admins)", ErrInvalid, app.Name)
	}
	return full, nil
}
