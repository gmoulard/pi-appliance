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

// Package backup sauvegarde l'appliance (spécification 9.1) : configuration
// et données des services, chaque nuit et à la demande, sur un disque USB
// autre que celui des données, chiffrées, avec rotation (7 quotidiennes,
// 4 hebdomadaires, 6 mensuelles) ; restauration d'un service à une date choisie.
//
// Une sauvegarde est un dossier <date>/ de la destination :
//
//	manifest.json        date, services, volumes, enveloppe de la clé (en clair)
//	config.enc           configuration de l'appliance (dossier des données de la console)
//	services/<nom>.enc   volumes Docker du service, copiés service arrêté
//
// Elle est écrite dans <date>.partial/, renommé à la fin : un dossier sans
// manifest.json n'est jamais une sauvegarde.
package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/system"
)

const (
	formatVersion = 1
	manifestName  = "manifest.json"
	configName    = "config.enc"
	partialSuffix = ".partial"
	idLayout      = "20060102-150405"
	// stateDir : dossier de la clé et des réglages, sous Dir ; jamais sauvegardé.
	stateDir = "backup"
)

// Kind : origine d'une sauvegarde.
type Kind string

const (
	Nightly Kind = "nightly"
	Manual  Kind = "manual"
	// BeforeRestore : copie d'un service juste avant sa restauration, gardée 7 jours.
	BeforeRestore Kind = "before-restore"
)

var (
	ErrBusy     = errors.New("une sauvegarde ou une restauration est déjà en cours")
	ErrNoDisk   = errors.New("aucun disque de sauvegarde branché")
	ErrNoSpace  = errors.New("pas assez de place sur le disque de sauvegarde")
	ErrNotFound = errors.New("sauvegarde introuvable")
	ErrInvalid  = errors.New("demande invalide")
)

var idPattern = regexp.MustCompile(`^\d{8}-\d{6}$`)

// Services : ce que les sauvegardes demandent aux services installés
// (*services.Manager convient).
type Services interface {
	List() ([]services.Instance, error)
	Volumes(ctx context.Context, name string) ([]services.Volume, error)
	CreateVolume(ctx context.Context, name, volume string) (string, error)
	Running(ctx context.Context, name string) bool
	Control(ctx context.Context, name string, action services.Action) error
}

