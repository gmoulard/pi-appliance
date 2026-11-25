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

// Package alerts : alertes par e-mail aux administrateurs (spécification 8.6,
// TODO-4.2), envoyées par le SMTP réglé sur le Pi (« Envoi des e-mails »).
//
// Chaque type d'alerte (disque presque plein, service en panne, sous-tension,
// sauvegarde de la nuit en échec, correctifs de sécurité, certificat qui
// expire) a sa vérification (checks.go), relancée toutes les 5 minutes. Pas de
// rafale : un e-mail par type et par jour au plus, quand un problème nouveau
// apparaît ; puis un e-mail « retour à la normale » quand tous les problèmes
// signalés de ce type ont disparu. Chaque type se coupe dans la console.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"
)

// Type : un type d'alerte.
type Type string

const (
	Disk        Type = "disk"
	Service     Type = "service"
	Power       Type = "power"
	Backup      Type = "backup"
	Updates     Type = "updates"
	Certificate Type = "certificate"
)

// Types : tous les types, dans l'ordre de la console.
var Types = []Type{Disk, Service, Power, Backup, Updates, Certificate}

const (
	// Every : temps entre deux vérifications.
	Every = 5 * time.Minute
	// minGap : temps minimal entre deux alertes d'un même type.
	minGap = 24 * time.Hour
	// serviceDelay : un service doit être arrêté (rouge) depuis ce temps avant
	// l'alerte : une installation, une mise à jour ou un redémarrage n'alerte pas.
	serviceDelay = 10 * time.Minute
)

// delay : temps pendant lequel un problème doit durer avant l'alerte.
func delay(t Type) time.Duration {
	if t == Service {
		return serviceDelay
	}
	return 0
}

// Problem : un problème vu par une vérification. Key le désigne (« sd »,
// nom du service, domaine, paquet…) ; Value le précise dans l'e-mail
// (« 93 % », date…). Ni l'un ni l'autre ne doit contenir de secret.
type Problem struct {
	Key   string
	Value string
}

// Check : une vérification ; ok faux quand l'état est inconnu pour l'instant
// (rien n'est alors changé pour ce type).
type Check func(ctx context.Context, now time.Time) (problems []Problem, ok bool)

// Recipient : un administrateur dont l'adresse est vérifiée.
type Recipient struct {
	Name  string
	Email string
	Lang  string
}

// Sender envoie un e-mail ; *mail.SMTP en est un (SendAlert).
type Sender interface {
	Configured() bool
	SendAlert(to, name, lang, subject, body string) error
}

// Monitor vérifie et envoie les alertes.
type Monitor struct {
	// Path : réglage et état, <données>/alerts.json (droits 600).
	Path   string
	Checks map[Type]Check
	// Recipients : administrateurs à prévenir.
	Recipients func() []Recipient
	Sender     Sender
	// Link : adresse de la console, donnée dans chaque e-mail.
	Link func() string
	Now  func() time.Time
	Logf func(string, ...any)

	mu     sync.Mutex
	state  state
	loaded bool
}

// state : ce qui est gardé dans alerts.json.
type state struct {
	// Disabled : types coupés par l'administrateur.
	Disabled []Type `json:"disabled,omitempty"`
	// Stopped : services arrêtés exprès dans la console : pas d'alerte.
	Stopped []string            `json:"stopped,omitempty"`
	Types   map[Type]*typeState `json:"types,omitempty"`
}

type typeState struct {
	// Seen : problèmes vus, et depuis quand.
	Seen map[string]time.Time `json:"seen,omitempty"`
	// Alerted : problèmes déjà signalés par e-mail.
	Alerted map[string]bool `json:"alerted,omitempty"`
	// SentAt : dernière alerte de ce type.
	SentAt time.Time `json:"sent_at,omitzero"`
	// Recovered : « retour à la normale » à envoyer.
	Recovered bool `json:"recovered,omitempty"`
}

// Setting : un type et s'il est actif, pour la console.
type Setting struct {
	Type    Type `json:"type"`
	Enabled bool `json:"enabled"`
}

// ErrUnknownType : type d'alerte inconnu.
var ErrUnknownType = errors.New("type d'alerte inconnu")

func (m *Monitor) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Monitor) logf(format string, args ...any) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

// load lit alerts.json une fois ; le verrou doit être pris.
func (m *Monitor) load() {
	if m.loaded {
		return
	}
	m.loaded = true
	data, err := os.ReadFile(m.Path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			m.logf("alertes : %v", err)
		}
		return
	}
	if err := json.Unmarshal(data, &m.state); err != nil {
		m.logf("alertes : %s illisible, il repart de zéro : %v", m.Path, err)
		m.state = state{}
	}
}

// save écrit alerts.json s'il a changé ; le verrou doit être pris.
func (m *Monitor) save(before []byte) {
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil || string(data) == string(before) || m.Path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		m.logf("alertes : %v", err)
		return
	}
	tmp := m.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		m.logf("alertes : %v", err)
		return
	}
	if err := os.Rename(tmp, m.Path); err != nil {
		m.logf("alertes : %v", err)
	}
}

func (m *Monitor) snapshot() []byte {
	data, _ := json.MarshalIndent(m.state, "", "  ")
	return data
}

// Settings : chaque type et s'il est actif.
func (m *Monitor) Settings() []Setting {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	settings := make([]Setting, 0, len(Types))
	for _, t := range Types {
		settings = append(settings, Setting{Type: t, Enabled: !slices.Contains(m.state.Disabled, t)})
	}
	return settings
}

