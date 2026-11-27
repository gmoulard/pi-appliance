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

package services

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// Docker Compose libre (spécification 6.4) : un administrateur installe
// n'importe quel fichier Docker Compose, hors marketplace. Les options qui
// donnent la main sur tout le Pi sont signalées et doivent être confirmées ;
// elles ne sont pas bloquées.

// CustomApp : identifiant d'application des services installés hors marketplace.
const CustomApp = "compose-libre"

// Unsupported : avertissement de tout service installé hors marketplace.
const Unsupported = "Ce service ne vient pas de la marketplace : il n'est ni vérifié ni pris en charge par le projet pi-appliance."

// Risk : une option dangereuse d'un fichier Docker Compose ou d'un réglage avancé.
type Risk struct {
	Service string `json:"service"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RisksError : options dangereuses à confirmer avant d'installer ou d'appliquer.
type RisksError struct {
	Risks []Risk
}

func (e RisksError) Error() string {
	return "options dangereuses à confirmer"
}

// riskService reprend les clés d'un service Compose qui touchent au Pi lui-même.
type riskService struct {
	Image       string `yaml:"image"`
	Build       any    `yaml:"build"`
	Privileged  bool   `yaml:"privileged"`
	NetworkMode string `yaml:"network_mode"`
	PID         string `yaml:"pid"`
	IPC         string `yaml:"ipc"`
	UsernsMode  string `yaml:"userns_mode"`
	Volumes     []any  `yaml:"volumes"`
	Devices     []any  `yaml:"devices"`
	CapAdd      []any  `yaml:"cap_add"`
	SecurityOpt []any  `yaml:"security_opt"`
}

type riskFile struct {
	Services map[string]riskService `yaml:"services"`
}

// systemDirs : dossiers du Pi dont le montage donne la main sur le système.
var systemDirs = []string{"/boot", "/dev", "/etc", "/proc", "/root", "/run", "/sys", "/usr", "/var/lib/docker", "/var/lib/pi-appliance"}

// AnalyzeCompose lit un fichier Docker Compose : noms des services, dans l'ordre
// alphabétique, et options dangereuses. Un fichier illisible, sans service, avec
// un service sans image ou à construire (build) est refusé.
func AnalyzeCompose(data []byte) ([]string, []Risk, error) {
	var file riskFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, nil, fmt.Errorf("%w : le fichier Docker Compose est illisible : %v", ErrInvalid, err)
	}
	if len(file.Services) == 0 {
		return nil, nil, fmt.Errorf("%w : le fichier Docker Compose n'a aucun service (section services)", ErrInvalid)
	}
	names := make([]string, 0, len(file.Services))
	for name := range file.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	var risks []Risk
	for _, name := range names {
		service := file.Services[name]
		if service.Build != nil {
			return nil, nil, fmt.Errorf("%w : le service « %s » est à construire (build) : le Pi n'installe que des images toutes prêtes", ErrInvalid, name)
		}
		if strings.TrimSpace(service.Image) == "" {
			return nil, nil, fmt.Errorf("%w : le service « %s » n'a pas d'image", ErrInvalid, name)
		}
		risks = append(risks, serviceRisks(name, service)...)
	}
	return names, risks, nil
}

func serviceRisks(name string, s riskService) []Risk {
	var risks []Risk
	add := func(code, format string, args ...any) {
		risks = append(risks, Risk{Service: name, Code: code, Message: fmt.Sprintf("« %s » ", name) + fmt.Sprintf(format, args...)})
	}
	if s.Privileged {
		add("privileged", "est un conteneur privilégié (privileged) : il a la main sur tout le Pi.")
	}
	if s.NetworkMode == "host" {
		add("host-network", "utilise le réseau du Pi lui-même (network_mode: host) : il voit tout le réseau et contourne Traefik.")
	}
	if s.PID == "host" {
		add("host-pid", "voit et peut arrêter tous les programmes du Pi (pid: host).")
	}
	if s.IPC == "host" {
		add("host-ipc", "partage la mémoire des programmes du Pi (ipc: host).")
	}
	if s.UsernsMode == "host" {
		add("host-userns", "désactive l'isolation des comptes du conteneur (userns_mode: host).")
	}
	for _, volume := range s.Volumes {
		if source, readOnly, ok := bindSource(volume); ok {
			risks = append(risks, mountRisk(name, source, readOnly))
		}
	}
	if len(s.Devices) > 0 {
		add("devices", "accède directement au matériel du Pi (devices).")
	}
	if len(s.CapAdd) > 0 {
		capabilities := make([]string, 0, len(s.CapAdd))
		for _, capability := range s.CapAdd {
			capabilities = append(capabilities, fmt.Sprint(capability))
		}
		add("capabilities", "reçoit des droits supplémentaires sur le Pi (cap_add : %s).", strings.Join(capabilities, ", "))
	}
	for _, option := range s.SecurityOpt {
		if text := fmt.Sprint(option); strings.Contains(text, "unconfined") || strings.Contains(text, "disable") {
			add("security-off", "désactive des protections du conteneur (security_opt : %s).", text)
		}
	}
	return risks
}

// bindSource : dossier du Pi monté par un volume Compose, en syntaxe courte
// (« /dossier:/cible[:ro] ») ou longue (type: bind) ; faux pour un volume nommé.
func bindSource(volume any) (source string, readOnly, ok bool) {
	switch v := volume.(type) {
	case string:
		parts := strings.Split(v, ":")
		if len(parts) < 2 {
			return "", false, false // volume anonyme
		}
		source = parts[0]
		readOnly = len(parts) > 2 && strings.Contains(parts[2], "ro")
		if strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~") || strings.HasPrefix(source, "$") {
			return source, readOnly, true
		}
	case map[string]any:
		if fmt.Sprint(v["type"]) == "bind" {
			return fmt.Sprint(v["source"]), v["read_only"] == true, true
		}
	}
	return "", false, false
}

// mountRisk décrit le montage d'un dossier du Pi dans un conteneur.
func mountRisk(service, source string, readOnly bool) Risk {
	can := "lire et modifier"
	if readOnly {
		can = "lire"
	}
	clean := source
	if strings.HasPrefix(source, "/") {
		clean = path.Clean(source)
	}
	risk := Risk{Service: service, Code: "host-mount"}
	switch {
	case clean == "/var/run/docker.sock" || clean == "/run/docker.sock":
		risk.Code = "docker-socket"
		risk.Message = fmt.Sprintf("« %s » a accès à Docker (%s) : il peut prendre la main sur tout le Pi.", service, source)
	case clean == "/":
		risk.Message = fmt.Sprintf("« %s » monte tout le système du Pi (/) : il peut %s tous ses fichiers.", service, can)
	case isSystemDir(clean):
		risk.Message = fmt.Sprintf("« %s » monte le dossier système %s du Pi : il peut %s ces fichiers.", service, source, can)
	default:
		risk.Message = fmt.Sprintf("« %s » monte le dossier %s du Pi : il peut %s ce qu'il contient.", service, source, can)
	}
	return risk
}

func isSystemDir(dir string) bool {
	for _, system := range systemDirs {
		if dir == system || strings.HasPrefix(dir, system+"/") {
			return true
		}
	}
	return false
}

// CustomRequest : un fichier Docker Compose libre à installer.
type CustomRequest struct {
	Name    string
	Compose []byte
	// Service et Port : le service publié par Traefik à l'adresse <nom>.<adresse> et son port interne.
	Service string
	Port    int
	// ConfirmRisks : l'administrateur a confirmé les options dangereuses.
	ConfirmRisks bool
	// SourceURL : adresse où la console a lu le fichier ; SourceEdited : il a
	// été modifié avant l'installation (source.go).
	SourceURL    string
	SourceEdited bool
	Env          string
}

// InstallCustom installe un fichier Docker Compose hors marketplace, avec les
// mêmes labels Traefik, le même réseau et la même désinstallation que les autres.
func (m *Manager) InstallCustom(ctx context.Context, request CustomRequest) (Result, error) {
	m.forgetProgress(request.Name)
	names, risks, err := AnalyzeCompose(request.Compose)
	if err != nil {
		return Result{}, err
	}
	known := false
	for _, name := range names {
		known = known || name == request.Service
	}
	if !known {
		return Result{}, fmt.Errorf("%w : le service publié « %s » n'est pas dans le fichier (services : %s)", ErrInvalid, request.Service, strings.Join(names, ", "))
	}
	if request.Port < 1 || request.Port > 65535 {
		return Result{}, fmt.Errorf("%w : le port du service publié va de 1 à 65535", ErrInvalid)
	}
	adminEnv, err := normalizeAdminEnv(request.Env)
	if err != nil {
		return Result{}, err
	}
	if len(risks) > 0 && !request.ConfirmRisks {
		return Result{}, RisksError{Risks: risks}
	}
	app := marketplace.App{
		ID:        CustomApp,
		Name:      "Docker Compose libre",
		Subdomain: request.Name,
		Service:   request.Service,
		Port:      request.Port,
		Compose:   request.Compose,
	}
	result, err := m.installWith(ctx, app, request.Name, nil, adminEnv)
	warnings := []string{Unsupported}
	for _, risk := range risks {
		warnings = append(warnings, risk.Message)
	}
	if err == nil {
		if err := m.writeSource(request.Name, sourceFile{URL: request.SourceURL, Edited: request.SourceEdited, Service: request.Service, Port: request.Port}); err != nil {
			warnings = append(warnings, fmt.Sprintf("Le fichier est installé, mais son adresse, son conteneur et son port n'ont pas pu être notés pour le bouton « Source » : %v", err))
		}
	}
	result.Warnings = append(warnings, result.Warnings...)
	return result, err
}
