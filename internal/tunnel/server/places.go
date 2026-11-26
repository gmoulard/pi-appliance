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

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/gmoulard/pi-appliance/internal/tunnel"
)

// place : appliance relayée. Ni adresse IP ni e-mail : la clé publique, le
// nom pi-appliance.com, des dates et les octets du mois.
type place struct {
	Name     string    `json:"name"`
	Since    time.Time `json:"since"`
	LastSeen time.Time `json:"last_seen"`
	// Month : mois des octets comptés, AAAA-MM en UTC.
	Month string `json:"month"`
	Bytes int64  `json:"bytes"`
}

// waiter : appliance sur la liste d'attente, dans l'ordre d'arrivée.
type waiter struct {
	Key      string    `json:"key"`
	Name     string    `json:"name"`
	Since    time.Time `json:"since"`
	LastSeen time.Time `json:"last_seen"`
}

type placesData struct {
	// Places : par clé publique de l'appliance.
	Places  map[string]*place `json:"places"`
	Waiting []waiter          `json:"waiting"`
}

// Places : nombre de places du relais et liste d'attente (spécification 7.3).
// Pas de verrou propre : Server le protège.
type Places struct {
	// Path : fichier des places (droits 600).
	Path string
	// Max : nombre maximal d'appliances relayées (TODO-1.45, à choisir par
	// l'utilisateur : pas de valeur par défaut).
	Max int
	// IdleRelease : place ou attente rendue après ce délai sans connexion
	// de l'appliance ; 0 : jamais (délai à décider, TODO-3.9).
	IdleRelease time.Duration
	// MonthlyBytes : octets par mois et par appliance, les deux sens.
	MonthlyBytes int64

	data  placesData
	dirty bool
}

// OpenPlaces lit le fichier des places, s'il existe.
func OpenPlaces(path string, max int) (*Places, error) {
	if max < 1 {
		return nil, errors.New("nombre maximal d'appliances relayées à choisir (option -max)")
	}
	p := &Places{Path: path, Max: max, MonthlyBytes: tunnel.MonthlyBytes, data: placesData{Places: map[string]*place{}}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &p.data); err != nil {
		return nil, fmt.Errorf("%s illisible : %w", path, err)
	}
	if p.data.Places == nil {
		p.data.Places = map[string]*place{}
	}
	return p, nil
}

func month(now time.Time) string { return now.UTC().Format("2006-01") }

// Admit : demande de place de l'appliance de clé key. Les premières de la
// liste d'attente prennent les places libres, dans l'ordre d'arrivée.
func (p *Places) Admit(key, name string, now time.Time) tunnel.Status {
	p.dirty = true
	p.promote(now)
	if pl := p.data.Places[key]; pl != nil {
		pl.Name, pl.LastSeen = name, now
		if pl.Month != month(now) {
			pl.Month, pl.Bytes = month(now), 0
		}
		if pl.Bytes >= p.MonthlyBytes {
			return tunnel.Status{State: tunnel.StateQuota, Message: "Votre Pi a utilisé les 20 Go du relais ce mois-ci ; il redevient joignable par le relais le mois prochain."}
		}
		return tunnel.Status{State: tunnel.StateReady}
	}
	index := slices.IndexFunc(p.data.Waiting, func(w waiter) bool { return w.Key == key })
	if index < 0 {
		p.data.Waiting = append(p.data.Waiting, waiter{Key: key, Name: name, Since: now, LastSeen: now})
		p.promote(now)
		if p.data.Places[key] != nil {
			return p.Admit(key, name, now)
		}
		index = len(p.data.Waiting) - 1
	}
	p.data.Waiting[index].Name, p.data.Waiting[index].LastSeen = name, now
	return tunnel.Status{State: tunnel.StateWaiting, Position: index + 1, Message: fmt.Sprintf("Le relais est complet. Votre Pi est n° %d sur la liste d'attente ; il sera relayé dès qu'une place se libère.", index+1)}
}

// promote donne les places libres aux premières appliances de la liste.
func (p *Places) promote(now time.Time) {
	for len(p.data.Places) < p.Max && len(p.data.Waiting) > 0 {
		w := p.data.Waiting[0]
		p.data.Waiting = p.data.Waiting[1:]
		p.data.Places[w.Key] = &place{Name: w.Name, Since: now, LastSeen: w.LastSeen, Month: month(now)}
		p.dirty = true
	}
}

// Release rend la place (ou la position d'attente) de l'appliance.
func (p *Places) Release(key string, now time.Time) {
	delete(p.data.Places, key)
	p.data.Waiting = slices.DeleteFunc(p.data.Waiting, func(w waiter) bool { return w.Key == key })
	p.dirty = true
	p.promote(now)
}

// Has : l'appliance a une place et n'a pas atteint son quota.
func (p *Places) Has(key string, now time.Time) bool {
	pl := p.data.Places[key]
	if pl == nil {
		return false
	}
	return pl.Month != month(now) || pl.Bytes < p.MonthlyBytes
}

// Count ajoute n octets au mois de l'appliance ; faux une fois le quota atteint.
func (p *Places) Count(key string, n int64, now time.Time) bool {
	pl := p.data.Places[key]
	if pl == nil {
		return false
	}
	if pl.Month != month(now) {
		pl.Month, pl.Bytes = month(now), 0
	}
	pl.Bytes += n
	p.dirty = true
	return pl.Bytes <= p.MonthlyBytes
}

// Expire rend les places et attentes muettes depuis IdleRelease.
func (p *Places) Expire(now time.Time) []string {
	if p.IdleRelease <= 0 {
		return nil
	}
	var gone []string
	for key, pl := range p.data.Places {
		if now.Sub(pl.LastSeen) > p.IdleRelease {
			delete(p.data.Places, key)
			gone = append(gone, key)
		}
	}
	before := len(p.data.Waiting)
	p.data.Waiting = slices.DeleteFunc(p.data.Waiting, func(w waiter) bool { return now.Sub(w.LastSeen) > p.IdleRelease })
	if len(gone) > 0 || len(p.data.Waiting) != before {
		p.dirty = true
		p.promote(now)
	}
	return gone
}

// Save écrit le fichier s'il a changé.
func (p *Places) Save() error {
	if !p.dirty {
		return nil
	}
	data, err := json.MarshalIndent(p.data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o700); err != nil {
		return err
	}
	temporary := p.Path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, p.Path); err != nil {
		return err
	}
	p.dirty = false
	return nil
}
