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

// Package reception est le service du projet qui reçoit les statistiques
// anonymes des appliances (spécification 10.2). Il garde chaque envoi accepté
// avec son jour de réception, dans un fichier par jour, et rien d'autre.
//
// NE PAS lire ni enregistrer l'adresse IP (RemoteAddr, X-Forwarded-For,
// X-Real-Ip), l'heure exacte, ni aucun en-tête : rien ne doit permettre de
// relier un envoi à une appliance, à sa clé ou à l'adresse e-mail de son
// administrateur. Le serveur HTTP qui porte ce service ne journalise pas
// ses erreurs (elles contiennent l'adresse du client), voir cmd/pi-appliance-stats.
package reception

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/stats"
)

// MaxDayBytes : au-delà, les envois du jour sont refusés (disque du Pi du projet).
const MaxDayBytes = 64 << 20

// Entry : une ligne du fichier du jour.
type Entry struct {
	Day string `json:"day"`
	stats.Report
}

// Summary : totaux d'un jour, pour le propriétaire du projet.
type Summary struct {
	Day      string         `json:"day"`
	Reports  int            `json:"reports"`
	Versions map[string]int `json:"versions"`
	Models   map[string]int `json:"models"`
	Services map[string]int `json:"services"`
	Custom   int            `json:"custom_services"`
	Crashes  map[string]int `json:"crashes"`
}

// Store garde les envois dans Dir, un fichier AAAA-MM-JJ.jsonl par jour (UTC).
type Store struct {
	Dir string
	Now func() time.Time
	// Logf ne reçoit que des erreurs du disque, jamais rien de la requête.
	Logf func(string, ...any)

	mu sync.Mutex
}

var ErrFull = errors.New("trop d'envois aujourd'hui")

var dayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *Store) day() string {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	return now().UTC().Format(time.DateOnly)
}

// Add vérifie un envoi et l'ajoute au fichier du jour.
func (s *Store) Add(report stats.Report) error {
	if err := report.Check(); err != nil {
		return err
	}
	day := s.day()
	line, err := json.Marshal(Entry{Day: day, Report: report})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.Dir, day+".jsonl")
	if info, err := os.Stat(path); err == nil && info.Size()+int64(len(line)) >= MaxDayBytes {
		return ErrFull
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Summarize totalise les envois d'un jour (AAAA-MM-JJ).
func (s *Store) Summarize(day string) (Summary, error) {
	summary := Summary{Day: day, Versions: map[string]int{}, Models: map[string]int{}, Services: map[string]int{}, Crashes: map[string]int{}}
	if !dayPattern.MatchString(day) {
		return summary, fmt.Errorf("jour invalide : %q", day)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.Open(filepath.Join(s.Dir, day+".jsonl"))
	if errors.Is(err, fs.ErrNotExist) {
		return summary, nil
	}
	if err != nil {
		return summary, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, stats.MaxBytes*2), stats.MaxBytes*4)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		summary.Reports++
		summary.Versions[entry.Version]++
		model := entry.Model
		if model == "" {
			model = "inconnu"
		}
		summary.Models[model]++
		for _, app := range entry.Services {
			summary.Services[app]++
		}
		summary.Custom += entry.CustomServices
		for _, crash := range entry.Crashes {
			summary.Crashes[crash.Where] += crash.Count
		}
	}
	return summary, scanner.Err()
}

// Handler : POST /api/report reçoit un envoi, GET /api/health dit que le
// service tourne. Summary, si true, ajoute GET /api/summary?day=AAAA-MM-JJ :
// à n'exposer que sur le réseau du projet.
func Handler(store *Store, summary bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/report", func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "JSON attendu"})
			return
		}
		var report stats.Report
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, stats.MaxBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&report); err != nil || decoder.More() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "envoi illisible"})
			return
		}
		switch err := store.Add(report); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, stats.ErrInvalid):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrFull):
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		default:
			if store.Logf != nil {
				store.Logf("statistiques : %v", err)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "enregistrement impossible"})
		}
	})
	if summary {
		mux.HandleFunc("GET /api/summary", func(w http.ResponseWriter, r *http.Request) {
			day := r.URL.Query().Get("day")
			if day == "" {
				day = store.day()
			}
			result, err := store.Summarize(day)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, result)
		})
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		log.Printf("statistiques : %v", err)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body.Bytes())
}
