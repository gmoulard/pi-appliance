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

package console

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/storage"
)

type fakeStorage struct {
	calls []string
	fail  bool
}

func (f *fakeStorage) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if f.fail {
		return []byte(`{"ok": false, "error": "not-usb", "message": "Ce disque n'est pas un disque USB branché au Pi."}`), errors.New("exit status 1")
	}
	if args[0] == "status" {
		return []byte(`{"disks": [{"device": "/dev/sda", "model": "T7", "sizeMB": 476940, "partitions": 1, "partition": "/dev/sda1", "fstype": "ext4", "label": "", "mounted": false}], "data": null, "move": null}`), nil
	}
	return []byte(`{"ok": true, "started": true}`), nil
}

func newStorageFixture(t *testing.T) (fixture, *fakeStorage) {
	fake := &fakeStorage{}
	return newConfigFixture(t, func(_ string, config *Config) {
		config.Storage = &storage.Client{Runner: fake}
	}), fake
}

func TestStorage(t *testing.T) {
	f, fake := newStorageFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/storage", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	status := decode[storage.Status](t, f.do(http.MethodGet, "/pi-appliance/api/storage", "", "", cookie))
	if len(status.Disks) != 1 || status.Disks[0].Model != "T7" {
		t.Fatalf("%+v", status)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/storage/use", "application/json", `{"partition": "/dev/sda1"}`, cookie); response.Code != http.StatusAccepted {
		t.Fatalf("use : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/storage/format", "application/json", `{"disk": "/dev/sda"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("formatage sans accord : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/storage/format", "application/json", `{"disk": "/dev/sda", "erase": true}`, cookie); response.Code != http.StatusAccepted {
		t.Fatalf("format : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/storage/use", "application/json", `{"partition": "../etc"}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("partition invalide : code %d", response.Code)
	}
	if got := strings.Join(fake.calls, ","); got != "status,use /dev/sda1,format /dev/sda" {
		t.Fatalf("commandes %s", got)
	}
	fake.fail = true
	response := f.do(http.MethodPost, "/pi-appliance/api/storage/use", "application/json", `{"partition": "/dev/mmcblk0p2"}`, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "not-usb") {
		t.Fatalf("refus : code %d : %s", response.Code, response.Body.String())
	}
}

func TestStorageUnavailable(t *testing.T) {
	f := newConfigFixture(t, nil)
	if response := f.do(http.MethodGet, "/pi-appliance/api/storage", "", "", f.admin(t)); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", response.Code)
	}
}
