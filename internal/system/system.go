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

// Package system lit l'état du Pi : mémoire, disque, charge du processeur,
// température et trafic réseau, pour les jauges et le tableau de bord de la
// console (spécification 5.2, 6.2).
package system

import (
	"bufio"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Gauge : ce que le Pi a en tout, ce qui est déjà pris, ce qui reste, en Mo.
type Gauge struct {
	TotalMB int64 `json:"total_mb"`
	UsedMB  int64 `json:"used_mb"`
	FreeMB  int64 `json:"free_mb"`
}

// Resources : état du Pi ; nil quand la valeur est inconnue (poste de
// développement, capteur absent).
type Resources struct {
	Memory *Gauge `json:"memory"`
	// Disk : la carte SD (disque du dossier des données de la console).
	Disk *Gauge `json:"disk"`
	// USBDisk : le disque USB des données des services, s'il y en a un (section 4).
	USBDisk *Gauge `json:"usb_disk"`
	// CPUPercent : part du temps où le processeur a travaillé depuis la
	// mesure précédente, tous cœurs confondus (la « charge » du tableau de bord).
	CPUPercent *float64 `json:"cpu_percent"`
	// TemperatureC : température de la puce, en degrés Celsius.
	TemperatureC *float64 `json:"temperature_c"`
}

// Fichiers du noyau lus par le paquet ; des variables pour les tests.
var (
	meminfoPath     = "/proc/meminfo"
	mountsPath      = "/proc/self/mounts"
	statPath        = "/proc/stat"
	thermalPath     = "/sys/class/thermal/thermal_zone0/temp"
	netDevPath      = "/proc/net/dev"
	firstCPUWait    = 250 * time.Millisecond
	cpuRefreshDelay = time.Second
)

// Read lit l'état du Pi ; le disque est celui qui porte dir.
func Read(dir string) Resources {
	var resources Resources
	if file, err := os.Open(meminfoPath); err == nil {
		resources.Memory = parseMeminfo(file)
		file.Close()
	}
	resources.Disk = disk(dir)
	resources.CPUPercent = cpu.percent()
	if file, err := os.Open(thermalPath); err == nil {
		resources.TemperatureC = parseTemperature(file)
		file.Close()
	}
	return resources
}

// DiskAt : jauge du disque qui porte dir ; nil si elle est inconnue.
func DiskAt(dir string) *Gauge {
	return disk(dir)
}

// MountedDisk : jauge du disque monté sur dir ; nil si rien n'y est monté
// (sinon ce serait la carte SD qui porte le dossier vide).
func MountedDisk(dir string) *Gauge {
	file, err := os.Open(mountsPath)
	if err != nil {
		return nil
	}
	defer file.Close()
	if !mounted(file, dir) {
		return nil
	}
	return disk(dir)
}

func mounted(r io.Reader, dir string) bool {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) >= 2 && fields[1] == dir {
			return true
		}
	}
	return false
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

// parseTemperature lit un capteur du noyau, en millièmes de degré.
func parseTemperature(r io.Reader) *float64 {
	data, err := io.ReadAll(io.LimitReader(r, 64))
	if err != nil {
		return nil
	}
	milli, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || milli < -40000 || milli > 150000 {
		return nil
	}
	celsius := math.Round(float64(milli)/100) / 10
	return &celsius
}

// cpuTimes : temps cumulés du processeur depuis le démarrage, en unités du noyau.
type cpuTimes struct {
	busy, total uint64
}

// parseStat lit la première ligne de /proc/stat : user nice system idle
// iowait irq softirq steal (guest est déjà compté dans user).
func parseStat(r io.Reader) (cpuTimes, bool) {
	scanner := bufio.NewScanner(r)
	if !scanner.Scan() {
		return cpuTimes{}, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuTimes{}, false
	}
	var times cpuTimes
	for i, field := range fields[1:min(len(fields), 9)] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuTimes{}, false
		}
		times.total += value
		if i != 3 && i != 4 { // idle, iowait
			times.busy += value
		}
	}
	return times, times.total > 0
}

func percentBetween(before, after cpuTimes) *float64 {
	if after.total <= before.total || after.busy < before.busy {
		return nil
	}
	percent := 100 * float64(after.busy-before.busy) / float64(after.total-before.total)
	percent = math.Round(min(percent, 100)*10) / 10
	return &percent
}

// cpuMeter garde la mesure précédente : la charge se calcule entre deux lectures.
type cpuMeter struct {
	mu   sync.Mutex
	last cpuTimes
	at   time.Time
	pct  *float64
}

var cpu cpuMeter

func readStat() (cpuTimes, bool) {
	file, err := os.Open(statPath)
	if err != nil {
		return cpuTimes{}, false
	}
	defer file.Close()
	return parseStat(file)
}

func (m *cpuMeter) percent() *float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, ok := readStat()
	if !ok {
		return nil
	}
	if m.at.IsZero() {
		// Première lecture : une courte attente donne une première mesure.
		m.last, m.at = now, time.Now()
		time.Sleep(firstCPUWait)
		if now, ok = readStat(); !ok {
			return nil
		}
	} else if time.Since(m.at) < cpuRefreshDelay && m.pct != nil {
		return m.pct
	}
	if pct := percentBetween(m.last, now); pct != nil {
		m.pct = pct
	}
	m.last, m.at = now, time.Now()
	return m.pct
}

// NetworkBytes : octets reçus et envoyés par le Pi depuis son démarrage, sur
// ses interfaces vers la maison (câble, Wi-Fi), sans le bouclage ni les
// réseaux internes de Docker.
func NetworkBytes() (received, sent uint64, ok bool) {
	file, err := os.Open(netDevPath)
	if err != nil {
		return 0, 0, false
	}
	defer file.Close()
	return parseNetDev(file)
}

var internalInterfaces = []string{"lo", "docker", "veth", "br-", "virbr", "tun", "wg"}

func parseNetDev(r io.Reader) (received, sent uint64, ok bool) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		name, counters, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		internal := false
		for _, prefix := range internalInterfaces {
			internal = internal || strings.HasPrefix(name, prefix)
		}
		fields := strings.Fields(counters)
		if internal || len(fields) < 9 {
			continue
		}
		rx, errRx := strconv.ParseUint(fields[0], 10, 64)
		tx, errTx := strconv.ParseUint(fields[8], 10, 64)
		if errRx != nil || errTx != nil {
			continue
		}
		received += rx
		sent += tx
		ok = true
	}
	return received, sent, ok
}
