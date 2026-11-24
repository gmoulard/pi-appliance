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

package upnp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// Description des ouvertures, visible dans la page de la box.
const Description = "pi-appliance"

// DefaultLease : durée de chaque ouverture, renouvelée à mi-parcours : un Pi
// débranché ne laisse pas ses ports ouverts plus d'une heure.
const DefaultLease = time.Hour

// retryDelay : nouvel essai après un échec (box injoignable, refus).
const retryDelay = 5 * time.Minute

// Settings : accord de l'administrateur et ports ouverts par le Pi, gardés
// dans <données>/upnp.json.
type Settings struct {
	Enabled   bool      `json:"enabled"`
	ChangedBy string    `json:"changed_by,omitempty"`
	ChangedAt time.Time `json:"changed_at,omitzero"`
	// Opened : ports ouverts sur la box par le Pi, à refermer quand ils ne
	// servent plus ou quand l'administrateur retire son accord.
	Opened []Mapping `json:"opened,omitempty"`
	// LocalIP : adresse du Pi vers qui ils ont été ouverts.
	LocalIP string `json:"local_ip,omitempty"`
}

// PortState : état d'un port voulu.
type PortState struct {
	Mapping
	Open  bool   `json:"open"`
	Error string `json:"error,omitempty"`
}

// Status : état montré à l'administrateur.
type Status struct {
	Enabled     bool        `json:"enabled"`
	ChangedBy   string      `json:"changed_by,omitempty"`
	ChangedAt   time.Time   `json:"changed_at,omitzero"`
	Gateway     string      `json:"gateway,omitempty"`
	LocalIP     string      `json:"local_ip,omitempty"`
	ExternalIP  string      `json:"external_ip,omitempty"`
	Ports       []PortState `json:"ports"`
	LastAttempt time.Time   `json:"last_attempt,omitzero"`
	LastSuccess time.Time   `json:"last_success,omitzero"`
	NextAttempt time.Time   `json:"next_attempt,omitzero"`
	Error       string      `json:"error,omitempty"`
}

// Manager tient ouverts les ports voulus tant que l'administrateur l'accepte.
type Manager struct {
	Path string
	// Ports : ports à ouvrir (80 et 443, plus ceux des services sans page web
	// comme WireGuard).
	Ports func() []Mapping
	// Find : recherche de la box ; nil : Discoverer{}.Discover.
	Find  func(context.Context) (*Gateway, error)
	Lease time.Duration
	Now   func() time.Time
	Logf  func(string, ...any)

	mu       sync.Mutex
	run      sync.Mutex
	settings Settings
	status   Status
	wake     chan struct{}
}

// Open lit l'accord gardé, s'il existe.
func Open(path string) (*Manager, error) {
	m := &Manager{Path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m.settings); err != nil {
		return nil, fmt.Errorf("%s illisible : %w", path, err)
	}
	return m, nil
}

// Status : état actuel (sans chercher la box).
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.status
	status.Enabled = m.settings.Enabled
	status.ChangedBy = m.settings.ChangedBy
	status.ChangedAt = m.settings.ChangedAt
	if len(status.Ports) == 0 || !status.Enabled {
		status.Ports = nil
		for _, port := range m.wanted() {
			status.Ports = append(status.Ports, PortState{Mapping: port, Open: status.Enabled && slices.Contains(m.settings.Opened, port)})
		}
	}
	status.Ports = slices.Clone(status.Ports)
	if status.Ports == nil {
		status.Ports = []PortState{}
	}
	return status
}

func (m *Manager) wanted() []Mapping {
	if m.Ports == nil {
		return nil
	}
	return m.Ports()
}

// Enable enregistre l'accord de l'administrateur et ouvre les ports aussitôt.
func (m *Manager) Enable(ctx context.Context, by string) (Status, error) {
	m.mu.Lock()
	m.settings.Enabled = true
	m.settings.ChangedBy = by
	m.settings.ChangedAt = m.now()
	err := m.save()
	m.mu.Unlock()
	if err != nil {
		return m.Status(), err
	}
	m.Sync(ctx)
	m.WakeUp()
	return m.Status(), nil
}

