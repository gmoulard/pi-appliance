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

// Package metrics garde l'historique de l'état du Pi pour les graphiques sur
// 30 jours du tableau de bord (spécification 5.2). Les mesures, prises chaque
// minute, sont agrégées par heure en mémoire et écrites une fois par heure,
// pour ménager la carte SD (spécification 11).
package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gmoulard/pi-appliance/internal/system"
)

const (
	// Keep : durée gardée dans l'historique.
	Keep = 30 * 24 * time.Hour
	// Step : durée d'un point de l'historique.
	Step = time.Hour
)

// Sample : une mesure ; un pointeur nil est une valeur inconnue.
type Sample struct {
	CPUPercent    *float64
	MemoryPercent *float64
	TemperatureC  *float64
	DiskPercent   *float64
	// Compteurs réseau du noyau, cumulés depuis le démarrage du Pi.
	Received, Sent uint64
	Network        bool
}

// Point : une heure de l'historique. Processeur et mémoire en moyenne,
// température au plus haut, disque à la dernière mesure, réseau en total.
type Point struct {
	Time         time.Time `json:"time"`
	CPUPercent   *float64  `json:"cpu_percent,omitempty"`
	Memory       *float64  `json:"memory_percent,omitempty"`
	TemperatureC *float64  `json:"temperature_c,omitempty"`
	Disk         *float64  `json:"disk_percent,omitempty"`
	ReceivedMB   *float64  `json:"received_mb,omitempty"`
	SentMB       *float64  `json:"sent_mb,omitempty"`
}

// hour : l'heure en cours, pas encore écrite.
type hour struct {
	Start       time.Time `json:"start"`
	CPUSum      float64   `json:"cpu_sum"`
	CPUCount    int       `json:"cpu_count"`
	MemorySum   float64   `json:"memory_sum"`
	MemoryCount int       `json:"memory_count"`
	Temperature *float64  `json:"temperature,omitempty"`
	Disk        *float64  `json:"disk,omitempty"`
	Received    uint64    `json:"received"`
	Sent        uint64    `json:"sent"`
	Network     bool      `json:"network"`
}

type file struct {
	Points  []Point `json:"points"`
	Current *hour   `json:"current,omitempty"`
}

// History : l'historique, lu et écrit dans Path.
type History struct {
	Path string
	Now  func() time.Time

	mu       sync.Mutex
	points   []Point
	current  *hour
	lastRx   uint64
	lastTx   uint64
	lastNet  bool
	modified bool
}

func (h *History) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

// Load lit l'historique ; un fichier absent donne un historique vide.
func (h *History) Load() error {
	data, err := os.ReadFile(h.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var content file
	if err := json.Unmarshal(data, &content); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.points, h.current = content.Points, content.Current
	h.prune(h.now())
	return nil
}

// Save écrit l'historique, heure en cours comprise (arrêt de la console).
func (h *History) Save() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.save()
}

func (h *History) save() error {
	data, err := json.Marshal(file{Points: h.points, Current: h.current})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.Path), 0o700); err != nil {
		return err
	}
	temporary := h.Path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, h.Path); err != nil {
		return err
	}
	h.modified = false
	return nil
}

// Record ajoute une mesure ; à chaque heure révolue, l'historique est écrit.
func (h *History) Record(sample Sample) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	start := now.Truncate(Step)
	closed := false
	if h.current != nil && !h.current.Start.Equal(start) {
		h.points = append(h.points, h.current.point())
		h.current = nil
		closed = true
	}
	if h.current == nil {
		h.current = &hour{Start: start}
	}
	c := h.current
	if sample.CPUPercent != nil {
		c.CPUSum += *sample.CPUPercent
		c.CPUCount++
	}
	if sample.MemoryPercent != nil {
		c.MemorySum += *sample.MemoryPercent
		c.MemoryCount++
	}
	if sample.TemperatureC != nil && (c.Temperature == nil || *sample.TemperatureC > *c.Temperature) {
		c.Temperature = sample.TemperatureC
	}
	if sample.DiskPercent != nil {
		c.Disk = sample.DiskPercent
	}
	if sample.Network {
		if h.lastNet {
			c.Received += delta(h.lastRx, sample.Received)
			c.Sent += delta(h.lastTx, sample.Sent)
			c.Network = true
		}
		h.lastRx, h.lastTx = sample.Received, sample.Sent
	}
	h.lastNet = sample.Network
	h.modified = true
	if closed {
		h.prune(now)
		return h.save()
	}
	return nil
}

// delta : écart entre deux compteurs ; un compteur revenu à zéro (Pi
// redémarré, interface recréée) compte depuis zéro.
func delta(before, after uint64) uint64 {
	if after < before {
		return after
	}
	return after - before
}

func (h *History) prune(now time.Time) {
	limit := now.Add(-Keep)
	kept := h.points[:0]
	for _, point := range h.points {
		if point.Time.After(limit) {
			kept = append(kept, point)
		}
	}
	h.points = kept
	if h.current != nil && !h.current.Start.After(limit) {
		h.current = nil
	}
}

// Points renvoie l'historique, heure en cours comprise, du plus ancien au plus récent.
func (h *History) Points() []Point {
	h.mu.Lock()
	defer h.mu.Unlock()
	points := make([]Point, 0, len(h.points)+1)
	points = append(points, h.points...)
	if h.current != nil {
		points = append(points, h.current.point())
	}
	return points
}

// Run prend une mesure à chaque période, jusqu'à la fin de ctx, puis écrit l'historique.
func (h *History) Run(ctx context.Context, every time.Duration, read func() Sample, logf func(string, ...any)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := h.Record(read()); err != nil && logf != nil {
			logf("historique : %v", err)
		}
		select {
		case <-ctx.Done():
			if err := h.Save(); err != nil && logf != nil {
				logf("historique : %v", err)
			}
			return
		case <-ticker.C:
		}
	}
}

func (c *hour) point() Point {
	point := Point{Time: c.Start, TemperatureC: c.Temperature, Disk: c.Disk}
	if c.CPUCount > 0 {
		point.CPUPercent = rounded(c.CPUSum / float64(c.CPUCount))
	}
	if c.MemoryCount > 0 {
		point.Memory = rounded(c.MemorySum / float64(c.MemoryCount))
	}
	if c.Network {
		point.ReceivedMB = rounded(float64(c.Received) / (1 << 20))
		point.SentMB = rounded(float64(c.Sent) / (1 << 20))
	}
	return point
}

func rounded(value float64) *float64 {
	value = math.Round(value*10) / 10
	return &value
}

// Measure prend une mesure de l'état du Pi ; dir est le dossier des données.
func Measure(dir string) Sample {
	resources := system.Read(dir)
	sample := Sample{CPUPercent: resources.CPUPercent, TemperatureC: resources.TemperatureC}
	if gauge := resources.Memory; gauge != nil && gauge.TotalMB > 0 {
		sample.MemoryPercent = rounded(100 * float64(gauge.UsedMB) / float64(gauge.TotalMB))
	}
	if gauge := resources.Disk; gauge != nil && gauge.TotalMB > 0 {
		sample.DiskPercent = rounded(100 * float64(gauge.UsedMB) / float64(gauge.TotalMB))
	}
	sample.Received, sample.Sent, sample.Network = system.NetworkBytes()
	return sample
}
