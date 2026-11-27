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
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const (
	Network      = "pi-appliance"
	composeName  = "compose.yaml"
	overrideName = "pi-appliance.override.json"
	envName      = ".env"
	stateName    = "instance.json"
	// siteDir : fichiers du site, déposés par Nextcloud.
	siteDir = "www"
)

var (
	ErrInvalid  = errors.New("demande invalide")
	ErrExists   = errors.New("service déjà installé")
	ErrNotFound = errors.New("service inconnu")
)

type Runner interface {
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

type Docker struct{}

func (Docker) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	command.Dir = dir
	return command.CombinedOutput()
}

type Instance struct {
	Name        string    `json:"name"`
	App         string    `json:"app"`
	AppName     string    `json:"app_name"`
	Address     string    `json:"address"`
	InstalledAt time.Time `json:"installed_at"`
	// NoWebPage : service sans page web (VPN…), joint par ses HostPorts.
	NoWebPage bool                   `json:"no_web_page,omitempty"`
	HostPorts []marketplace.HostPort `json:"host_ports,omitempty"`
	// Access : qui ouvre la page du service (public, users, home, admins ; access.go).
	Access string `json:"access,omitempty"`
	// SiteFiles : fichiers du site déposés par Nextcloud (manifeste, champ files).
	SiteFiles bool `json:"site_files,omitempty"`
	// Build : dernière construction d'un générateur de site statique.
	Build *Build `json:"build,omitempty"`
	// NextcloudFolder : dossier du site dans Nextcloud, une fois relié ;
	// NextcloudPending : Nextcloud installé, dossier pas encore créé.
	NextcloudFolder  string `json:"nextcloud_folder,omitempty"`
	NextcloudPending bool   `json:"nextcloud_pending,omitempty"`
	// Tags : étiquettes d'un service hors catalogue, choisies par l'administrateur
	// tant que sa fiche le permettait (réglage retiré le 2026-11-27, TODO-5.28) :
	// gardées et lues ; un service du catalogue prend celles de son application.
	Tags []string `json:"tags,omitempty"`
	// SubPath : Docker Compose libre qui marche sous le chemin /<nom>/,
	// publié sur internet avec ce chemin gardé (SetSubPath, TODO-2.72).
	SubPath bool `json:"sub_path,omitempty"`
	// Mail : le service envoie des e-mails par le relais du Pi (SetMail, TODO-2.74).
	Mail bool `json:"mail,omitempty"`
	// RedirectHosts : domaines personnels renvoyés vers l'adresse internet du
	// service (SetRedirectHosts, TODO-3.18).
	RedirectHosts []string `json:"redirect_hosts,omitempty"`
	// Database : base du PostgreSQL partagé créée pour ce service (database.go, TODO-2.73).
	Database *Database `json:"database,omitempty"`
	// LocalCertificate : certificat local du Pi vu par le service (clé local_certificate).
	LocalCertificate string `json:"local_certificate,omitempty"`
}

type Result struct {
	Instance Instance          `json:"instance"`
	Warnings []string          `json:"warnings,omitempty"`
	Secrets  map[string]string `json:"secrets,omitempty"`
}

type Manager struct {
	Dir    string
	Domain string
	// FollowName : l'adresse des services suit le nom du Pi, <nom>.local,
	// quand l'administrateur le change (Renamed) ; faux si -domain est donné.
	FollowName bool
	Runner     Runner
	MemoryMB   func() int
	// Measure mesure la mémoire et le disque du Pi ; nil : inconnus.
	Measure func(dir string) Resources
	// Copier copie les données d'un service avant sa mise à jour ; nil : « cp -a ».
	Copier Copier
	// FreeMB donne la place libre du disque qui porte un dossier ; nil : lue
	// par internal/system.
	FreeMB func(dir string) (int64, bool)
	// HomeNetworks donne les réseaux de la maison pour le VPN ; nil : lus
	// sur les interfaces du Pi.
	HomeNetworks func() []netip.Prefix
	// Images télécharge les images avec l'avancement (Progress) ; nil :
	// Compose les télécharge à la création, sans chiffres.
	Images ImagePuller
	// Firewall charge le pare-feu des ports « maison seulement »
	// (homeports.go) ; nil : « nft -f ».
	Firewall func(ctx context.Context, path string) error
	// LocalCertificate : certificat local du Pi (local.crt), copié dans les
	// services qui le demandent (clé local_certificate) ; vide : aucun.
	LocalCertificate string
	// ReadHomeIPv6 lit les préfixes IPv6 globaux de la maison ; nil : sur les
	// interfaces du Pi (homeipv6.go).
	ReadHomeIPv6 func() []netip.Prefix
	// TraefikHomeFile : configuration dynamique de Traefik des plages de la
	// maison (pi-appliance-home@file) ; vide : aucune.
	TraefikHomeFile string
	homeIPv6        atomic.Pointer[[]netip.Prefix]
	mu              sync.Mutex
	progressMu      sync.Mutex
	progress        map[string]*Progress
	renamed         atomic.Pointer[string]
	linkMu          sync.Mutex
	wakeOnce        sync.Once
	wake            chan struct{}
}