// Settings : réglages des sauvegardes, modifiables dans la console.
type Settings struct {
	// Enabled : sauvegarde chaque nuit.
	Enabled bool `json:"enabled"`
	// Hour : heure de la sauvegarde de la nuit (0 à 23, heure du Pi).
	Hour    int `json:"hour"`
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

// DefaultSettings : une sauvegarde chaque nuit à 3 h ; 7 quotidiennes,
// 4 hebdomadaires, 6 mensuelles (spécification 9.1).
var DefaultSettings = Settings{Enabled: true, Hour: 3, Daily: 7, Weekly: 4, Monthly: 6}

func (s Settings) check() error {
	if s.Hour < 0 || s.Hour > 23 {
		return fmt.Errorf("%w : l'heure va de 0 à 23", ErrInvalid)
	}
	for _, n := range []int{s.Daily, s.Weekly, s.Monthly} {
		if n < 0 || n > 100 {
			return fmt.Errorf("%w : de 0 à 100 sauvegardes gardées", ErrInvalid)
		}
	}
	if s.Daily+s.Weekly+s.Monthly == 0 {
		return fmt.Errorf("%w : gardez au moins une sauvegarde", ErrInvalid)
	}
	return nil
}

// ServiceCopy : un service dans une sauvegarde.
type ServiceCopy struct {
	Name    string   `json:"name"`
	App     string   `json:"app"`
	AppName string   `json:"app_name"`
	Volumes []string `json:"volumes"`
	// Error : la copie de ce service a échoué ; il n'est pas restaurable.
	Error string `json:"error,omitempty"`
}

// Snapshot : le manifest d'une sauvegarde.
type Snapshot struct {
	Version  int           `json:"version"`
	ID       string        `json:"id"`
	Created  time.Time     `json:"created"`
	Kind     Kind          `json:"kind"`
	Host     string        `json:"host"`
	Services []ServiceCopy `json:"services"`
	SizeMB   int64         `json:"size_mb"`
	Envelope Envelope      `json:"envelope"`
}

func (s Snapshot) service(name string) (ServiceCopy, bool) {
	for _, c := range s.Services {
		if c.Name == name && c.Error == "" {
			return c, true
		}
	}
	return ServiceCopy{}, false
}

// Outcome : résultat de la dernière opération.
type Outcome struct {
	Action string `json:"action"`
	// Kind : sauvegarde de la nuit ou à la demande (alertes, spécification 8.6).
	Kind    Kind      `json:"kind,omitempty"`
	Service string    `json:"service,omitempty"`
	At      time.Time `json:"at"`
	OK      bool      `json:"ok"`
	Message string    `json:"message"`
}

// Manager fait les sauvegardes et les restaurations, une à la fois.
type Manager struct {
	// Dir : dossier des données de la console (configuration sauvegardée).
	Dir      string
	Services Services
	// Target rend le dossier des sauvegardes sur le disque ; ErrNoDisk sans disque.
	Target func() (string, error)
	// FreeMB : place libre du disque qui porte un dossier ; nil : internal/system.
	FreeMB func(dir string) (int64, bool)
	// Disks : disque USB de sauvegarde ; nil : non réglable ici.
	Disks *Disks
	Now   func() time.Time
	Logf  func(format string, args ...any)
	Host  string

	mu       sync.Mutex // une opération à la fois
	stateMu  sync.Mutex
	running  string
	attempts map[string]bool // nuits déjà tentées
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

func (m *Manager) keys() keyring {
	return keyring{dir: filepath.Join(m.Dir, stateDir)}
}

func (m *Manager) settingsPath() string { return filepath.Join(m.Dir, stateDir, "settings.json") }
func (m *Manager) outcomePath() string  { return filepath.Join(m.Dir, stateDir, "last.json") }

// Protect (re)chiffre la clé des sauvegardes par le mot de passe
// administrateur : à la création du compte et à chaque changement ou
// réinitialisation du mot de passe.
func (m *Manager) Protect(password string) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.keys().protect(password)
}

// ProtectIfMissing chiffre la clé à la connexion d'un administrateur si ce
// n'est pas encore fait (appliance d'avant les sauvegardes).
func (m *Manager) ProtectIfMissing(password string) error {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.keys().hasEnvelope() {
		return nil
	}
	return m.keys().protect(password)
}

// Settings rend les réglages, ceux par défaut si rien n'est enregistré.
func (m *Manager) Settings() Settings {
	settings := DefaultSettings
	data, err := os.ReadFile(m.settingsPath())
	if err == nil && json.Unmarshal(data, &settings) == nil && settings.check() == nil {
		return settings
	}
	return DefaultSettings
}

func (m *Manager) SetSettings(settings Settings) error {
	if err := settings.check(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return writeFile(m.settingsPath(), append(data, '\n'), 0o600)
}

// Last rend le résultat de la dernière opération, nil s'il n'y en a pas.
func (m *Manager) Last() *Outcome {
	data, err := os.ReadFile(m.outcomePath())
	if err != nil {
		return nil
	}
	var outcome Outcome
	if json.Unmarshal(data, &outcome) != nil {
		return nil
	}
	return &outcome
}

func (m *Manager) record(outcome Outcome) {
	outcome.At = m.now().UTC()
	data, err := json.MarshalIndent(outcome, "", "  ")
	if err == nil {
		err = writeFile(m.outcomePath(), append(data, '\n'), 0o600)
	}
	if err != nil {
		m.logf("sauvegardes : résultat non enregistré : %v", err)
	}
}

// Running dit quelle opération est en cours : "backup", "restore" ou "".
func (m *Manager) Running() string {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.running
}

func (m *Manager) begin(action string) bool {
	if !m.mu.TryLock() {
		return false
	}
	m.stateMu.Lock()
	m.running = action
	m.stateMu.Unlock()
	return true
}

func (m *Manager) end() {
	m.stateMu.Lock()
	m.running = ""
	m.stateMu.Unlock()
	m.mu.Unlock()
}

func (m *Manager) freeMB(dir string) (int64, bool) {
	if m.FreeMB != nil {
		return m.FreeMB(dir)
	}
	if gauge := system.DiskAt(dir); gauge != nil {
		return gauge.FreeMB, true
	}
	return 0, false
}

// Snapshots rend les sauvegardes de la destination, la plus récente d'abord.
func (m *Manager) Snapshots() ([]Snapshot, error) {
	root, err := m.Target()
	if err != nil {
		return nil, err
	}
	return readSnapshots(root)
}

func readSnapshots(root string) ([]Snapshot, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	snapshots := []Snapshot{}
	for _, entry := range entries {
		if !entry.IsDir() || !idPattern.MatchString(entry.Name()) {
			continue
		}
		snapshot, err := readManifest(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ID > snapshots[j].ID })
	return snapshots, nil
}

func readManifest(dir string) (Snapshot, error) {
	var snapshot Snapshot
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return snapshot, err
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, err
	}
	if snapshot.ID != filepath.Base(dir) {
		return snapshot, ErrCorrupt
	}
	return snapshot, nil
}

// Backup fait une sauvegarde complète : configuration et tous les services.
func (m *Manager) Backup(ctx context.Context, kind Kind) (Snapshot, error) {
	if !m.begin("backup") {
		return Snapshot{}, ErrBusy
	}
	defer m.end()
	snapshot, err := m.backup(ctx, kind, nil)
	outcome := Outcome{Action: "backup", Kind: kind, OK: err == nil, Message: "Sauvegarde terminée."}
	if err != nil {
		outcome.Message = err.Error()
		m.logf("sauvegarde : %v", err)
	} else if failed := failedServices(snapshot); len(failed) > 0 {
		outcome.OK = false
		outcome.Message = fmt.Sprintf("Sauvegarde terminée, sauf pour : %s.", strings.Join(failed, ", "))
	}
	m.record(outcome)
	return snapshot, err
}

func failedServices(s Snapshot) []string {
	var failed []string
	for _, c := range s.Services {
		if c.Error != "" {
			failed = append(failed, c.Name)
		}
	}
	return failed
}

// backup sauvegarde la configuration et les services ; only limite aux services
// nommés (copie avant restauration). Le verrou doit être pris.
func (m *Manager) backup(ctx context.Context, kind Kind, only []string) (Snapshot, error) {
	root, err := m.Target()
	if err != nil {
		return Snapshot{}, err
	}
	envelope, err := m.keys().envelope()
	if err != nil {
		return Snapshot{}, err
	}
	key, err := m.keys().key()
	if err != nil {
		return Snapshot{}, err
	}
	if keyID(key) != envelope.KeyID {
		return Snapshot{}, ErrNoKey
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Snapshot{}, err
	}
	removePartials(root)
	instances, err := m.Services.List()
	if err != nil {
		return Snapshot{}, err
	}
	if only != nil {
		instances = slices.DeleteFunc(instances, func(i services.Instance) bool { return !slices.Contains(only, i.Name) })
	}
	volumes := map[string][]services.Volume{}
	size := dirSize(m.Dir)
	for _, instance := range instances {
		list, err := m.Services.Volumes(ctx, instance.Name)
		if err != nil {
			m.logf("sauvegarde de %s : %v", instance.Name, err)
			continue
		}
		volumes[instance.Name] = list
		for _, v := range list {
			size += dirSize(v.Path)
		}
	}
	// Place : les données sans compression, plus 10 %.
	needMB := (size + size/10) >> 20
	if free, ok := m.freeMB(root); ok && needMB > free {
		return Snapshot{}, fmt.Errorf("%w : la sauvegarde demande %d Mo, il en reste %d. Branchez un disque plus grand, ou gardez moins de sauvegardes", ErrNoSpace, needMB, free)
	}

	now := m.now()
	id := now.Format(idLayout)
	for exists(filepath.Join(root, id)) {
		now = now.Add(time.Second)
		id = now.Format(idLayout)
	}
	partial := filepath.Join(root, id+partialSuffix)
	if err := os.MkdirAll(filepath.Join(partial, "services"), 0o700); err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Version: formatVersion, ID: id, Created: now.UTC(), Kind: kind, Host: m.Host, Services: []ServiceCopy{}, Envelope: envelope}
	fail := func(err error) (Snapshot, error) {
		os.RemoveAll(partial)
		return Snapshot{}, err
	}
	config := tree{dir: m.Dir, prefix: "config", skip: func(rel string) bool {
		return rel == stateDir || strings.HasPrefix(rel, stateDir+"/")
	}}
	if err := writeEncrypted(filepath.Join(partial, configName), key, []tree{config}); err != nil {
		return fail(fmt.Errorf("copie de la configuration : %w", err))
	}
	for _, instance := range instances {
		saved := ServiceCopy{Name: instance.Name, App: instance.App, AppName: instance.AppName, Volumes: []string{}}
		list, ok := volumes[instance.Name]
		if !ok {
			saved.Error = "données illisibles"
		} else if err := m.copyService(ctx, partial, key, instance.Name, list); err != nil {
			m.logf("sauvegarde de %s : %v", instance.Name, err)
			saved.Error = err.Error()
		} else {
			for _, v := range list {
				saved.Volumes = append(saved.Volumes, v.Name)
			}
		}
		snapshot.Services = append(snapshot.Services, saved)
	}
	snapshot.SizeMB = dirSize(partial) >> 20
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(partial, manifestName), append(data, '\n'), 0o600); err != nil {
		return fail(err)
	}
	if err := os.Rename(partial, filepath.Join(root, id)); err != nil {
		return fail(err)
	}
	m.rotate(root)
	return snapshot, nil
}

