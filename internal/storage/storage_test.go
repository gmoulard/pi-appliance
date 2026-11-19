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

package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fake struct {
	calls []string
	out   string
	err   error
}

func (f *fake) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	return []byte(f.out), f.err
}

func TestStatus(t *testing.T) {
	f := &fake{out: `{"disks": [{"device": "/dev/sda", "model": "T7", "sizeMB": 476940, "partitions": 1, "partition": "/dev/sda1", "fstype": "ext4", "label": "", "mounted": false}], "data": null, "move": {"state": "copying", "error": "", "message": "", "at": "2026-11-19T10:00:00Z"}}`}
	status, err := (&Client{Runner: f}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Disks) != 1 || *status.Disks[0].Partition != "/dev/sda1" || status.Data != nil || !status.Busy() {
		t.Fatalf("%+v", status)
	}
	f.out = `{"disks": [], "data": {"device": "/dev/sda1", "mountpoint": "/mnt/pi-appliance-data"}, "move": null}`
	if status, _ := (&Client{Runner: f}).Status(context.Background()); status.Data == nil || status.Busy() {
		t.Fatalf("%+v", status)
	}
}

func TestUseFormat(t *testing.T) {
	f := &fake{out: `{"ok": true, "started": true}`}
	c := &Client{Runner: f}
	for _, bad := range []string{"sda1", "/dev/../etc", "/dev/sda1 --force", ""} {
		if err := c.Use(context.Background(), bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%q : %v", bad, err)
		}
	}
	if err := c.Use(context.Background(), "/dev/sda1"); err != nil {
		t.Fatal(err)
	}
	if err := c.Format(context.Background(), "/dev/sdb"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls, ","); got != "use /dev/sda1,format /dev/sdb" {
		t.Fatal(got)
	}
}

func TestFailure(t *testing.T) {
	f := &fake{out: `{"ok": false, "error": "not-usb", "message": "Ce disque n'est pas un disque USB branché au Pi."}`, err: errors.New("exit status 1")}
	var failure Failure
	if err := (&Client{Runner: f}).Use(context.Background(), "/dev/mmcblk0p2"); !errors.As(err, &failure) || failure.Code != "not-usb" {
		t.Fatalf("%v", err)
	}
	f.out = "pas du JSON"
	if err := (&Client{Runner: f}).Use(context.Background(), "/dev/sda1"); err == nil || errors.As(err, &failure) {
		t.Fatalf("%v", err)
	}
}
