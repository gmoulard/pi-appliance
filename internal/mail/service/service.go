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

// Package service : service d'envoi d'e-mails du projet (spécification 10.2),
// sur le Pi du projet. Il reçoit les demandes signées des appliances, écrit
// lui-même le texte du message et l'envoie par le relais SMTP du domaine
// pi-appliance.com. Il ne garde rien sur disque : ni adresse e-mail, ni
// contenu, ni adresse IP ; les compteurs des limites sont en mémoire.
package service

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/mail"
)

const maxBody = 4 << 10

// Limites d'envoi : un service ouvert à toute appliance ne doit pas servir à
// inonder une boîte e-mail, ni épuiser le quota du relais.
const (
	PerKeyPerHour       = 10
	PerKeyPerDay        = 30
	PerRecipientPerHour = 5
	PerRecipientPerDay  = 10
	PerIPPerHour        = 30
	PerDay              = 1000
)

// Sender envoie un message déjà écrit ; SMTP en est un, les tests le remplacent.
type Sender interface {
	Send(to string, message []byte) error
}

type Service struct {
	Sender Sender
	// From : expéditeur des messages.
	From string
	Now  func() time.Time
	Logf func(string, ...any)
	// BehindProxy : l'adresse du client est lue dans X-Real-IP, posé par le reverse proxy.
	BehindProxy bool

	limits limiter
	mu     sync.Mutex
	// seen : signatures déjà reçues ; une demande signée ne sert qu'une fois.
	seen map[string]time.Time
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/send", s.send)
	return mux
}

func (s *Service) send(w http.ResponseWriter, r *http.Request) {
	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(http.MaxBytesReader(w, r.Body, maxBody)); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "demande trop longue")
		return
	}
	now := s.now()
	key, err := addresses.Verify(r, buffer.Bytes(), now)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "demande non reconnue : vérifiez la date et l'heure de l'appliance")
		return
	}
	if !s.firstTime(r.Header.Get(addresses.HeaderSignature), now) {
		writeError(w, http.StatusConflict, "demande déjà reçue")
		return
	}
	var request mail.Request
	decoder := json.NewDecoder(bytes.NewReader(buffer.Bytes()))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		writeError(w, http.StatusBadRequest, "demande illisible")
		return
	}
	request, err = request.Check()
	if err != nil {
		writeError(w, http.StatusBadRequest, "demande refusée")
		return
	}
	recipient := strings.ToLower(request.To)
	checks := []struct {
		key    string
		length time.Duration
		max    int
	}{
		{"all", 24 * time.Hour, PerDay},
		{"ip:" + s.clientIP(r), time.Hour, PerIPPerHour},
		{"key:" + key, time.Hour, PerKeyPerHour},
		{"key-day:" + key, 24 * time.Hour, PerKeyPerDay},
		{"to:" + recipient, time.Hour, PerRecipientPerHour},
		{"to-day:" + recipient, 24 * time.Hour, PerRecipientPerDay},
	}
	for _, check := range checks {
		if !s.limits.peek(check.key, check.length, check.max, now) {
			writeError(w, http.StatusTooManyRequests, "trop de messages envoyés, réessayez plus tard")
			return
		}
	}
	for _, check := range checks {
		s.limits.allow(check.key, check.length, check.max, now)
	}
	message, err := Compose(s.From, request, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, "demande refusée")
		return
	}
	// NE PAS journaliser l'adresse, le lien ni l'erreur complète du relais, qui peut citer l'adresse.
	if err := s.Sender.Send(request.To, message); err != nil {
		s.logf("envoi d'un message %s : échec du relais", request.Kind)
		writeError(w, http.StatusBadGateway, "le message n'a pas pu être envoyé, réessayez plus tard")
		return
	}
	s.logf("message %s envoyé", request.Kind)
	w.WriteHeader(http.StatusAccepted)
}

// firstTime : la signature n'a pas déjà servi (pendant sa durée de validité).
func (s *Service) firstTime(signature string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]time.Time{}
	}
	for old, at := range s.seen {
		if now.Sub(at) > 2*addresses.MaxClockSkew {
			delete(s.seen, old)
		}
	}
	if _, ok := s.seen[signature]; ok {
		return false
	}
	s.seen[signature] = now
	return true
}

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

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(mail.ErrorBody{Error: message})
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

func (l *limiter) peek(key string, length time.Duration, max int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current(key, length, now).count < max
}

func (l *limiter) allow(key string, length time.Duration, max int, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.current(key, length, now)
	w.count++
	l.windows[key] = w
	if len(l.windows) > 10000 {
		for k, old := range l.windows {
			if now.Sub(old.start) >= 24*time.Hour {
				delete(l.windows, k)
			}
		}
	}
}
