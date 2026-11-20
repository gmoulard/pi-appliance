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
