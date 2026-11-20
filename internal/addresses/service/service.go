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

// Package service est le service d'adresses du projet (spécification 7.1,
// 10.2), sur le Pi du projet : il réserve les noms <nom>.pi-appliance.com,
// chacun lié à la clé d'une appliance, et écrit leurs alias dans Route 53.
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
)

// maxBody : taille maximale d'une demande.
const maxBody = 4 << 10

type entry struct {
	Key      string    `json:"key"`
	Target   string    `json:"target"`
	Reserved time.Time `json:"reserved"`
	LastSeen time.Time `json:"last_seen"`
}

type released struct {
	Key string    `json:"key"`
	At  time.Time `json:"at"`
}

// registry : seul fichier du service. Il ne contient ni adresse IP ni e-mail
// (spécification 10.2) : nom, clé publique, cible, dates.
type registry struct {
	Names    map[string]*entry   `json:"names"`
	Released map[string]released `json:"released"`
}

// Service : registre des adresses et API pour les appliances.
type Service struct {
	Path string
	DNS  DNS
	Now  func() time.Time
	Logf func(string, ...any)
	// BehindProxy : l'adresse du client est lue dans X-Real-IP, posé par le
	// reverse proxy du Pi du projet. Elle sert aux limites, en mémoire seulement.
	BehindProxy bool

	mu     sync.Mutex
	data   registry
	limits limiter
}

// Open lit le registre (absent : registre vide).
func Open(path string, dns DNS) (*Service, error) {
	s := &Service{Path: path, DNS: dns, data: registry{Names: map[string]*entry{}, Released: map[string]released{}}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		return nil, fmt.Errorf("%s illisible : %w", path, err)
	}
	if s.data.Names == nil {
		s.data.Names = map[string]*entry{}
	}
	if s.data.Released == nil {
		s.data.Released = map[string]released{}
	}
	return s, nil
}

// Handler : API du service (addresses/protocol.go).
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/names/{name}", s.check)
	mux.HandleFunc("GET /api/mine", s.signed(s.mine))
	mux.HandleFunc("POST /api/names", s.signed(s.reserve))
	mux.HandleFunc("PUT /api/names/{name}", s.signed(s.update))
	mux.HandleFunc("DELETE /api/names/{name}", s.signed(s.release))
	return mux
}

// Run libère chaque heure les adresses sans signe de vie, jusqu'à la fin de ctx.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		s.Expire(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Expire libère les adresses sans signe de vie depuis addresses.Expiry et
// oublie les noms libérés depuis plus de addresses.Cooldown.
func (s *Service) Expire(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	changed := false
	for name, e := range s.data.Names {
		if now.Sub(e.LastSeen) < addresses.Expiry {
			continue
		}
		if err := s.DNS.Remove(ctx, addresses.Hostname(name), e.Target); err != nil {
			s.logf("adresse %s sans signe de vie, non retirée : %v", name, err)
			continue
		}
		delete(s.data.Names, name)
		s.data.Released[name] = released{Key: e.Key, At: now}
		s.logf("adresse %s libérée : pas de signe de vie depuis le %s", name, e.LastSeen.Format(time.DateOnly))
		changed = true
	}
	for name, r := range s.data.Released {
		if now.Sub(r.At) >= addresses.Cooldown {
			delete(s.data.Released, name)
			changed = true
		}
	}
	if changed {
		if err := s.save(); err != nil {
			s.logf("registre : %v", err)
		}
	}
}

func (s *Service) check(w http.ResponseWriter, r *http.Request) {
	// Public : le site www.pi-appliance.com pose aussi la question (spécification 13).
	w.Header().Set("Access-Control-Allow-Origin", "*")
	now := s.now()
	if !s.limits.allow("check|"+s.clientIP(r), time.Minute, addresses.ChecksPerMinute, now) {
		writeError(w, http.StatusTooManyRequests, "trop de questions d'un coup, réessayez dans une minute")
		return
	}
	name := addresses.Normalize(r.PathValue("name"))
	result := addresses.Availability{Name: name}
	if err := addresses.Check(name); err != nil {
		result.Reason = strings.TrimPrefix(err.Error(), addresses.ErrInvalid.Error()+" : ")
		writeJSON(w, http.StatusOK, result)
		return
	}
	s.mu.Lock()
	reason := s.taken(name, "", now)
	s.mu.Unlock()
	result.Available = reason == ""
	result.Reason = reason
	if result.Available {
		result.Hostname = addresses.Hostname(name)
	}
	writeJSON(w, http.StatusOK, result)
}

