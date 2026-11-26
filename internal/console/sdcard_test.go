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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/sdcard"
	"github.com/gmoulard/pi-appliance/internal/updates"
)

type noReleases struct{}

func (noReleases) List(context.Context) ([]updates.Release, error) { return nil, nil }

func TestSDCard(t *testing.T) {
	f := newConfigFixture(t, func(dir string, config *Config) {
		config.SDCard = &sdcard.Manager{
			Releases: noReleases{},
			Lsblk: func(context.Context) ([]byte, error) {
				return []byte(`{"blockdevices":[{"name":"/dev/sda","type":"disk","tran":"usb","rm":true,"size":31914983424,"model":"SD","vendor":"Generic"}]}`), nil
			},
			Fstab: dir + "/fstab",
		}
	})
	if response := f.do(http.MethodGet, "/pi-appliance/api/sdcard", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	status := decode[SDCardStatus](t, f.do(http.MethodGet, "/pi-appliance/api/sdcard", "", "", cookie))
	if len(status.Cards) != 1 || status.Cards[0].Model != "Generic SD" || status.Image != nil || status.Internet {
		t.Fatalf("%+v", status)
	}
	body := `{"device": "/dev/sda", "model": "Generic SD", "size": 31914983424, "erase": true}`
	if response := f.fromOutside(http.MethodPost, "/pi-appliance/api/sdcard/write", body, cookie); response.Code < 400 {
		t.Fatalf("depuis internet : code %d", response.Code)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/sdcard/write", "application/json", strings.Replace(body, `"erase": true`, `"erase": false`, 1), cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("sans accord : code %d", response.Code)
	}
	response := f.do(http.MethodPost, "/pi-appliance/api/sdcard/write", "application/json", body, cookie)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"no-image"`) {
		t.Fatalf("sans image : code %d : %s", response.Code, response.Body)
	}
	if response := f.do(http.MethodPost, "/pi-appliance/api/sdcard/cancel", "", "", cookie); response.Code != http.StatusConflict {
		t.Fatalf("annulation sans copie : code %d", response.Code)
	}
}

func TestSDCardUnavailable(t *testing.T) {
	f := newConfigFixture(t, nil)
	if response := f.do(http.MethodGet, "/pi-appliance/api/sdcard", "", "", f.admin(t)); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("code %d", response.Code)
	}
}

func TestSDCardWriteFromInternetRefused(t *testing.T) {
	c := &console{Config: Config{SDCard: &sdcard.Manager{Releases: noReleases{}}}}
	request := httptest.NewRequest(http.MethodPost, "/pi-appliance/api/sdcard/write", strings.NewReader(`{"device": "/dev/sda", "erase": true}`))
	request.RemoteAddr = "127.0.0.1:40000"
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	recorder := httptest.NewRecorder()
	c.sdcardWrite(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("code %d : %s", recorder.Code, recorder.Body)
	}
}
