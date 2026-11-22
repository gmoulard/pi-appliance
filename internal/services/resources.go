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
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Resources est la mémoire et la place disque du Pi, en Mo. Une valeur nulle veut dire « inconnue ».
type Resources struct {
	MemoryTotalMB     int
	MemoryAvailableMB int
	DiskTotalMB       int
	DiskFreeMB        int
}

// Measure lit la mémoire dans /proc/meminfo et la place libre du système de fichiers
// qui contient dir (le dossier de données). Ce qui ne se lit pas reste à zéro.
func Measure(dir string) Resources {
	var resources Resources
	if file, err := os.Open("/proc/meminfo"); err == nil {
		resources.MemoryTotalMB, resources.MemoryAvailableMB, _ = ParseMeminfo(file)
		file.Close()
	}
	resources.DiskTotalMB, resources.DiskFreeMB, _ = DiskSpace(dir)
	return resources
}

// ParseMeminfo lit MemTotal et MemAvailable au format de /proc/meminfo, et les rend en Mo.
func ParseMeminfo(r io.Reader) (totalMB, availableMB int, err error) {
	scanner := bufio.NewScanner(r)
	found := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || (fields[0] != "MemTotal:" && fields[0] != "MemAvailable:") {
			continue
		}
		kilobytes, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, 0, fmt.Errorf("meminfo : %s illisible", fields[0])
		}
		if fields[0] == "MemTotal:" {
			totalMB = kilobytes / 1024
		} else {
			availableMB = kilobytes / 1024
		}
		found++
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	if found < 2 {
		return 0, 0, errors.New("meminfo : MemTotal ou MemAvailable absent")
	}
	return totalMB, availableMB, nil
}

// DiskSpace rend la taille et la place libre, en Mo, du système de fichiers qui contient path.
// Si path n'existe pas encore, il mesure le premier dossier parent qui existe.
func DiskSpace(path string) (totalMB, freeMB int, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return 0, 0, err
	}
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		parent := filepath.Dir(path)
		if parent == path {
			return 0, 0, fmt.Errorf("aucun dossier existant pour %s", path)
		}
		path = parent
	}
	return statfs(path)
}

// blocksMB convertit un nombre de blocs de blockSize octets en Mo.
func blocksMB(blocks, blockSize uint64) int {
	return int(blocks * blockSize / (1 << 20))
}

// GaugeKind distingue les deux jauges de la console.
type GaugeKind string

const (
	MemoryGauge GaugeKind = "memory"
	DiskGauge   GaugeKind = "disk"
)

// Gauge est une jauge de la console (spécification 6.2) : ce que le Pi a, ce qui est déjà utilisé,
// la part que prendrait un nouveau service et ce qui resterait, avec des phrases toutes faites.
type Gauge struct {
	Kind    GaugeKind `json:"kind"`
	Title   string    `json:"title"`
	TotalMB int       `json:"total_mb"`
	UsedMB  int       `json:"used_mb"`
	NeedMB  int       `json:"need_mb"`
	LeftMB  int       `json:"left_mb"`
	Color   Color     `json:"color"`
	Summary string    `json:"summary"`
	Advice  string    `json:"advice,omitempty"`
}

// Gauges construit les jauges de mémoire et de disque. appName vide : pas de nouveau service,
// la jauge montre seulement ce qui est utilisé et ce qui reste. Une mesure inconnue n'a pas de jauge.
func Gauges(resources Resources, appName string, memoryMB, diskMB int) []Gauge {
	gauges := []Gauge{}
	if resources.MemoryTotalMB > 0 {
		gauges = append(gauges, gauge(MemoryGauge, resources.MemoryTotalMB, resources.MemoryAvailableMB, appName, memoryMB))
	}
	if resources.DiskTotalMB > 0 {
		gauges = append(gauges, gauge(DiskGauge, resources.DiskTotalMB, resources.DiskFreeMB, appName, diskMB))
	}
	return gauges
}