// copyService copie les volumes d'un service, arrêté pendant la copie pour
// des données cohérentes (une base de données en marche peut être à moitié écrite).
func (m *Manager) copyService(ctx context.Context, partial string, key []byte, name string, volumes []services.Volume) error {
	if len(volumes) == 0 {
		return nil
	}
	running := m.Services.Running(ctx, name)
	if running {
		if err := m.Services.Control(ctx, name, services.Stop); err != nil {
			return err
		}
	}
	trees := make([]tree, 0, len(volumes))
	for _, v := range volumes {
		trees = append(trees, tree{dir: v.Path, prefix: v.Name})
	}
	err := writeEncrypted(filepath.Join(partial, "services", name+".enc"), key, trees)
	if running {
		// Redémarré même si la copie a échoué, avec un contexte à part :
		// un service ne reste jamais arrêté par une sauvegarde.
		start, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
		defer cancel()
		if startErr := m.Services.Control(start, name, services.Start); startErr != nil {
			err = errors.Join(err, startErr)
		}
	}
	return err
}

func writeEncrypted(path string, key []byte, trees []tree) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	err = writeArchive(f, key, trees)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}

// removePartials efface les sauvegardes interrompues (Pi éteint, disque débranché).
func removePartials(root string) {
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if id, ok := strings.CutSuffix(entry.Name(), partialSuffix); ok && idPattern.MatchString(id) {
			os.RemoveAll(filepath.Join(root, entry.Name()))
		}
	}
}