// Disable retire l'accord et referme sur la box les ports ouverts par le Pi.
// Box injoignable : les ouvertures tombent d'elles-mêmes à la fin du bail.
func (m *Manager) Disable(ctx context.Context, by string) (Status, error) {
	m.run.Lock()
	defer m.run.Unlock()
	m.mu.Lock()
	opened := slices.Clone(m.settings.Opened)
	m.mu.Unlock()
	message := ""
	if len(opened) > 0 {
		gateway, err := m.find(ctx)
		if err != nil {
			message = "La box ne répond pas : les ports se refermeront d'eux-mêmes d'ici une heure."
		} else {
			for _, port := range opened {
				if err := gateway.Delete(ctx, port); err != nil && !isCode(err, CodeNoSuchEntry) {
					m.logf("upnp : fermeture de %s : %v", port, err)
					message = "La box n'a pas refermé tous les ports : ils se refermeront d'eux-mêmes d'ici une heure, ou retirez-les dans la page de la box."
				}
			}
		}
	}
	m.mu.Lock()
	m.settings = Settings{Enabled: false, ChangedBy: by, ChangedAt: m.now()}
	m.status = Status{Error: message}
	err := m.save()
	m.mu.Unlock()
	m.WakeUp()
	return m.Status(), err
}

// Probe cherche la box sans rien ouvrir : nom, adresse publique selon elle.
func (m *Manager) Probe(ctx context.Context) (*Gateway, string, error) {
	gateway, err := m.find(ctx)
	if err != nil {
		return nil, "", err
	}
	external, err := gateway.ExternalIP(ctx)
	return gateway, external, err
}

// Sync ouvre les ports voulus et referme ceux qui ne servent plus ; renvoie
// le délai avant le prochain passage.
func (m *Manager) Sync(ctx context.Context) time.Duration {
	m.run.Lock()
	defer m.run.Unlock()
	m.mu.Lock()
	settings := m.settings
	settings.Opened = slices.Clone(settings.Opened)
	m.mu.Unlock()
	lease := m.lease()
	if !settings.Enabled {
		return lease / 2
	}
	now := m.now()
	status := Status{LastAttempt: now}
	m.mu.Lock()
	status.LastSuccess = m.status.LastSuccess
	m.mu.Unlock()
	gateway, err := m.find(ctx)
	if err != nil {
		status.Error = Explain(err)
		for _, port := range m.wanted() {
			status.Ports = append(status.Ports, PortState{Mapping: port})
		}
		return m.finish(settings, status, retryDelay)
	}
	status.Gateway = gateway.Name
	status.LocalIP = gateway.LocalIP
	if external, err := gateway.ExternalIP(ctx); err == nil {
		status.ExternalIP = external
	}
	wanted := m.wanted()
	var opened []Mapping
	failed := false
	for _, port := range wanted {
		err := m.open(ctx, gateway, port, lease, settings.LocalIP)
		state := PortState{Mapping: port, Open: err == nil}
		if err != nil {
			failed = true
			state.Error = Explain(err)
			m.logf("upnp : ouverture de %s : %v", port, err)
		} else {
			opened = append(opened, port)
		}
		status.Ports = append(status.Ports, state)
	}
	for _, port := range settings.Opened {
		if slices.Contains(wanted, port) {
			continue
		}
		if err := gateway.Delete(ctx, port); err != nil && !isCode(err, CodeNoSuchEntry) {
			m.logf("upnp : fermeture de %s : %v", port, err)
			opened = append(opened, port)
		}
	}
	settings.Opened = opened
	settings.LocalIP = gateway.LocalIP
	next := lease / 2
	if failed {
		next = retryDelay
		status.Error = "La box n'a pas ouvert tous les ports : détails ci-dessous."
	} else {
		status.LastSuccess = now
	}
	return m.finish(settings, status, next)
}