// CurrentDomain : adresse de l'appliance, d'où vient l'adresse de chaque service (ServiceAddress).
func (m *Manager) CurrentDomain() string {
	if domain := m.renamed.Load(); domain != nil {
		return *domain
	}
	return m.Domain
}

// Renamed prend le nouveau nom du Pi sans relancer la console : les services
// installés ensuite ont l'adresse <service>-<nom>.local, et Relocate y déplace
// ceux déjà installés.
func (m *Manager) Renamed(name string) {
	if !m.FollowName || !marketplace.ValidLabel(name) {
		return
	}
	domain := strings.ToLower(name) + ".local"
	m.renamed.Store(&domain)
}

// Resources mesure la mémoire du Pi et la place libre du dossier de données.
func (m *Manager) Resources() Resources {
	if m.Measure != nil {
		return m.Measure(m.Dir)
	}
	if m.MemoryMB != nil {
		return Resources{MemoryTotalMB: m.MemoryMB()}
	}
	return Resources{}
}

func (m *Manager) serviceDir(name string) string {
	return filepath.Join(m.Dir, "services", name)
}

func (m *Manager) compose(ctx context.Context, name string, args ...string) ([]byte, error) {
	base := []string{"compose", "--project-name", "pi-" + name, "--env-file", envName, "--file", composeName, "--file", overrideName}
	// Réglages avancés (spécification 6.4), lus en dernier.
	if _, err := os.Stat(filepath.Join(m.serviceDir(name), advancedName)); err == nil {
		base = append(base, "--file", advancedName)
	}
	return m.Runner.Run(ctx, m.serviceDir(name), append(base, args...)...)
}

