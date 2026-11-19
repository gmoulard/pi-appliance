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

package metrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func value(v float64) *float64 { return &v }

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newHistory(t *testing.T) (*History, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	return &History{Path: filepath.Join(t.TempDir(), "history.json"), Now: c.now}, c
}

func TestRecordAggregatesByHour(t *testing.T) {
	h, c := newHistory(t)
	samples := []Sample{
		{CPUPercent: value(10), MemoryPercent: value(40), TemperatureC: value(45), DiskPercent: value(20), Received: 1 << 20, Sent: 0, Network: true},
		{CPUPercent: value(30), MemoryPercent: value(60), TemperatureC: value(55), DiskPercent: value(21), Received: 3 << 20, Sent: 1 << 20, Network: true},
		{CPUPercent: nil, TemperatureC: value(50), Received: 4 << 20, Sent: 2 << 20, Network: true},
	}
	for _, sample := range samples {
		if err := h.Record(sample); err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(10 * time.Minute)
	}
	if _, err := os.Stat(h.Path); err == nil {
		t.Fatal("historique écrit avant la fin de l'heure")
	}
	points := h.Points()
	if len(points) != 1 {
		t.Fatalf("%d points", len(points))
	}
	p := points[0]
	if *p.CPUPercent != 20 || *p.Memory != 50 || *p.TemperatureC != 55 || *p.Disk != 21 || *p.ReceivedMB != 3 || *p.SentMB != 2 {
		t.Fatalf("%+v", p)
	}
	c.t = c.t.Add(time.Hour)
	if err := h.Record(Sample{CPUPercent: value(5)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.Path); err != nil {
		t.Fatal("historique pas écrit à la fin de l'heure")
	}
	if points := h.Points(); len(points) != 2 || points[1].ReceivedMB != nil {
		t.Fatalf("%+v", points)
	}
}

func TestCounterReset(t *testing.T) {
	h, _ := newHistory(t)
	h.Record(Sample{Received: 5 << 20, Sent: 5 << 20, Network: true})
	h.Record(Sample{Received: 1 << 20, Sent: 2 << 20, Network: true})
	if p := h.Points()[0]; *p.ReceivedMB != 1 || *p.SentMB != 2 {
		t.Fatalf("%+v", p)
	}
}

func TestSaveLoadAndPrune(t *testing.T) {
	h, c := newHistory(t)
	for range 40 * 24 {
		h.Record(Sample{CPUPercent: value(1)})
		c.t = c.t.Add(time.Hour)
	}
	h.Record(Sample{CPUPercent: value(3)})
	if err := h.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := &History{Path: h.Path, Now: c.now}
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	points := loaded.Points()
	if len(points) != 30*24 {
		t.Fatalf("%d points gardés, %d attendus", len(points), 30*24)
	}
	if last := points[len(points)-1]; !last.Time.Equal(c.t) || *last.CPUPercent != 3 {
		t.Fatalf("heure en cours perdue : %+v", last)
	}
	// L'heure en cours reprend après un redémarrage de la console.
	loaded.Record(Sample{CPUPercent: value(5)})
	if last := loaded.Points()[len(loaded.Points())-1]; *last.CPUPercent != 4 {
		t.Fatalf("%+v", last)
	}
}

func TestLoadMissing(t *testing.T) {
	h, _ := newHistory(t)
	if err := h.Load(); err != nil || len(h.Points()) != 0 {
		t.Fatal(err)
	}
}

func TestRunSavesOnStop(t *testing.T) {
	h, _ := newHistory(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx, time.Hour, func() Sample { return Sample{CPUPercent: value(7)} }, t.Logf)
		close(done)
	}()
	for len(h.Points()) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if _, err := os.Stat(h.Path); err != nil {
		t.Fatal("historique pas écrit à l'arrêt")
	}
}
