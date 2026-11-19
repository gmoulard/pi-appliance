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

package system

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseMeminfo(t *testing.T) {
	gauge := parseMeminfo(strings.NewReader("MemTotal:        3884452 kB\nMemFree:          120000 kB\nMemAvailable:    2048000 kB\n"))
	if gauge == nil || *gauge != (Gauge{TotalMB: 3793, UsedMB: 1793, FreeMB: 2000}) {
		t.Fatalf("%+v", gauge)
	}
	if gauge := parseMeminfo(strings.NewReader("MemTotal: 1000 kB\n")); gauge != nil {
		t.Fatalf("sans MemAvailable : %+v", gauge)
	}
}

func TestNewGauge(t *testing.T) {
	if gauge := newGauge(100, 150); *gauge != (Gauge{TotalMB: 100, UsedMB: 0, FreeMB: 100}) {
		t.Fatalf("%+v", gauge)
	}
}

func TestRead(t *testing.T) {
	resources := Read(t.TempDir())
	if runtime.GOOS != "linux" {
		return
	}
	if resources.Memory == nil || resources.Disk == nil || resources.Disk.TotalMB <= 0 {
		t.Fatalf("%+v", resources)
	}
}

func TestMounted(t *testing.T) {
	mounts := "/dev/mmcblk0p2 / ext4 rw 0 0\n/dev/sda1 /mnt/pi-appliance-data ext4 rw,noatime 0 0\n"
	if !mounted(strings.NewReader(mounts), "/mnt/pi-appliance-data") || mounted(strings.NewReader(mounts), "/mnt") {
		t.Fatal("points de montage mal lus")
	}
	if MountedDisk(filepath.Join(t.TempDir(), "absent")) != nil {
		t.Fatal("jauge d'un dossier où rien n'est monté")
	}
}

func TestParseTemperature(t *testing.T) {
	if celsius := parseTemperature(strings.NewReader("48312\n")); celsius == nil || *celsius != 48.3 {
		t.Fatalf("%v", celsius)
	}
	for _, text := range []string{"", "chaud", "250000"} {
		if celsius := parseTemperature(strings.NewReader(text)); celsius != nil {
			t.Fatalf("%q : %v", text, *celsius)
		}
	}
}

func TestCPUPercent(t *testing.T) {
	before, ok := parseStat(strings.NewReader("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 2 3 4\n"))
	if !ok || before != (cpuTimes{busy: 200, total: 1000}) {
		t.Fatalf("%+v %v", before, ok)
	}
	after, _ := parseStat(strings.NewReader("cpu  200 0 150 1000 150 0 0 0 0 0\n"))
	if pct := percentBetween(before, after); pct == nil || *pct != 30 {
		t.Fatalf("%v", pct)
	}
	if pct := percentBetween(after, before); pct != nil {
		t.Fatalf("compteurs remis à zéro : %v", *pct)
	}
	if _, ok := parseStat(strings.NewReader("intr 1 2 3\n")); ok {
		t.Fatal("ligne cpu absente acceptée")
	}
}

func TestCPUMeter(t *testing.T) {
	dir := t.TempDir()
	statPath = filepath.Join(dir, "stat")
	firstCPUWait, cpuRefreshDelay = 0, 0
	t.Cleanup(func() { statPath, firstCPUWait, cpuRefreshDelay = "/proc/stat", 250*time.Millisecond, time.Second })
	write := func(line string) {
		if err := os.WriteFile(statPath, []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var meter cpuMeter
	write("cpu 0 0 0 100 0 0 0 0\n")
	if pct := meter.percent(); pct != nil {
		t.Fatalf("première lecture sans écart : %v", *pct)
	}
	write("cpu 50 0 0 150 0 0 0 0\n")
	if pct := meter.percent(); pct == nil || *pct != 50 {
		t.Fatalf("%v", pct)
	}
}

func TestParseNetDev(t *testing.T) {
	text := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 9000 10 0 0 0 0 0 0 9000 10 0 0 0 0 0 0
  eth0: 1000 10 0 0 0 0 0 0 2000 10 0 0 0 0 0 0
 wlan0: 300 10 0 0 0 0 0 0 400 10 0 0 0 0 0 0
docker0: 5000 10 0 0 0 0 0 0 5000 10 0 0 0 0 0 0
vethab12: 5000 10 0 0 0 0 0 0 5000 10 0 0 0 0 0 0
`
	received, sent, ok := parseNetDev(strings.NewReader(text))
	if !ok || received != 1300 || sent != 2400 {
		t.Fatalf("%d %d %v", received, sent, ok)
	}
}