// lowThreshold : en dessous de ce qui resterait, la jauge passe à l'orange.
// Un dixième de ce que le Pi a, entre 100 Mo et 1 Go pour la mémoire, entre 1 Go et 4 Go pour le disque.
func lowThreshold(kind GaugeKind, totalMB int) int {
	low, high := 100, 1024
	if kind == DiskGauge {
		low, high = 1024, 4096
	}
	return min(max(totalMB/10, low), high)
}

func gauge(kind GaugeKind, totalMB, freeMB int, appName string, needMB int) Gauge {
	freeMB = min(max(freeMB, 0), totalMB)
	if appName == "" {
		needMB = 0
	}
	g := Gauge{Kind: kind, TotalMB: totalMB, UsedMB: totalMB - freeMB, NeedMB: needMB, LeftMB: freeMB - needMB, Color: Green}
	word := "de mémoire"
	g.Title = "Mémoire"
	if kind == DiskGauge {
		word = "de place sur le disque"
		g.Title = "Disque"
	}
	switch {
	case g.LeftMB < 0:
		g.Color = Red
	case g.LeftMB < lowThreshold(kind, totalMB):
		g.Color = Orange
	}
	if appName == "" {
		g.Summary = fmt.Sprintf("Il reste %s %s, sur %s.", Size(g.LeftMB), word, Size(totalMB))
		if g.Color != Green {
			if kind == MemoryGauge {
				g.Advice = "Le Pi a peu de mémoire libre : les services risquent d'être lents. Arrêtez un service dont vous ne vous servez pas."
			} else {
				g.Advice = "Le disque est presque plein : une installation ou une mise à jour risque d'échouer. Désinstallez un service dont vous ne vous servez pas, ou branchez un disque USB."
			}
		}
		return g
	}
	switch {
	case g.Color == Red && kind == MemoryGauge:
		g.Summary = fmt.Sprintf("Il manquerait %s %s.", Size(-g.LeftMB), word)
		g.Advice = fmt.Sprintf("%s demande %s de mémoire et le Pi n'en a que %s de libre : il s'installera, mais lui et les autres services risquent d'être lents. Pour lui faire de la place, arrêtez un service dont vous ne vous servez pas.", appName, Size(needMB), Size(freeMB))
	case g.Color == Red:
		g.Summary = fmt.Sprintf("Il manquerait %s %s.", Size(-g.LeftMB), word)
		g.Advice = fmt.Sprintf("%s demande %s de place et le disque n'en a que %s de libre : l'installation risque d'échouer. Désinstallez un service dont vous ne vous servez pas, ou branchez un disque USB.", appName, Size(needMB), Size(freeMB))
	case g.Color == Orange && kind == MemoryGauge:
		g.Summary = fmt.Sprintf("Il resterait seulement %s %s.", Size(g.LeftMB), word)
		g.Advice = "C'est peu : le Pi risque de ralentir. Si c'est le cas, arrêtez un service dont vous ne vous servez pas."
	case g.Color == Orange:
		g.Summary = fmt.Sprintf("Il resterait seulement %s %s.", Size(g.LeftMB), word)
		g.Advice = "C'est peu : le disque sera bientôt plein. Pensez à brancher un disque USB."
	default:
		g.Summary = fmt.Sprintf("Il resterait %s %s.", Size(g.LeftMB), word)
	}
	return g
}

// Size écrit une taille en Mo comme on la dit : « 120 Mo », « 1,5 Go », « 32 Go ».
func Size(mb int) string {
	if mb < 1024 {
		return fmt.Sprintf("%d Mo", mb)
	}
	tenths := (mb*10 + 512) / 1024
	if tenths%10 == 0 || tenths >= 100 {
		return fmt.Sprintf("%d Go", (tenths+5)/10)
	}
	return fmt.Sprintf("%d,%d Go", tenths/10, tenths%10)
}
