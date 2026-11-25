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

// Hébergement d'une application qui n'est pas dans le catalogue, par exemple
// le serveur de NautiLink (spécification 6.4 ; TODO-2.72, TODO-2.74, TODO-3.18) :
//   - SubPath : un Docker Compose libre déclaré capable de marcher sous le
//     chemin /<nom>/ est publié sur internet sous ce chemin, gardé ;
//   - Mail : le service envoie des e-mails par le relais du Pi
//     (internal/relay), joint à l'adresse pi-appliance-mail:2525 ;
//   - RedirectHosts : domaines personnels renvoyés par Traefik vers
//     l'adresse internet du service (écrit par la console).
// La base du PostgreSQL partagé (TODO-2.73) est dans database.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	// MailHost, MailPort : adresse du relais d'e-mails vue des conteneurs
	// (extra_hosts « host-gateway » : l'adresse du Pi sur le pont Docker).
	MailHost = "pi-appliance-mail"
	MailPort = 2525
	// MaxRedirectHosts : domaines de redirection par service.
	MaxRedirectHosts = 4
)

var hostnamePattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

// changeInstance lit l'état d'un service, le change par f et l'écrit.
// Appelé sous m.mu.
func (m *Manager) changeInstance(name string, f func(*Instance) error) (Instance, error) {
	instance, err := m.instance(name)
	if err != nil {
		return Instance{}, err
	}
	if err := f(&instance); err != nil {
		return Instance{}, err
	}
	if err := writeJSON(filepath.Join(m.serviceDir(name), stateName), instance, 0o600); err != nil {
		return Instance{}, err
	}
	return instance, nil
}

// SetSubPath dit si un Docker Compose libre marche sous le chemin /<nom>/ :
// la console le publie alors sur internet avec ce chemin (InternetPath). Les
// services du catalogue ne se règlent pas ainsi (liste pathApps).
func (m *Manager) SetSubPath(name string, on bool) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.changeInstance(name, func(instance *Instance) error {
		if instance.App != CustomApp {
			return fmt.Errorf("%w : seul un Docker Compose libre se règle ainsi ; %s suit sa fiche de la marketplace", ErrInvalid, name)
		}
		if instance.NoWebPage {
			return fmt.Errorf("%w : %s n'a pas de page web", ErrInvalid, name)
		}
		instance.SubPath = on
		return nil
	})
}

// SetMail ouvre ou ferme le relais d'e-mails à un service : variables
// PI_SMTP_HOST et PI_SMTP_PORT et adresse pi-appliance-mail dans son
// conteneur publié, service redémarré.
func (m *Manager) SetMail(ctx context.Context, name string, on bool) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	instance, err := m.changeInstance(name, func(instance *Instance) error {
		if instance.NoWebPage {
			return fmt.Errorf("%w : %s n'a pas de page web", ErrInvalid, name)
		}
		instance.Mail = on
		return nil
	})
	if err != nil {
		return Instance{}, err
	}
	return instance, m.applyHosting(ctx, name)
}