func (m *Manager) Install(ctx context.Context, app marketplace.App, name string, values map[string]string) (Result, error) {
	m.forgetProgress(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !marketplace.ValidLabel(name) {
		return Result{}, fmt.Errorf("%w : le nom « %s » ne convient pas, utilisez des lettres minuscules, des chiffres et des tirets", ErrInvalid, name)
	}
	// Adresse sur internet https://<adresse du Pi>/<nom>/ (internet.go).
	if slices.Contains(ReservedNames, name) {
		return Result{}, fmt.Errorf("%w : le nom « %s » est réservé à la console, choisissez un autre nom", ErrInvalid, name)
	}
	dir := m.serviceDir(name)
	if _, err := os.Stat(filepath.Join(dir, stateName)); err == nil {
		return Result{}, fmt.Errorf("%w : un service s'appelle déjà « %s », choisissez un autre nom", ErrExists, name)
	}
	if app.ID != CustomApp {
		if _, err := MarketplaceRisks(app); err != nil {
			return Result{}, err
		}
	}
	m.track(name, app.Name)
	result, err := m.install(ctx, app, name, values)
	m.finish(name, err)
	return result, err
}

func (m *Manager) install(ctx context.Context, app marketplace.App, name string, values map[string]string) (Result, error) {
	dir := m.serviceDir(name)
	address := ServiceAddress(name, m.CurrentDomain())
	if err := checkAddress(name, address); err != nil {
		return Result{}, err
	}
	env, secrets, err := m.environment(app, dir, name, address, values)
	if err != nil {
		return Result{}, err
	}
	warnings, err := m.hostPortsFree(app, name)
	if err != nil {
		return Result{}, err
	}
	if m.Measure != nil || m.MemoryMB != nil {
		if memory := m.Resources().MemoryTotalMB; memory > 0 && memory < app.MemoryMB {
			warnings = append(warnings, fmt.Sprintf("Ce Raspberry Pi a %d Mo de mémoire et %s en demande %d : le service risque d'être lent.", memory, app.Name, app.MemoryMB))
		}
	}
	if err := m.writeFiles(dir, app, name, address, env); err != nil {
		return Result{}, err
	}
	if app.SiteFiles {
		if err := m.prepareSiteDir(name); err != nil {
			return Result{}, err
		}
	}
	if app.ID == WireGuardApp {
		if err := m.prepareWireGuard(dir); err != nil {
			return Result{}, err
		}
	}
	if homeOnly(app) {
		// NE PAS démarrer le service sans son pare-feu : ses ports seraient
		// ouverts à tous ceux qui joignent le Pi, internet compris.
		if err := m.ApplyHomePorts(ctx, app.HostPorts...); err != nil {
			return Result{}, fmt.Errorf("%s n'a pas été installé : %v", app.Name, err)
		}
	}
	if _, err := m.Runner.Run(ctx, dir, "network", "inspect", Network); err != nil {
		if output, err := m.Runner.Run(ctx, dir, "network", "create", Network); err != nil {
			return Result{}, fmt.Errorf("préparation du réseau impossible : %s", trimOutput(output, err))
		}
	}
	if app.ID == PostgresApp {
		if err := m.ensureNetwork(ctx, dir, DatabaseNetwork); err != nil {
			return Result{}, err
		}
	}
	m.setStep(name, StepDownload)
	pullErr := m.pullImages(ctx, name, false)
	for _, step := range []struct {
		name string
		args []string
	}{
		{StepCreate, []string{"create"}},
		{StepStart, []string{"start"}},
		{StepCheck, []string{"up", "--detach", "--wait"}},
	} {
		m.setStep(name, step.name)
		output, err := m.compose(ctx, name, step.args...)
		if err == nil {
			continue
		}
		if _, downErr := m.compose(context.WithoutCancel(ctx), name, "down"); downErr != nil {
			warnings = append(warnings, "Le service n'a pas pu être arrêté proprement.")
		}
		if homeOnly(app) {
			// Le service n'est pas installé : son port ne reste pas fermé pour rien.
			_ = m.ApplyHomePorts(context.WithoutCancel(ctx))
		}
		if step.name == StepCreate && pullErr != nil {
			return Result{Warnings: warnings}, fmt.Errorf("%s n'a pas pu être téléchargé : vérifiez que le Pi est bien relié à internet, puis réessayez (%v)", app.Name, pullErr)
		}
		return Result{Warnings: warnings}, fmt.Errorf("%s n'a pas pu démarrer : %s", app.Name, trimOutput(output, err))
	}
	instance := Instance{Name: name, App: app.ID, AppName: app.Name, Address: address, InstalledAt: time.Now().UTC(), NoWebPage: app.Port == 0, HostPorts: app.HostPorts, Access: webAccess(app)}
	if m.wantsLocalCertificate(app) {
		instance.LocalCertificate = localCertificateTarget(app)
	}
	if err := writeJSON(filepath.Join(dir, stateName), instance, 0o600); err != nil {
		return Result{}, err
	}
	if app.SiteFiles || app.ID == NextcloudApp {
		m.WakeNextcloud()
	}
	return Result{Instance: instance, Warnings: warnings, Secrets: secrets}, nil
}

// hostPortsFree refuse un port du Pi déjà ouvert par un autre service et
// rappelle la redirection de port à faire sur la box (accès direct, 7.2).
func (m *Manager) hostPortsFree(app marketplace.App, name string) ([]string, error) {
	if len(app.HostPorts) == 0 {
		return nil, nil
	}
	instances, err := m.List()
	if err != nil {
		return nil, err
	}
	var warnings []string
	for _, port := range app.HostPorts {
		for _, other := range instances {
			if other.Name != name && slices.Contains(other.HostPorts, port) {
				return nil, fmt.Errorf("%w : le port %s %d du Pi sert déjà au service « %s » ; une seule installation de %s est possible", ErrExists, strings.ToUpper(port.Protocol), port.Port, other.Name, app.Name)
			}
		}
		if homeOnly(app) {
			// Fermé hors de la maison : rien à ouvrir sur la box.
			continue
		}
		warnings = append(warnings, fmt.Sprintf("Pour vous en servir hors de la maison, ouvrez sur votre box internet le port %s %d vers ce Raspberry Pi (redirection de port).", strings.ToUpper(port.Protocol), port.Port))
	}
	return warnings, nil
}

func (m *Manager) environment(app marketplace.App, dir, name, address string, values map[string]string) (map[string]string, map[string]string, error) {
	known := map[string]bool{}
	for _, parameter := range app.Parameters {
		known[parameter.Name] = true
	}
	for key := range values {
		if !known[key] {
			return nil, nil, fmt.Errorf("%w : paramètre inconnu %s", ErrInvalid, key)
		}
	}
	previous, err := readEnv(filepath.Join(dir, envName))
	if err != nil {
		return nil, nil, err
	}
	env := map[string]string{"PI_INSTANCE": name, "PI_HOST": address}
	secrets := map[string]string{}
	for _, parameter := range app.Parameters {
		value := strings.TrimSpace(values[parameter.Name])
		switch {
		case value != "":
		case previous[parameter.Name] != "":
			value = previous[parameter.Name]
		case parameter.Generate:
			value = randomSecret()
			if parameter.Label != "" {
				secrets[parameter.Label] = value
			}
		case parameter.Default != "":
			value = parameter.Default
		default:
			return nil, nil, fmt.Errorf("%w : « %s » est obligatoire", ErrInvalid, parameter.Label)
		}
		if strings.ContainsAny(value, "'\r\n") {
			return nil, nil, fmt.Errorf("%w : « %s » ne doit contenir ni apostrophe ni retour à la ligne", ErrInvalid, parameter.Label)
		}
		env[parameter.Name] = value
	}
	m.hostingEnv(name, env)
	return env, secrets, nil
}

func (m *Manager) writeFiles(dir string, app marketplace.App, name, address string, env map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, composeName), app.Compose, 0o600); err != nil {
		return err
	}
	if m.wantsLocalCertificate(app) {
		if _, err := m.copyLocalCertificate(name); err != nil {
			return err
		}
	}
	if err := writeEnv(dir, env); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, overrideName), m.override(app, name, address), 0o600)
}

