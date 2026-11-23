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

// Package catalog tient le catalogue de la marketplace à jour (spécification
// 6.1) : le catalogue intégré au binaire, remplacé application par
// application par le catalogue publié dans le dépôt GitHub du projet
// (docs/static, écrit par cmd/marketplace-json), lu en ligne.
package catalog

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// DefaultURL : l'index du catalogue publié, lu directement dans le dépôt.
const DefaultURL = "https://raw.githubusercontent.com/gmoulard/pi-appliance/master/docs/static/pi-appliance-catalog.json"

const (
	maxIndex    = 64 << 10
	maxDocument = 8 << 20
	// Interval : délai entre deux lectures ; RetryInterval après un échec.
	Interval      = 6 * time.Hour
	RetryInterval = 15 * time.Minute
	// MinRefresh : délai minimal entre deux lectures demandées depuis la
	// console (bouton Marketplace), pour ne pas solliciter GitHub à chaque clic.
	MinRefresh = time.Minute
)

var documentName = regexp.MustCompile(`^pi-appliance-([0-9a-f]{32})\.json$`)

// Status : d'où vient le catalogue affiché.
type Status struct {
	// Online : le catalogue en ligne a été lu (maintenant ou avant, gardé sur le Pi).
	Online    bool       `json:"online"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// Error : dernière lecture en ligne ratée ; le catalogue précédent reste.
	Error string `json:"error,omitempty"`
	// Refused : applications du catalogue en ligne que cette console n'accepte
	// pas (manifeste qu'elle ne comprend pas, options dangereuses ajoutées).
	Refused []string `json:"refused,omitempty"`
}

// Source : le catalogue courant, sûr pour plusieurs requêtes à la fois.
type Source struct {
	Builtin marketplace.Catalog
	// URL de l'index du catalogue publié, en HTTPS ; vide : catalogue intégré seulement.
	URL string
	// CacheFile garde le dernier catalogue en ligne accepté, pour un Pi
	// redémarré sans internet ; vide : pas de copie.
	CacheFile string
	// Version de la console : la copie d'une autre version n'est pas reprise
	// (son catalogue intégré peut être plus récent).
	Version string
	Client  *http.Client
	Logf    func(string, ...any)

	mu      sync.RWMutex
	current *marketplace.Catalog
	status  Status
	// digest : MD5 du catalogue en ligne appliqué ; un index qui le nomme
	// encore ne fait pas retélécharger le catalogue.
	digest string

	// reading : une seule lecture en ligne à la fois ; lastRead : dernière
	// lecture, pour MinRefresh.
	reading  sync.Mutex
	lastRead time.Time
}

type cache struct {
	Version   string          `json:"version"`
	FetchedAt time.Time       `json:"fetched_at"`
	Document  json.RawMessage `json:"document"`
}

func (s *Source) catalog() marketplace.Catalog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current != nil {
		return *s.current
	}
	return s.Builtin
}

func (s *Source) Apps() []marketplace.App { return s.catalog().Apps() }

func (s *Source) Get(id string) (marketplace.App, bool) { return s.catalog().Get(id) }

func (s *Source) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := s.status
	status.Refused = append([]string(nil), s.status.Refused...)
	return status
}

func (s *Source) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Run reprend la copie gardée sur le Pi, puis lit le catalogue en ligne tout
// de suite et toutes les 6 heures (15 minutes après un échec).
func (s *Source) Run(ctx context.Context) {
	s.LoadCache()
	if s.URL == "" {
		return
	}
	for {
		wait := Interval
		if err := s.Refresh(ctx); err != nil {
			wait = RetryInterval
			s.logf("catalogue en ligne : %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// LoadCache reprend le dernier catalogue en ligne accepté.
func (s *Source) LoadCache() {
	if s.CacheFile == "" {
		return
	}
	data, err := os.ReadFile(s.CacheFile)
	if err != nil {
		return
	}
	var saved cache
	if err := json.Unmarshal(data, &saved); err != nil || saved.Version != s.Version {
		return
	}
	if err := s.apply(saved.Document, saved.FetchedAt); err != nil {
		s.logf("copie du catalogue en ligne ignorée : %v", err)
	}
}

// Refresh lit le catalogue en ligne et le prend s'il est valable ; sinon le
// catalogue précédent reste et l'erreur est notée dans Status.
func (s *Source) Refresh(ctx context.Context) error {
	s.reading.Lock()
	defer s.reading.Unlock()
	return s.refreshLocked(ctx)
}

// RefreshNow : lecture demandée depuis la console (bouton Marketplace). Rien
// n'est lu si la dernière lecture date de moins de MinRefresh. Changed dit si
// le catalogue a changé.
func (s *Source) RefreshNow(ctx context.Context) (changed bool, err error) {
	if s.URL == "" {
		return false, nil
	}
	s.reading.Lock()
	defer s.reading.Unlock()
	if !s.lastRead.IsZero() && time.Since(s.lastRead) < MinRefresh {
		return false, nil
	}
	before := s.currentDigest()
	err = s.refreshLocked(ctx)
	return err == nil && s.currentDigest() != before, err
}

func (s *Source) currentDigest() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.digest
}

func (s *Source) refreshLocked(ctx context.Context) error {
	s.lastRead = time.Now()
	err := s.refresh(ctx)
	if err != nil {
		s.mu.Lock()
		s.status.Error = err.Error()
		s.mu.Unlock()
	}
	return err
}

func (s *Source) refresh(ctx context.Context) error {
	index, err := url.Parse(s.URL)
	if err != nil || index.Scheme != "https" || index.Host == "" {
		return fmt.Errorf("adresse du catalogue invalide (HTTPS seulement) : %s", s.URL)
	}
	data, final, err := s.get(ctx, index, maxIndex)
	if err != nil {
		return err
	}
	var pointer struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(data, &pointer); err != nil {
		return fmt.Errorf("index du catalogue illisible : %w", err)
	}
	match := documentName.FindStringSubmatch(pointer.File)
	if match == nil {
		return fmt.Errorf("index du catalogue : nom de fichier invalide %q", pointer.File)
	}
	if match[1] == s.currentDigest() {
		// Catalogue inchangé : il est à jour, rien à retélécharger.
		now := time.Now().UTC()
		s.mu.Lock()
		s.status.UpdatedAt = &now
		s.status.Error = ""
		s.mu.Unlock()
		return nil
	}
	document, _, err := s.get(ctx, final.ResolveReference(&url.URL{Path: pointer.File}), maxDocument)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", md5.Sum(document)) != match[1] {
		return errors.New("catalogue abîmé : son contenu ne correspond pas à son nom")
	}
	now := time.Now().UTC()
	if err := s.apply(document, now); err != nil {
		return err
	}
	if s.CacheFile != "" {
		saved, err := json.Marshal(cache{Version: s.Version, FetchedAt: now, Document: document})
		if err == nil {
			err = writeFile(s.CacheFile, saved)
		}
		if err != nil {
			s.logf("copie du catalogue en ligne : %v", err)
		}
	}
	return nil
}

func (s *Source) get(ctx context.Context, target *url.URL, limit int64) ([]byte, *url.URL, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("User-Agent", "pi-appliance")
	response, err := client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("catalogue injoignable : %w", err)
	}
	defer response.Body.Close()
	if response.Request.URL.Scheme != "https" {
		return nil, nil, errors.New("catalogue redirigé hors de HTTPS")
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("catalogue : HTTP %d pour %s", response.StatusCode, target)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("catalogue : lecture interrompue : %w", err)
	}
	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("catalogue trop gros (%d octets au plus)", limit)
	}
	return data, response.Request.URL, nil
}

// apply : le catalogue intégré, où chaque application acceptée du catalogue
// en ligne prend la place de la version intégrée ou s'ajoute. Une application
// absente en ligne garde sa version intégrée.
func (s *Source) apply(document []byte, at time.Time) error {
	manifests, err := marketplace.ReadDocument(document)
	if err != nil {
		return err
	}
	apps := s.Builtin.Apps()
	byID := map[string]int{}
	for i, app := range apps {
		byID[app.ID] = i
	}
	var refused []string
	for _, manifest := range manifests {
		builtin, known := s.Builtin.Get(manifest.ID)
		if err := accept(manifest, builtin, known); err != nil {
			refused = append(refused, manifest.ID)
			s.logf("catalogue en ligne : %s refusé : %v", manifest.ID, err)
			continue
		}
		if i, ok := byID[manifest.ID]; ok {
			apps[i] = manifest.App
		} else {
			byID[manifest.ID] = len(apps)
			apps = append(apps, manifest.App)
		}
	}
	sort.Strings(refused)
	catalog := marketplace.NewCatalog(apps)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = &catalog
	s.digest = fmt.Sprintf("%x", md5.Sum(document))
	s.status = Status{Online: true, UpdatedAt: &at, Refused: refused}
	return nil
}

// accept : un manifeste en ligne n'apporte pas d'option dangereuse (accès à
// Docker, conteneur privilégié, dossier du Pi…) que sa version intégrée n'a
// pas ; une nouvelle application n'en a aucune.
func accept(manifest marketplace.Manifest, builtin marketplace.App, known bool) error {
	if manifest.Err != nil {
		return manifest.Err
	}
	_, risks, err := services.AnalyzeCompose(manifest.App.Compose)
	if err != nil {
		return err
	}
	var allowed []services.Risk
	if known {
		if _, allowed, err = services.AnalyzeCompose(builtin.Compose); err != nil {
			return err
		}
	}
	for _, risk := range risks {
		found := false
		for _, other := range allowed {
			found = found || reflect.DeepEqual(risk, other)
		}
		if !found {
			return fmt.Errorf("option dangereuse absente de la version intégrée : %s", risk.Message)
		}
	}
	return nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".catalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := io.Copy(temp, bytes.NewReader(data)); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
