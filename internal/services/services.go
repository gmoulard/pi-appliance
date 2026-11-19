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
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const (
	Network      = "pi-appliance"
	composeName  = "compose.yaml"
	overrideName = "pi-appliance.override.json"
	envName      = ".env"
	stateName    = "instance.json"
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
}

type Result struct {
	Instance Instance          `json:"instance"`
	Warnings []string          `json:"warnings,omitempty"`
	Secrets  map[string]string `json:"secrets,omitempty"`
}

type Manager struct {
	Dir      string
	Domain   string
	Runner   Runner
	MemoryMB func() int
	// Copier copie les données d'un service avant sa mise à jour ; nil : « cp -a ».
	Copier Copier
	// FreeMB donne la place libre du disque qui porte un dossier ; nil : lue
	// par internal/system.
	FreeMB func(dir string) (int64, bool)
	mu     sync.Mutex
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if !marketplace.ValidLabel(name) {
		return Result{}, fmt.Errorf("%w : le nom « %s » ne convient pas, utilisez des lettres minuscules, des chiffres et des tirets", ErrInvalid, name)
	}
	dir := m.serviceDir(name)
	if _, err := os.Stat(filepath.Join(dir, stateName)); err == nil {
		return Result{}, fmt.Errorf("%w : un service s'appelle déjà « %s », choisissez un autre nom", ErrExists, name)
	}
	address := name + "." + m.Domain
	env, secrets, err := m.environment(app, dir, name, address, values)
	if err != nil {
		return Result{}, err
	}
	var warnings []string
	if m.MemoryMB != nil {
		if memory := m.MemoryMB(); memory > 0 && memory < app.MemoryMB {
			warnings = append(warnings, fmt.Sprintf("Ce Raspberry Pi a %d Mo de mémoire et %s en demande %d : le service risque d'être lent.", memory, app.Name, app.MemoryMB))
		}
	}
	if err := m.writeFiles(dir, app, name, address, env); err != nil {
		return Result{}, err
	}
	if _, err := m.Runner.Run(ctx, dir, "network", "inspect", Network); err != nil {
		if output, err := m.Runner.Run(ctx, dir, "network", "create", Network); err != nil {
			return Result{}, fmt.Errorf("préparation du réseau impossible : %s", trimOutput(output, err))
		}
	}
	if output, err := m.compose(ctx, name, "up", "--detach", "--wait"); err != nil {
		if _, downErr := m.compose(context.WithoutCancel(ctx), name, "down"); downErr != nil {
			warnings = append(warnings, "Le service n'a pas pu être arrêté proprement.")
		}
		return Result{Warnings: warnings}, fmt.Errorf("%s n'a pas pu démarrer : %s", app.Name, trimOutput(output, err))
	}
	instance := Instance{Name: name, App: app.ID, AppName: app.Name, Address: address, InstalledAt: time.Now().UTC()}
	if err := writeJSON(filepath.Join(dir, stateName), instance, 0o600); err != nil {
		return Result{}, err
	}
	return Result{Instance: instance, Warnings: warnings, Secrets: secrets}, nil
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
	return env, secrets, nil
}

func (m *Manager) writeFiles(dir string, app marketplace.App, name, address string, env map[string]string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, composeName), app.Compose, 0o600); err != nil {
		return err
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var content strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&content, "%s='%s'\n", key, env[key])
	}
	if err := os.WriteFile(filepath.Join(dir, envName), []byte(content.String()), 0o600); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, overrideName), m.override(app, name, address), 0o600)
}

func (m *Manager) override(app marketplace.App, name, address string) map[string]any {
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
	service := map[string]any{
		"labels":   labels,
		"networks": []string{"default", Network},
	}
	if app.ID == "site-web" {
		service["volumes"] = []string{filepath.Join(m.Dir, "services", name, "www") + ":/var/www/html"}
	}
	if app.ID == "nextcloud" {
		service["volumes"] = []string{filepath.Join(m.Dir, "services") + ":/srv/pi-appliance/services"}
	}
	return map[string]any{
		"services": map[string]any{app.Service: service},
		"networks": map[string]any{Network: map[string]any{"external": true}},
	}
}

func (m *Manager) usesLetsEncrypt() bool {
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(m.Domain)), ".")
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
		instances = append(instances, instance)
	}
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return instances, nil
}

// DeployZIP remplace le contenu du site par une archive sûre.
func (m *Manager) DeployZIP(ctx context.Context, name string, r io.Reader) error {
	_ = ctx
	m.mu.Lock()
	defer m.mu.Unlock()
	if !marketplace.ValidLabel(name) {
		return fmt.Errorf("%w : %s", ErrInvalid, name)
	}
	dir := m.serviceDir(name)
	data, err := os.ReadFile(filepath.Join(dir, stateName))
	if err != nil {
		return fmt.Errorf("%w : %s", ErrNotFound, name)
	}
	var instance Instance
	if err := json.Unmarshal(data, &instance); err != nil || instance.App != "site-web" {
		return fmt.Errorf("%w : %s", ErrInvalid, name)
	}

	temp, err := os.CreateTemp(m.Dir, "site-*.zip")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Close(); err != nil {
		return err
	}

	limited := io.LimitReader(r, 256<<20)
	zipData, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(zipData) == (256 << 20) {
		return fmt.Errorf("%w : archive trop volumineuse (256 Mo maximum)", ErrInvalid)
	}
	if err := os.WriteFile(tempName, zipData, 0o600); err != nil {
		return err
	}
	archive, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("%w : fichier ZIP illisible", ErrInvalid)
	}

	root := filepath.Join(dir, "www")
	staging, err := os.MkdirTemp(m.Dir, "site-deploy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	var extracted int64
	for _, file := range archive.File {
		name := filepath.Clean(filepath.FromSlash(file.Name))
		if name == "." || name == "" {
			continue
		}
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("%w : chemin interdit dans l'archive", ErrInvalid)
		}
		target := filepath.Join(staging, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w : liens symboliques interdits", ErrInvalid)
		}
		if file.UncompressedSize64 > 1<<30 || extracted+int64(file.UncompressedSize64) > 1<<30 {
			return fmt.Errorf("%w : contenu trop volumineux (1 Go maximum)", ErrInvalid)
		}
		extracted += int64(file.UncompressedSize64)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			src.Close()
			return err
		}
		_, copyErr := io.Copy(dst, src)
		closeErr := dst.Close()
		srcCloseErr := src.Close()
		if copyErr != nil || closeErr != nil || srcCloseErr != nil {
			return fmt.Errorf("archive illisible : %v", firstError(copyErr, closeErr, srcCloseErr))
		}
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	return os.Rename(staging, root)
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
	args := []string{"down"}
	if !keepData {
		args = append(args, "--volumes")
	}
	if output, err := m.compose(ctx, name, args...); err != nil {
		return fmt.Errorf("le service %s n'a pas pu être arrêté : %s", name, trimOutput(output, err))
	}
	if keepData {
		return os.Remove(filepath.Join(dir, stateName))
	}
	return os.RemoveAll(dir)
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

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
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