// rotate efface les sauvegardes que les réglages ne gardent plus.
func (m *Manager) rotate(root string) {
	snapshots, err := readSnapshots(root)
	if err != nil {
		m.logf("rotation des sauvegardes : %v", err)
		return
	}
	for _, s := range expired(snapshots, m.Settings(), m.now()) {
		if err := os.RemoveAll(filepath.Join(root, s.ID)); err != nil {
			m.logf("rotation des sauvegardes : %v", err)
		}
	}
}

// expired : sauvegardes à effacer. On garde la plus récente de chacun des
// derniers jours, semaines et mois qui ont une sauvegarde (comme restic
// --keep-daily, --keep-weekly, --keep-monthly), toujours la plus récente, et
// les copies d'avant restauration pendant 7 jours.
func expired(snapshots []Snapshot, settings Settings, now time.Time) []Snapshot {
	sorted := slices.Clone(snapshots)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Created.After(sorted[j].Created) })
	keep := map[string]bool{}
	periods := []struct {
		count int
		key   func(time.Time) string
	}{
		{settings.Daily, func(t time.Time) string { return t.Format("2006-01-02") }},
		{settings.Weekly, func(t time.Time) string { y, w := t.ISOWeek(); return fmt.Sprintf("%d-%02d", y, w) }},
		{settings.Monthly, func(t time.Time) string { return t.Format("2006-01") }},
	}
	if i := slices.IndexFunc(sorted, func(s Snapshot) bool { return s.Kind != BeforeRestore }); i >= 0 {
		keep[sorted[i].ID] = true
	}
	for _, p := range periods {
		seen := map[string]bool{}
		for _, s := range sorted {
			if s.Kind == BeforeRestore {
				continue
			}
			k := p.key(s.Created.In(now.Location()))
			if seen[k] {
				continue
			}
			if len(seen) == p.count {
				break
			}
			seen[k] = true
			keep[s.ID] = true
		}
	}
	var gone []Snapshot
	for _, s := range sorted {
		if s.Kind == BeforeRestore {
			if now.Sub(s.Created) > 7*24*time.Hour {
				gone = append(gone, s)
			}
			continue
		}
		if !keep[s.ID] {
			gone = append(gone, s)
		}
	}
	return gone
}

