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

// Package system lit la mémoire et le disque du Pi, pour les jauges de la
// console (spécification 6.2).
package system

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// Gauge : ce que le Pi a en tout, ce qui est déjà pris, ce qui reste, en Mo.
type Gauge struct {
	TotalMB int64 `json:"total_mb"`
	UsedMB  int64 `json:"used_mb"`
	FreeMB  int64 `json:"free_mb"`
}

// Resources : jauges de la mémoire et du disque ; nil quand la valeur est inconnue.
type Resources struct {
	Memory *Gauge `json:"memory"`
	Disk   *Gauge `json:"disk"`
}

// Read lit la mémoire (/proc/meminfo) et le disque qui porte dir.
func Read(dir string) Resources {
	var resources Resources
	if file, err := os.Open("/proc/meminfo"); err == nil {
		resources.Memory = parseMeminfo(file)
		file.Close()
	}
	resources.Disk = disk(dir)
	return resources
}

// parseMeminfo : la mémoire libre est MemAvailable (ce que le noyau peut
// rendre sans gêner), pas MemFree (qui ignore les caches).
func parseMeminfo(r io.Reader) *Gauge {
	values := map[string]int64{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		if kilobytes, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			values[strings.TrimSuffix(fields[0], ":")] = kilobytes
		}
	}
	total, ok := values["MemTotal"]
	available, okAvailable := values["MemAvailable"]
	if !ok || !okAvailable || total <= 0 {
		return nil
	}
	return newGauge(total/1024, available/1024)
}

func newGauge(totalMB, freeMB int64) *Gauge {
	freeMB = min(max(freeMB, 0), totalMB)
	return &Gauge{TotalMB: totalMB, UsedMB: totalMB - freeMB, FreeMB: freeMB}
}
