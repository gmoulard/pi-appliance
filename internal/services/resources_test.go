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
	"os"
	"strings"
	"testing"
)

const meminfo = `MemTotal:        3884404 kB
MemFree:          214820 kB
MemAvailable:    1536000 kB
Buffers:           88044 kB
`

func TestParseMeminfo(t *testing.T) {
	total, available, err := ParseMeminfo(strings.NewReader(meminfo))
	if err != nil || total != 3793 || available != 1500 {
		t.Fatalf("total %d, disponible %d, erreur %v", total, available, err)
	}
	if _, _, err := ParseMeminfo(strings.NewReader("MemTotal: 1024 kB\n")); err == nil {
		t.Fatal("MemAvailable absent accepté")
	}
	if _, _, err := ParseMeminfo(strings.NewReader("MemTotal: x kB\nMemAvailable: 1 kB\n")); err == nil {
		t.Fatal("valeur illisible acceptée")
	}
}

func TestDiskSpace(t *testing.T) {
	dir := t.TempDir()
	total, free, err := DiskSpace(dir)
	if err != nil || total <= 0 || free < 0 || free > total {
		t.Fatalf("total %d, libre %d, erreur %v", total, free, err)
	}
	// Dossier de données pas encore créé : on mesure son parent.
	missingTotal, _, err := DiskSpace(dir + "/pas/encore/cree")
	if err != nil || missingTotal != total {
		t.Fatalf("dossier absent : total %d, erreur %v", missingTotal, err)
	}
	if _, err := os.Stat(dir + "/pas"); err == nil {
		t.Fatal("DiskSpace a créé le dossier")
	}
	if got := blocksMB(262144, 4096); got != 1024 {
		t.Fatalf("blocs : %d Mo", got)
	}
}

func TestSize(t *testing.T) {
	for mb, want := range map[int]string{0: "0 Mo", 120: "120 Mo", 1023: "1023 Mo", 1024: "1 Go", 1536: "1,5 Go", 3793: "3,7 Go", 10240: "10 Go", 32768: "32 Go", 19456: "19 Go"} {
		if got := Size(mb); got != want {
			t.Errorf("Size(%d) = %q, attendu %q", mb, got, want)
		}
	}
}

func TestGauges(t *testing.T) {
	resources := Resources{MemoryTotalMB: 1024, MemoryAvailableMB: 500, DiskTotalMB: 16384, DiskFreeMB: 2048}
	at := func(gauges []Gauge, kind GaugeKind) Gauge {
		for _, g := range gauges {
			if g.Kind == kind {
				return g
			}
		}
		t.Fatalf("pas de jauge %s dans %+v", kind, gauges)
		return Gauge{}
	}

	memory := at(Gauges(resources, "Wiki", 128, 1024), MemoryGauge)
	if memory.UsedMB != 524 || memory.NeedMB != 128 || memory.LeftMB != 372 || memory.Color != Green || memory.Summary != "Il resterait 372 Mo de mémoire." || memory.Advice != "" {
		t.Fatalf("mémoire %+v", memory)
	}
	memory = at(Gauges(resources, "Etherpad", 450, 1024), MemoryGauge)
	if memory.LeftMB != 50 || memory.Color != Orange || !strings.Contains(memory.Summary, "seulement 50 Mo") || !strings.Contains(memory.Advice, "ralentir") {
		t.Fatalf("mémoire juste %+v", memory)
	}
	memory = at(Gauges(resources, "Etherpad", 600, 1024), MemoryGauge)
	if memory.LeftMB != -100 || memory.Color != Red || memory.Summary != "Il manquerait 100 Mo de mémoire." || !strings.Contains(memory.Advice, "lents") {
		t.Fatalf("mémoire insuffisante %+v", memory)
	}

	disk := at(Gauges(resources, "Wiki", 128, 1024), DiskGauge)
	if disk.LeftMB != 1024 || disk.Color != Orange || !strings.Contains(disk.Advice, "disque USB") {
		t.Fatalf("disque juste %+v", disk)
	}
	disk = at(Gauges(resources, "Wiki", 128, 3072), DiskGauge)
	if disk.Color != Red || disk.Summary != "Il manquerait 1 Go de place sur le disque." || !strings.Contains(disk.Advice, "échouer") || !strings.Contains(disk.Advice, "disque USB") {
		t.Fatalf("disque insuffisant %+v", disk)
	}

	// Sans nouveau service : la part demandée est ignorée.
	disk = at(Gauges(resources, "", 128, 3072), DiskGauge)
	if disk.NeedMB != 0 || disk.LeftMB != 2048 || disk.Color != Green || disk.Summary != "Il reste 2 Go de place sur le disque, sur 16 Go." || disk.Advice != "" {
		t.Fatalf("disque sans service %+v", disk)
	}
	resources.DiskFreeMB = 1536
	disk = at(Gauges(resources, "", 0, 0), DiskGauge)
	if disk.Color != Orange || disk.Summary != "Il reste 1,5 Go de place sur le disque, sur 16 Go." || !strings.Contains(disk.Advice, "presque plein") {
		t.Fatalf("disque sans service %+v", disk)
	}

	// Mesure inconnue : pas de jauge.
	if gauges := Gauges(Resources{DiskTotalMB: 100, DiskFreeMB: 50}, "", 0, 0); len(gauges) != 1 || gauges[0].Kind != DiskGauge {
		t.Fatalf("jauges %+v", gauges)
	}
}