// SetEnabled active ou coupe un type d'alerte.
func (m *Monitor) SetEnabled(t Type, enabled bool) error {
	if !slices.Contains(Types, t) {
		return ErrUnknownType
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	before := m.snapshot()
	m.state.Disabled = slices.DeleteFunc(m.state.Disabled, func(d Type) bool { return d == t })
	if !enabled {
		m.state.Disabled = append(m.state.Disabled, t)
		sort.Slice(m.state.Disabled, func(i, j int) bool { return m.state.Disabled[i] < m.state.Disabled[j] })
	}
	m.save(before)
	return nil
}

// SetStopped note qu'un service a été arrêté exprès (pas d'alerte) ou
// redémarré, mis à jour ou désinstallé.
func (m *Monitor) SetStopped(name string, stopped bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	before := m.snapshot()
	m.state.Stopped = slices.DeleteFunc(m.state.Stopped, func(s string) bool { return s == name })
	if stopped {
		m.state.Stopped = append(m.state.Stopped, name)
		sort.Strings(m.state.Stopped)
	}
	m.save(before)
}

// Stopped : le service a été arrêté exprès.
func (m *Monitor) Stopped(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	return slices.Contains(m.state.Stopped, name)
}

// Run vérifie toutes les every jusqu'à la fin de ctx ; la première fois
// après first (laisser la console et les services démarrer).
func (m *Monitor) Run(ctx context.Context, first, every time.Duration) {
	wait := first
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		m.Check(ctx)
		wait = every
	}
}

// Check fait une vérification de chaque type et envoie ce qu'il faut.
func (m *Monitor) Check(ctx context.Context) {
	now := m.now()
	// Les vérifications (Docker, scripts) se font hors du verrou.
	found := map[Type][]Problem{}
	known := map[Type]bool{}
	for _, t := range Types {
		check := m.Checks[t]
		if check == nil {
			continue
		}
		problems, ok := check(ctx, now)
		if ok {
			found[t], known[t] = problems, true
		}
	}
	var recipients []Recipient
	if m.Sender != nil && m.Sender.Configured() && m.Recipients != nil {
		recipients = m.Recipients()
	}
	link := ""
	if m.Link != nil {
		link = m.Link()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.load()
	before := m.snapshot()
	defer func() { m.save(before) }()
	if m.state.Types == nil {
		m.state.Types = map[Type]*typeState{}
	}
	for _, t := range Types {
		if !known[t] {
			continue
		}
		s := m.state.Types[t]
		if s == nil {
			s = &typeState{}
			m.state.Types[t] = s
		}
		m.step(t, s, found[t], now, recipients, link)
		if len(s.Seen) == 0 && len(s.Alerted) == 0 && !s.Recovered && s.SentAt.IsZero() {
			delete(m.state.Types, t)
		}
	}
}

// step : un type ; le verrou est pris.
func (m *Monitor) step(t Type, s *typeState, problems []Problem, now time.Time, recipients []Recipient, link string) {
	present := map[string]Problem{}
	for _, p := range problems {
		present[p.Key] = p
	}
	if s.Seen == nil {
		s.Seen = map[string]time.Time{}
	}
	for key := range s.Seen {
		if _, ok := present[key]; !ok {
			delete(s.Seen, key)
		}
	}
	for key := range present {
		if _, ok := s.Seen[key]; !ok {
			s.Seen[key] = now
		}
	}
	wasAlerted := len(s.Alerted) > 0
	for key := range s.Alerted {
		if _, ok := present[key]; !ok {
			delete(s.Alerted, key)
		}
	}
	if len(present) > 0 {
		s.Recovered = false
	} else if wasAlerted {
		s.Recovered = true
	}
	if slices.Contains(m.state.Disabled, t) {
		// Coupé : rien n'est envoyé, ni alerte ni retour à la normale.
		s.Recovered = false
		return
	}
	var ready []Problem
	fresh := false
	for key, p := range present {
		if now.Sub(s.Seen[key]) >= delay(t) {
			ready = append(ready, p)
			if !s.Alerted[key] {
				fresh = true
			}
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i].Key < ready[j].Key })
	if len(recipients) == 0 {
		return
	}
	if fresh && (s.SentAt.IsZero() || now.Sub(s.SentAt) >= minGap) {
		if m.send(t, false, ready, recipients, link) {
			s.SentAt = now
			if s.Alerted == nil {
				s.Alerted = map[string]bool{}
			}
			for _, p := range ready {
				s.Alerted[p.Key] = true
			}
		}
		return
	}
	if s.Recovered {
		if m.send(t, true, nil, recipients, link) {
			s.Recovered = false
		}
	}
}

// send envoie l'e-mail à chaque administrateur ; vrai si au moins un est parti.
func (m *Monitor) send(t Type, recovered bool, problems []Problem, recipients []Recipient, link string) bool {
	sent := false
	for _, r := range recipients {
		subject, body := Compose(t, recovered, problems, r.Name, link, r.Lang)
		if err := m.Sender.SendAlert(r.Email, r.Name, r.Lang, subject, body); err != nil {
			// NE PAS journaliser l'adresse : elle irait dans le diagnostic.
			m.logf("alertes : e-mail « %s » non envoyé à %s : %v", t, r.Name, err)
			continue
		}
		sent = true
	}
	if sent {
		what := "alerte"
		if recovered {
			what = "retour à la normale"
		}
		m.logf("alertes : %s « %s » envoyé aux administrateurs", what, t)
	}
	return sent
}

// String : pour les journaux.
func (p Problem) String() string { return fmt.Sprintf("%s=%s", p.Key, p.Value) }