// Restore remet un service tel qu'il était dans la sauvegarde id : ses
// réglages et ses données. Son état actuel est d'abord sauvegardé (copie
// d'avant restauration). password ouvre l'enveloppe d'une sauvegarde faite
// avec une autre clé (autre carte SD) ; vide sinon.
func (m *Manager) Restore(ctx context.Context, id, name, password string) (err error) {
	if !m.begin("restore") {
		return ErrBusy
	}
	defer m.end()
	defer func() {
		outcome := Outcome{Action: "restore", Service: name, OK: err == nil, Message: fmt.Sprintf("%s est restauré.", name)}
		if err != nil {
			outcome.Message = err.Error()
			m.logf("restauration de %s : %v", name, err)
		}
		m.record(outcome)
	}()
	if !idPattern.MatchString(id) || !validName(name) {
		return fmt.Errorf("%w : sauvegarde ou service inconnu", ErrInvalid)
	}
	root, err := m.Target()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, id)
	snapshot, err := readManifest(dir)
	if err != nil {
		return ErrNotFound
	}
	saved, ok := snapshot.service(name)
	if !ok {
		return fmt.Errorf("%w : %s n'est pas dans cette sauvegarde", ErrNotFound, name)
	}
	key, err := m.snapshotKey(snapshot, password)
	if err != nil {
		return err
	}
	installed, err := m.installed(name)
	if err != nil {
		return err
	}
	// Copie de l'état actuel, pour revenir en arrière.
	var before string
	if installed {
		safety, err := m.backup(ctx, BeforeRestore, []string{name})
		if err != nil {
			return fmt.Errorf("la copie de %s avant restauration a échoué, rien n'a été touché : %w", name, err)
		}
		before = safety.ID
	}
	if installed && m.Services.Running(ctx, name) {
		if err := m.Services.Control(ctx, name, services.Stop); err != nil {
			return err
		}
	}
	if err := m.restoreFiles(ctx, dir, key, name, saved); err != nil {
		if before != "" {
			return fmt.Errorf("%w. L'état d'avant est dans la sauvegarde du %s", err, before)
		}
		return err
	}
	return m.Services.Control(context.WithoutCancel(ctx), name, services.Start)
}