// taken dit pourquoi name n'est pas libre pour la clé key ("" : libre). À appeler sous s.mu.
func (s *Service) taken(name, key string, now time.Time) string {
	if e, found := s.data.Names[name]; found && e.Key != key {
		return "ce nom est déjà pris"
	}
	if r, found := s.data.Released[name]; found && r.Key != key && now.Sub(r.At) < addresses.Cooldown {
		return "ce nom n'est pas libre pour l'instant"
	}
	return ""
}

type signedHandler func(w http.ResponseWriter, r *http.Request, key string, body []byte)

func (s *Service) signed(next signedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var buffer bytes.Buffer
		if _, err := buffer.ReadFrom(http.MaxBytesReader(w, r.Body, maxBody)); err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "demande trop longue")
			return
		}
		key, err := addresses.Verify(r, buffer.Bytes(), s.now())
		if err != nil {
			writeError(w, http.StatusUnauthorized, "demande non reconnue : vérifiez la date et l'heure de l'appliance")
			return
		}
		next(w, r, key, buffer.Bytes())
	}
}

// nameOf renvoie le nom réservé par key ("" : aucun). À appeler sous s.mu.
func (s *Service) nameOf(key string) string {
	for name, e := range s.data.Names {
		if e.Key == key {
			return name
		}
	}
	return ""
}

func (s *Service) mine(w http.ResponseWriter, _ *http.Request, key string, _ []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := s.nameOf(key)
	if name == "" {
		writeError(w, http.StatusNotFound, "aucune adresse pour cette appliance")
		return
	}
	writeJSON(w, http.StatusOK, record(name, s.data.Names[name]))
}

func (s *Service) reserve(w http.ResponseWriter, r *http.Request, key string, body []byte) {
	var request addresses.Request
	if !decode(w, body, &request) {
		return
	}
	name := addresses.Normalize(request.Name)
	if err := addresses.Check(name); err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), addresses.ErrInvalid.Error()+" : "))
		return
	}
	target, err := addresses.CheckTarget(request.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), addresses.ErrInvalid.Error()+" : "))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if mine := s.nameOf(key); mine != "" {
		writeError(w, http.StatusConflict, "cette appliance a déjà l'adresse "+addresses.Hostname(mine)+" : libérez-la d'abord")
		return
	}
	if reason := s.taken(name, key, now); reason != "" {
		writeError(w, http.StatusConflict, reason)
		return
	}
	ip := "ip|" + s.clientIP(r)
	if !s.limits.peek(ip, 24*time.Hour, addresses.ReservationsPerIP, now) {
		writeError(w, http.StatusTooManyRequests, "trop de réservations depuis votre connexion aujourd'hui, réessayez demain")
		return
	}
	if !s.limits.peek("day", 24*time.Hour, addresses.ReservationsPerDay, now) {
		writeError(w, http.StatusTooManyRequests, "trop de réservations aujourd'hui pour le service, réessayez demain")
		return
	}
	if err := s.DNS.Set(r.Context(), addresses.Hostname(name), target); err != nil {
		s.logf("réservation de %s : %v", name, err)
		writeError(w, http.StatusServiceUnavailable, "l'adresse ne peut pas être écrite pour l'instant, réessayez plus tard")
		return
	}
	s.limits.allow(ip, 24*time.Hour, addresses.ReservationsPerIP, now)
	s.limits.allow("day", 24*time.Hour, addresses.ReservationsPerDay, now)
	e := &entry{Key: key, Target: target, Reserved: now, LastSeen: now}
	s.data.Names[name] = e
	delete(s.data.Released, name)
	if err := s.save(); err != nil {
		s.logf("registre : %v", err)
	}
	s.logf("adresse %s réservée", name)
	writeJSON(w, http.StatusCreated, record(name, e))
}

