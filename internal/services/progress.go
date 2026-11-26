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

package services

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Étapes d'une installation (spécification 6.2), lues par la console.
const (
	StepPrepare  = "prepare"
	StepDownload = "download"
	StepCreate   = "create"
	StepStart    = "start"
	StepCheck    = "check"
	StepDone     = "done"
)

// progressKeep : durée pendant laquelle une installation finie reste lisible.
const progressKeep = 10 * time.Minute

// Progress : avancement de l'installation d'un service. Step reste sur l'étape
// en cours quand l'installation échoue ; Error dit alors pourquoi.
type Progress struct {
	Name     string    `json:"name"`
	AppName  string    `json:"app_name"`
	Step     string    `json:"step"`
	Images   int       `json:"images"`
	Image    int       `json:"image"`
	Received int64     `json:"received_bytes"`
	Total    int64     `json:"total_bytes"`
	Finished bool      `json:"finished"`
	Error    string    `json:"error,omitempty"`
	Updated  time.Time `json:"updated_at"`
	layers   map[string]*layer
}

type layer struct {
	current, total int64
}

// PullEvent : une ligne du téléchargement d'une image (API Docker, images/create).
type PullEvent struct {
	ID      string
	Status  string
	Current int64
	Total   int64
}

// ImagePuller télécharge les images en donnant l'avancement, couche par couche.
type ImagePuller interface {
	Present(ctx context.Context, image string) (bool, error)
	Pull(ctx context.Context, image string, report func(PullEvent)) error
}

// InstallProgress : avancement de l'installation en cours (ou finie depuis peu) de ce service.
func (m *Manager) InstallProgress(name string) (Progress, bool) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	progress, ok := m.progress[name]
	if !ok {
		return Progress{}, false
	}
	copied := *progress
	copied.layers = nil
	return copied, true
}

// forgetProgress oublie l'installation finie d'un service : une nouvelle
// demande ne doit pas montrer l'échec de la précédente.
func (m *Manager) forgetProgress(name string) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	if progress, ok := m.progress[name]; ok && progress.Finished {
		delete(m.progress, name)
	}
}

func (m *Manager) track(name, appName string) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	if m.progress == nil {
		m.progress = map[string]*Progress{}
	}
	for key, progress := range m.progress {
		if progress.Finished && time.Since(progress.Updated) > progressKeep {
			delete(m.progress, key)
		}
	}
	m.progress[name] = &Progress{Name: name, AppName: appName, Step: StepPrepare, Updated: time.Now().UTC(), layers: map[string]*layer{}}
}

func (m *Manager) updateProgress(name string, change func(*Progress)) {
	m.progressMu.Lock()
	defer m.progressMu.Unlock()
	if progress, ok := m.progress[name]; ok {
		change(progress)
		progress.Updated = time.Now().UTC()
	}
}

func (m *Manager) setStep(name, step string) {
	m.updateProgress(name, func(p *Progress) { p.Step = step })
}

func (m *Manager) finish(name string, err error) {
	m.updateProgress(name, func(p *Progress) {
		p.Finished = true
		if err != nil {
			p.Error = err.Error()
			return
		}
		p.Step = StepDone
	})
}

// pulled compte les octets reçus : les couches déjà sur le Pi ne comptent pas,
// une couche reçue compte pour sa taille entière (l'extraction a ses propres
// chiffres, qui ne sont pas un téléchargement).
func (p *Progress) pulled(event PullEvent) {
	if event.ID == "" {
		return
	}
	known, ok := p.layers[event.ID]
	switch event.Status {
	case "Pulling fs layer", "Waiting":
		if !ok {
			p.layers[event.ID] = &layer{}
		}
	case "Downloading":
		if !ok {
			known = &layer{}
			p.layers[event.ID] = known
		}
		known.total = max(known.total, event.Total)
		known.current = min(max(known.current, event.Current), known.total)
	case "Verifying Checksum", "Download complete", "Extracting", "Pull complete":
		if ok {
			known.current = known.total
		}
	default:
		return
	}
	p.Received, p.Total = 0, 0
	for _, l := range p.layers {
		p.Received += l.current
		p.Total += l.total
	}
}

// pullImages télécharge les images du service une à une avec l'avancement ;
// une image déjà sur le Pi n'est retéléchargée que si force est vrai. Sans
// ImagePuller, le téléchargement se fait au moment de la création, sans chiffres.
func (m *Manager) pullImages(ctx context.Context, name string, force bool) error {
	if m.Images == nil {
		return nil
	}
	output, err := m.compose(ctx, name, "config", "--images")
	if err != nil {
		return fmt.Errorf("liste des images illisible : %s", trimOutput(output, err))
	}
	var images []string
	for _, line := range strings.Split(string(output), "\n") {
		// NE PAS garder les lignes avec des espaces : ce sont les avertissements de Compose (sortie d'erreur mêlée).
		if image := strings.TrimSpace(line); image != "" && !strings.ContainsAny(image, " \t") && !slices.Contains(images, image) {
			images = append(images, image)
		}
	}
	m.updateProgress(name, func(p *Progress) { p.Images = len(images) })
	for i, image := range images {
		m.updateProgress(name, func(p *Progress) { p.Image = i + 1 })
		if !force {
			if present, err := m.Images.Present(ctx, image); err == nil && present {
				continue
			}
		}
		if err := m.Images.Pull(ctx, image, func(event PullEvent) {
			m.updateProgress(name, func(p *Progress) { p.pulled(event) })
		}); err != nil {
			return fmt.Errorf("%s : %w", image, err)
		}
	}
	return nil
}