// SetRedirectHosts garde les domaines personnels renvoyés vers l'adresse
// internet du service (au plus MaxRedirectHosts, aucun déjà pris par un autre
// service). Traefik obtient leur certificat quand leur DNS mène au Pi.
func (m *Manager) SetRedirectHosts(name string, hosts []string) (Instance, error) {
	clean := make([]string, 0, len(hosts))
	for _, host := range hosts {
		host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
		if host == "" || slices.Contains(clean, host) {
			continue
		}
		if !hostnamePattern.MatchString(host) || len(host) > 253 {
			return Instance{}, fmt.Errorf("%w : « %s » n'est pas un nom de domaine (par exemple nautilink.exemple.org)", ErrInvalid, host)
		}
		clean = append(clean, host)
	}
	if len(clean) > MaxRedirectHosts {
		return Instance{}, fmt.Errorf("%w : %d domaines au plus", ErrInvalid, MaxRedirectHosts)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	instances, err := m.List()
	if err != nil {
		return Instance{}, err
	}
	for _, other := range instances {
		for _, host := range other.RedirectHosts {
			if other.Name != name && slices.Contains(clean, host) {
				return Instance{}, fmt.Errorf("%w : %s renvoie déjà vers le service « %s »", ErrExists, host, other.Name)
			}
		}
	}
	return m.changeInstance(name, func(instance *Instance) error {
		if instance.NoWebPage {
			return fmt.Errorf("%w : %s n'a pas de page web", ErrInvalid, name)
		}
		instance.RedirectHosts = nil
		if len(clean) > 0 {
			instance.RedirectHosts = clean
		}
		return nil
	})
}

// hostingEnv : variables d'hébergement ajoutées au fichier .env du service,
// pour que son fichier Compose les reprenne (${PI_DATABASE_URL}…).
func (m *Manager) hostingEnv(name string, env map[string]string) {
	instance, err := m.instance(name)
	if err != nil {
		return // installation : pas encore d'état
	}
	if instance.Mail {
		env["PI_SMTP_HOST"] = MailHost
		env["PI_SMTP_PORT"] = fmt.Sprint(MailPort)
	}
	if instance.Database != nil {
		if password, err := m.databasePassword(name); err == nil {
			for key, value := range instance.Database.env(password) {
				env[key] = value
			}
		}
	}
}

// hostingOverride complète le fichier override d'un service : réseau de la
// base pour le serveur PostgreSQL et pour ses clients, adresse du relais
// d'e-mails. doc : override décodé (map[string]any, comme du JSON).
func (m *Manager) hostingOverride(name, app string, doc map[string]any) {
	services, _ := doc["services"].(map[string]any)
	networks, _ := doc["networks"].(map[string]any)
	if services == nil || networks == nil {
		return
	}
	if app == PostgresApp {
		db, _ := services[postgresService].(map[string]any)
		if db == nil {
			db = map[string]any{}
			services[postgresService] = db
		}
		db["networks"] = map[string]any{
			"default":       map[string]any{},
			DatabaseNetwork: map[string]any{"aliases": []any{databaseHost(name)}},
		}
		networks[DatabaseNetwork] = map[string]any{"external": true}
	}
	instance, err := m.instance(name)
	if err != nil {
		return
	}
	main := publishedService(services)
	if main == nil {
		return
	}
	if instance.Database != nil {
		main["networks"] = addToList(main["networks"], DatabaseNetwork)
		networks[DatabaseNetwork] = map[string]any{"external": true}
	}
	if instance.Mail {
		main["extra_hosts"] = addToList(main["extra_hosts"], MailHost+":host-gateway")
	}
}

// publishedService : le service du fichier override publié par Traefik.
func publishedService(services map[string]any) map[string]any {
	for _, value := range services {
		service, _ := value.(map[string]any)
		labels, _ := service["labels"].(map[string]any)
		if labels["traefik.enable"] == "true" {
			return service
		}
	}
	return nil
}

func addToList(list any, value string) []any {
	var out []any
	switch items := list.(type) {
	case []any:
		out = append(out, items...)
	case []string:
		for _, item := range items {
			out = append(out, item)
		}
	}
	for _, item := range out {
		if item == value {
			return out
		}
	}
	return append(out, value)
}

// rewriteOverride relit l'override écrit d'un service et le complète
// (hostingOverride) ; il ne retire rien.
func (m *Manager) rewriteOverride(name string) error {
	instance, err := m.instance(name)
	if err != nil {
		return err
	}
	path := filepath.Join(m.serviceDir(name), overrideName)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s : %w", path, err)
	}
	m.removeHosting(doc, instance)
	m.hostingOverride(name, instance.App, doc)
	return writeJSON(path, doc, 0o600)
}

// removeHosting retire de l'override ce que l'état du service ne demande plus.
func (m *Manager) removeHosting(doc map[string]any, instance Instance) {
	services, _ := doc["services"].(map[string]any)
	main := publishedService(services)
	if main == nil {
		return
	}
	if !instance.Mail {
		main["extra_hosts"] = without(main["extra_hosts"], MailHost+":host-gateway")
		if list, _ := main["extra_hosts"].([]any); len(list) == 0 {
			delete(main, "extra_hosts")
		}
	}
	if instance.Database == nil && instance.App != PostgresApp {
		main["networks"] = without(main["networks"], DatabaseNetwork)
		if networks, _ := doc["networks"].(map[string]any); networks != nil {
			delete(networks, DatabaseNetwork)
		}
	}
}