func validName(name string) bool {
	return name != "" && filepath.IsLocal(name) && !strings.ContainsAny(name, `/\`)
}

func (m *Manager) installed(name string) (bool, error) {
	instances, err := m.Services.List()
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(instances, func(i services.Instance) bool { return i.Name == name }), nil
}

// snapshotKey : la clé de l'appliance si c'est la même, sinon celle de
// l'enveloppe, ouverte par le mot de passe en vigueur lors de la sauvegarde.
func (m *Manager) snapshotKey(snapshot Snapshot, password string) ([]byte, error) {
	key, err := m.keys().key()
	if err == nil && keyID(key) == snapshot.Envelope.KeyID {
		return key, nil
	}
	if password == "" {
		return nil, ErrPassword
	}
	return snapshot.Envelope.Open(password)
}

// restoreFiles remet les réglages du service (son dossier dans la
// configuration) puis ses volumes ; le service est arrêté.
func (m *Manager) restoreFiles(ctx context.Context, dir string, key []byte, name string, saved ServiceCopy) error {
	serviceDir := filepath.Join(m.Dir, "services", name)
	staging := serviceDir + ".restore"
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	settings := newExtractor(staging)
	found := false
	err := readFile(filepath.Join(dir, configName), key, func(header *tar.Header, r io.Reader) error {
		rel, ok := entryPath(header.Name, "config/services/"+name)
		if !ok {
			return nil
		}
		found = true
		if rel == "." {
			return nil
		}
		return settings.add(header, rel, r)
	})
	if err == nil {
		err = settings.finish()
	}
	if err != nil {
		return fmt.Errorf("réglages de %s illisibles : %w", name, err)
	}
	if !found {
		return fmt.Errorf("%w : les réglages de %s manquent dans cette sauvegarde", ErrNotFound, name)
	}

	// Volumes : ceux qui existent sont vidés, les autres créés.
	paths := map[string]string{}
	if _, err := os.Stat(serviceDir); err == nil {
		current, err := m.Services.Volumes(ctx, name)
		if err != nil {
			return err
		}
		for _, v := range current {
			paths[v.Name] = v.Path
		}
	}
	// Les réglages d'abord : les volumes se créent avec le dossier du service en place.
	if err := os.RemoveAll(serviceDir); err != nil {
		return err
	}
	if err := os.Rename(staging, serviceDir); err != nil {
		return err
	}
	extractors := map[string]*extractor{}
	for _, volume := range saved.Volumes {
		path, ok := paths[volume]
		if !ok {
			created, err := m.Services.CreateVolume(ctx, name, volume)
			if err != nil {
				return err
			}
			path = created
		}
		if err := clearDir(path); err != nil {
			return fmt.Errorf("données de %s : %w", volume, err)
		}
		extractors[volume] = newExtractor(path)
	}
	if len(saved.Volumes) == 0 {
		return nil
	}
	err = readFile(filepath.Join(dir, "services", name+".enc"), key, func(header *tar.Header, r io.Reader) error {
		volume, _, _ := strings.Cut(strings.TrimSuffix(header.Name, "/"), "/")
		x, ok := extractors[volume]
		if !ok {
			return nil
		}
		rel, ok := entryPath(header.Name, volume)
		if !ok || rel == "." {
			return nil
		}
		return x.add(header, rel, r)
	})
	if err != nil {
		return fmt.Errorf("données de %s illisibles : %w", name, err)
	}
	for _, x := range extractors {
		if err := x.finish(); err != nil {
			return err
		}
	}
	return nil
}

func readFile(path string, key []byte, visit func(*tar.Header, io.Reader) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return readArchive(f, key, visit)
}

// Due dit si la sauvegarde de la nuit est à faire maintenant : à l'heure
// réglée, une fois par nuit.
func (m *Manager) due(now time.Time) bool {
	settings := m.Settings()
	if !settings.Enabled || now.Hour() != settings.Hour {
		return false
	}
	night := now.Format("2006-01-02")
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	if m.attempts[night] {
		return false
	}
	if m.attempts == nil {
		m.attempts = map[string]bool{}
	}
	m.attempts[night] = true
	return true
}

// Run fait la sauvegarde de chaque nuit jusqu'à la fin de ctx.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.due(m.now()) {
				m.Backup(ctx, Nightly)
			}
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// clearDir vide un dossier sans le supprimer (c'est le volume lui-même).
func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func dirSize(dir string) int64 {
	var size int64
	filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			if info, err := entry.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}