func writeEnv(dir string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var content strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&content, "%s='%s'\n", key, env[key])
	}
	return os.WriteFile(filepath.Join(dir, envName), []byte(content.String()), 0o600)
}

func (m *Manager) override(app marketplace.App, name, address string) map[string]any {
	if app.Port == 0 {
		// Pas de page web : pas de Traefik, les ports s'ouvrent sur le Pi.
		ports := make([]string, 0, len(app.HostPorts))
		for _, port := range app.HostPorts {
			ports = append(ports, fmt.Sprintf("%d:%d/%s", port.Port, port.Port, port.Protocol))
		}
		service := map[string]any{"ports": ports}
		if app.ID == WireGuardApp {
			service["volumes"] = []string{filepath.Join(m.serviceDir(name), wireGuardDir) + ":/config/wg_confs"}
		}
		return map[string]any{"services": map[string]any{app.Service: service}}
	}
	router := "traefik.http.routers.pi-" + name
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": Network,
		router + ".rule":         "Host(`" + address + "`)",
		router + ".entrypoints":  "websecure",
		router + ".tls":          "true",
		"traefik.http.services.pi-" + name + ".loadbalancer.server.port": strconv.Itoa(app.Port),
	}
	if m.usesLetsEncrypt() {
		labels[router+".tls.certresolver"] = "letsencrypt"
	}
	labels[router+".middlewares"] = routerMiddlewares(app.ID, app.Access)
	service := map[string]any{
		"labels":   labels,
		"networks": []string{"default", Network},
	}
	if app.ID == EtherpadApp {
		service["environment"] = etherpadEnvironment(app.Access)
	}
	if app.ID == "nextcloud" {
		service["volumes"] = []string{filepath.Join(m.Dir, "services") + ":" + nextcloudSitesMount}
	}
	services := map[string]any{app.Service: service}
	// Dossier des fichiers du site (Nextcloud), dans chaque service qui le lit.
	for _, mount := range app.Files {
		target, ok := services[mount.Service].(map[string]any)
		if !ok {
			target = map[string]any{}
			services[mount.Service] = target
		}
		volumes, _ := target["volumes"].([]string)
		target["volumes"] = append(volumes, filepath.Join(m.serviceDir(name), siteDir)+":"+mount.Path)
	}
	if app.ID == CameraApp {
		addCameras(services)
	}
	m.localCertificateOverride(app, name, services)
	doc := map[string]any{
		"services": services,
		"networks": map[string]any{Network: map[string]any{"external": true}},
	}
	// Hébergement (hosting.go, database.go) : sur la forme JSON du fichier.
	data, err := json.Marshal(doc)
	if err != nil {
		return doc
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		return doc
	}
	m.hostingOverride(name, app.ID, generic)
	return generic
}