func without(list any, value string) []any {
	var out []any
	for _, item := range addToList(list, "") {
		if item != value && item != "" {
			out = append(out, item)
		}
	}
	return out
}

// applyHosting réécrit .env et override d'un service selon son état, puis le
// redémarre (seuls les conteneurs changés sont recréés). Appelé sous m.mu.
func (m *Manager) applyHosting(ctx context.Context, name string) error {
	dir := m.serviceDir(name)
	env, err := readEnv(filepath.Join(dir, envName))
	if err != nil {
		return err
	}
	for _, key := range []string{"PI_SMTP_HOST", "PI_SMTP_PORT", "PI_DATABASE_URL", "PI_DATABASE_HOST", "PI_DATABASE_PORT", "PI_DATABASE_NAME", "PI_DATABASE_USER", "PI_DATABASE_PASSWORD"} {
		delete(env, key)
	}
	m.hostingEnv(name, env)
	if err := writeEnv(dir, env); err != nil {
		return err
	}
	if err := m.rewriteOverride(name); err != nil {
		return err
	}
	if err := m.ensureNetwork(ctx, dir, DatabaseNetwork); err != nil {
		return err
	}
	if output, err := m.compose(ctx, name, "up", "--detach", "--wait"); err != nil {
		return fmt.Errorf("%s n'a pas redémarré : %s", name, trimOutput(output, err))
	}
	return nil
}

// ensureNetwork crée un réseau Docker partagé s'il n'existe pas.
func (m *Manager) ensureNetwork(ctx context.Context, dir, network string) error {
	if _, err := m.Runner.Run(ctx, dir, "network", "inspect", network); err != nil {
		if output, err := m.Runner.Run(ctx, dir, "network", "create", network); err != nil {
			return fmt.Errorf("préparation du réseau %s impossible : %s", network, trimOutput(output, err))
		}
	}
	return nil
}

// ServiceByIP : le service dont un conteneur a cette adresse sur le réseau
// pi-appliance (expéditeur d'un e-mail au relais). Vide si aucun.
func (m *Manager) ServiceByIP(ctx context.Context, ip string) (string, error) {
	output, err := m.Runner.Run(ctx, m.Dir, "network", "inspect", Network, "--format", "{{json .Containers}}")
	if err != nil {
		return "", fmt.Errorf("réseau %s illisible : %s", Network, trimOutput(output, err))
	}
	var containers map[string]struct {
		IPv4Address string `json:"IPv4Address"`
	}
	if err := json.Unmarshal(output, &containers); err != nil {
		return "", err
	}
	for id, container := range containers {
		address, _, _ := strings.Cut(container.IPv4Address, "/")
		if address != ip {
			continue
		}
		output, err := m.Runner.Run(ctx, m.Dir, "inspect", "--format", `{{index .Config.Labels "com.docker.compose.project"}}`, id)
		if err != nil {
			return "", fmt.Errorf("conteneur %s illisible : %s", id, trimOutput(output, err))
		}
		project := strings.TrimSpace(string(output))
		if name, ok := strings.CutPrefix(project, "pi-"); ok {
			return name, nil
		}
		return "", nil
	}
	return "", nil
}

// MailAllowed : le service peut envoyer par le relais, et son nom affiché.
func (m *Manager) MailAllowed(name string) (string, bool) {
	instance, err := m.instance(name)
	if err != nil || !instance.Mail {
		return "", false
	}
	return instance.Name, true
}

// RelayAddress : adresse d'écoute du relais d'e-mails, celle du Pi sur le
// pont Docker par défaut (« host-gateway » des conteneurs, 172.17.0.1 le plus
// souvent), au port MailPort.
func (m *Manager) RelayAddress(ctx context.Context) (string, error) {
	output, err := m.Runner.Run(ctx, m.Dir, "network", "inspect", "bridge", "--format", "{{range .IPAM.Config}}{{.Gateway}} {{end}}")
	if err != nil {
		return "", fmt.Errorf("pont Docker illisible : %s", trimOutput(output, err))
	}
	for _, field := range strings.Fields(string(output)) {
		if ip := net.ParseIP(field); ip != nil && ip.To4() != nil {
			return net.JoinHostPort(ip.String(), fmt.Sprint(MailPort)), nil
		}
	}
	return "", fmt.Errorf("pont Docker sans adresse IPv4")
}