func (m *Manager) finish(settings Settings, status Status, next time.Duration) time.Duration {
	status.NextAttempt = m.now().Add(next)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.settings.Enabled {
		// Accord retiré pendant le passage : Disable a déjà tout refermé.
		return next
	}
	settings.Enabled = m.settings.Enabled
	settings.ChangedBy = m.settings.ChangedBy
	settings.ChangedAt = m.settings.ChangedAt
	changed := !slices.Equal(settings.Opened, m.settings.Opened) || settings.LocalIP != m.settings.LocalIP
	m.settings = settings
	m.status = status
	if changed {
		if err := m.save(); err != nil {
			m.logf("upnp : %v", err)
		}
	}
	return next
}

// open ouvre un port ; box qui n'accepte que les ouvertures sans fin : sans
// bail ; port déjà donné au Pi (ancienne adresse comprise) : refait.
func (m *Manager) open(ctx context.Context, gateway *Gateway, port Mapping, lease time.Duration, previous string) error {
	err := gateway.Add(ctx, port, lease, Description)
	if isCode(err, CodeOnlyPermanent) {
		err = gateway.Add(ctx, port, 0, Description)
	}
	if !isCode(err, CodeConflict) {
		return err
	}
	owner, ownerErr := gateway.Owner(ctx, port)
	if ownerErr != nil || owner == "" || (owner != gateway.LocalIP && owner != previous) {
		return &Conflict{Port: port, Owner: owner}
	}
	if err := gateway.Delete(ctx, port); err != nil && !isCode(err, CodeNoSuchEntry) {
		return err
	}
	err = gateway.Add(ctx, port, lease, Description)
	if isCode(err, CodeOnlyPermanent) {
		err = gateway.Add(ctx, port, 0, Description)
	}
	return err
}

// Conflict : le port de la box est déjà donné à un autre appareil.
type Conflict struct {
	Port  Mapping
	Owner string
}

func (c *Conflict) Error() string {
	if c.Owner == "" {
		return fmt.Sprintf("Le port %d de la box est déjà donné à un autre appareil.", c.Port.Port)
	}
	return fmt.Sprintf("Le port %d de la box est déjà donné à un autre appareil (%s).", c.Port.Port, c.Owner)
}

// Explain : l'erreur en langage courant.
func Explain(err error) string {
	var conflict *Conflict
	var refused *Error
	switch {
	case errors.As(err, &conflict):
		return conflict.Error() + " Retirez cette redirection dans la page de la box, puis réessayez."
	case errors.Is(err, ErrNoGateway):
		return "La box ne répond pas à l'ouverture automatique (UPnP) : activez l'UPnP dans les réglages de la box, ou ouvrez les ports à la main en suivant le guide."
	case errors.As(err, &refused) && refused.Code == CodeNotAuthorized:
		return "La box refuse d'ouvrir des ports : activez l'UPnP dans ses réglages, ou ouvrez les ports à la main en suivant le guide."
	case errors.As(err, &refused):
		return fmt.Sprintf("La box a refusé d'ouvrir le port (code %d). Ouvrez-le à la main en suivant le guide.", refused.Code)
	}
	return "La box n'a pas répondu comme prévu : " + err.Error() + "."
}

func isCode(err error, code int) bool {
	var refused *Error
	return errors.As(err, &refused) && refused.Code == code
}

// WakeUp relance tout de suite le passage (accord donné, service installé).
func (m *Manager) WakeUp() {
	m.mu.Lock()
	wake := m.wake
	m.mu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

// Run renouvelle les ouvertures jusqu'à la fin de ctx.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.wake = make(chan struct{}, 1)
	wake := m.wake
	m.mu.Unlock()
	for {
		next := m.Sync(ctx)
		timer := time.NewTimer(next)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-wake:
			timer.Stop()
		}
	}
}

func (m *Manager) find(ctx context.Context) (*Gateway, error) {
	if m.Find != nil {
		return m.Find(ctx)
	}
	return Discoverer{}.Discover(ctx)
}

func (m *Manager) lease() time.Duration {
	if m.Lease > 0 {
		return m.Lease
	}
	return DefaultLease
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

// save écrit les réglages (m.mu tenu), droits 600.
func (m *Manager) save() error {
	if m.Path == "" {
		return nil
	}
	data, err := json.MarshalIndent(m.settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o700); err != nil {
		return err
	}
	temporary := m.Path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, m.Path)
}
