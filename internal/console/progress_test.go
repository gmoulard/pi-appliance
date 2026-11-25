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
	"net/http"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/services"
)

func TestInstallProgressAPI(t *testing.T) {
	f := newFixture(t)
	cookie := f.admin(t)
	path := "/pi-appliance/api/services/cloud/progress"
	if code := f.do(http.MethodGet, path, "", "", nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", code)
	}
	if code := f.do(http.MethodGet, path, "", "", cookie).Code; code != http.StatusNotFound {
		t.Fatalf("avant l'installation : code %d", code)
	}
	if response := f.install(`{"app": "nextcloud"}`, cookie); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	response := f.do(http.MethodGet, path, "", "", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("après l'installation : code %d", response.Code)
	}
	if progress := decode[services.Progress](t, response); progress.Step != services.StepDone || !progress.Finished || progress.Name != "cloud" {
		t.Fatalf("avancement %+v", progress)
	}
}