// owned renvoie l'adresse name si elle appartient à key, sinon répond l'erreur. À appeler sous s.mu.
func (s *Service) owned(w http.ResponseWriter, name, key string) *entry {
	e, found := s.data.Names[name]
	if !found {
		writeError(w, http.StatusNotFound, "cette adresse n'est pas réservée")
		return nil
	}
	if e.Key != key {
		writeError(w, http.StatusForbidden, "cette adresse appartient à une autre appliance")
		return nil
	}
	return e
}

func (s *Service) update(w http.ResponseWriter, r *http.Request, key string, body []byte) {
	var request addresses.Request
	if !decode(w, body, &request) {
		return
	}
	target, err := addresses.CheckTarget(request.Target)
	if err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), addresses.ErrInvalid.Error()+" : "))
		return
	}
	name := addresses.Normalize(r.PathValue("name"))
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.owned(w, name, key)
	if e == nil {
		return
	}
	now := s.now()
	if target != e.Target {
		if !s.limits.allow("target|"+name, 24*time.Hour, addresses.TargetChangesPerDay, now) {
			writeError(w, http.StatusTooManyRequests, "l'adresse a changé trop souvent aujourd'hui, réessayez demain")
			return
		}
		if err := s.DNS.Set(r.Context(), addresses.Hostname(name), target); err != nil {
			s.logf("nouvelle cible de %s : %v", name, err)
			writeError(w, http.StatusServiceUnavailable, "l'adresse ne peut pas être écrite pour l'instant, réessayez plus tard")
			return
		}
		e.Target = target
	}
	e.LastSeen = now
	if err := s.save(); err != nil {
		s.logf("registre : %v", err)
	}
	writeJSON(w, http.StatusOK, record(name, e))
}

func (s *Service) release(w http.ResponseWriter, r *http.Request, key string, _ []byte) {
	name := addresses.Normalize(r.PathValue("name"))
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.owned(w, name, key)
	if e == nil {
		return
	}
	if err := s.DNS.Remove(r.Context(), addresses.Hostname(name), e.Target); err != nil {
		s.logf("libération de %s : %v", name, err)
		writeError(w, http.StatusServiceUnavailable, "l'adresse ne peut pas être retirée pour l'instant, réessayez plus tard")
		return
	}
	delete(s.data.Names, name)
	s.data.Released[name] = released{Key: key, At: s.now()}
	if err := s.save(); err != nil {
		s.logf("registre : %v", err)
	}
	s.logf("adresse %s libérée par son appliance", name)
	w.WriteHeader(http.StatusNoContent)
}

func record(name string, e *entry) addresses.Record {
	return addresses.Record{Name: name, Hostname: addresses.Hostname(name), Target: e.Target, Reserved: e.Reserved, LastSeen: e.LastSeen}
}

// save écrit le registre (droits 600) en remplaçant l'ancien d'un coup. À appeler sous s.mu.
func (s *Service) save() error {
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	temporary := s.Path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.Path)
}

// clientIP : adresse du client pour les limites, jamais écrite ni journalisée.
func (s *Service) clientIP(r *http.Request) string {
	if s.BehindProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func decode(w http.ResponseWriter, body []byte, value any) bool {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "demande illisible")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, addresses.ErrorBody{Error: message})
}

// limiter compte des événements par clé sur une fenêtre fixe, en mémoire seulement.
type limiter struct {
	mu      sync.Mutex
	windows map[string]window
}

type window struct {
	start time.Time
	count int
}

func (l *limiter) current(key string, length time.Duration, now time.Time) window {
	if l.windows == nil {
		l.windows = map[string]window{}
	}
	w := l.windows[key]
	if now.Sub(w.start) >= length {
		w = window{start: now}
	}
	return w
}

// peek dit si un événement de plus resterait sous max, sans le compter.
func (l *limiter) peek(key string, length time.Duration, max int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current(key, length, now).count < max
}

// allow compte un événement et dit s'il reste sous max.
func (l *limiter) allow(key string, length time.Duration, max int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.current(key, length, now)
	if w.count >= max {
		l.windows[key] = w
		return false
	}
	w.count++
	l.windows[key] = w
	// Les fenêtres finies sont oubliées de temps en temps : pas d'adresse IP gardée plus que nécessaire.
	if len(l.windows) > 10000 {
		for k, old := range l.windows {
			if now.Sub(old.start) >= 24*time.Hour {
				delete(l.windows, k)
			}
		}
	}
	return true
}
