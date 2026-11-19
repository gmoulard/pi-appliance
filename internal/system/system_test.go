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
	"runtime"
	"strings"
	"testing"
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