func (m *Manager) usesLetsEncrypt() bool {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(m.CurrentDomain())), ".")
	if domain == "" || net.ParseIP(domain) != nil || !strings.Contains(domain, ".") {
		return false
	}
	for _, localSuffix := range []string{".local", ".localhost", ".test", ".invalid", ".example"} {
		if strings.HasSuffix(domain, localSuffix) {
			return false
		}
	}
	return true
}

func (m *Manager) List() ([]Instance, error) {
	paths, err := filepath.Glob(filepath.Join(m.Dir, "services", "*", stateName))
	if err != nil {
		return nil, err
	}
	instances := []Instance{}
	for _, path := range paths {
		var instance Instance
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &instance); err != nil {
			return nil, fmt.Errorf("%s : %w", path, err)
		}
		if !instance.NoWebPage && instance.Access == "" {
			instance.Access = AccessPublic
		}
		if instance.SiteFiles = m.siteFiles(instance.Name, instance.App); instance.SiteFiles {
			instance.Build = readBuild(filepath.Join(m.serviceDir(instance.Name), siteDir))
		}
		instances = append(instances, instance)
	}
	linked, cloud := m.linkedSites(instances)
	for i := range instances {
		if !instances[i].SiteFiles {
			continue
		}
		if linked[instances[i].Name] {
			instances[i].NextcloudFolder = NextcloudFolder(instances[i].Name)
		} else {
			instances[i].NextcloudPending = cloud
		}
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return instances, nil
}

func (m *Manager) Remove(ctx context.Context, name string, keepData bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir := m.serviceDir(name)
	if !marketplace.ValidLabel(name) {
		return ErrNotFound
	}
	if _, err := os.Stat(filepath.Join(dir, stateName)); err != nil {
		return fmt.Errorf("%w : %s", ErrNotFound, name)
	}
	if instance, err := m.instance(name); err == nil {
		// PostgreSQL partagé (database.go) : ses clients d'abord.
		if clients := m.databaseClients(name); instance.App == PostgresApp && len(clients) > 0 {
			return fmt.Errorf("%w : la base de %s sert aux services %s ; désinstallez-les d'abord", ErrInvalid, name, strings.Join(clients, ", "))
		}
		if !keepData {
			if err := m.dropDatabase(ctx, instance); err != nil {
				return fmt.Errorf("la base de %s n'a pas pu être effacée : %v", name, err)
			}
		}
	}
	args := []string{"down"}
	if !keepData {
		args = append(args, "--volumes")
	}
	if output, err := m.compose(ctx, name, args...); err != nil {
		return fmt.Errorf("le service %s n'a pas pu être arrêté : %s", name, trimOutput(output, err))
	}
	defer m.WakeNextcloud()
	var err error
	if keepData {
		err = os.Remove(filepath.Join(dir, stateName))
	} else {
		err = os.RemoveAll(dir)
	}
	if err == nil {
		// Le port d'un service « maison seulement » retiré n'est plus filtré.
		if applyErr := m.ApplyHomePorts(context.WithoutCancel(ctx)); applyErr != nil {
			log.Printf("%v", applyErr)
		}
	}
	return err
}

func readEnv(path string) (map[string]string, error) {
	values := map[string]string{}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[key] = strings.Trim(value, "'")
		}
	}
	return values, scanner.Err()
}

func writeJSON(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), mode)
}

func randomSecret() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	secret := make([]byte, 24)
	for i := range secret {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err)
		}
		secret[i] = alphabet[n.Int64()]
	}
	return string(secret)
}

func trimOutput(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if text == "" {
		return err.Error()
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.Join(lines, "\n")
}

func SystemMemoryMB() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kilobytes, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0
			}
			return kilobytes / 1024
		}
	}
	return 0
}
