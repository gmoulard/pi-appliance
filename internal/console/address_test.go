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
)

func TestAddressCheck(t *testing.T) {
	f := newDuckDNSFixture(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/address/check?name=maison", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	cookie := f.admin(t)
	response := f.do(http.MethodGet, "/pi-appliance/api/address/check?name=+Maison.pi-appliance.com", "", "", cookie)
	if check := decode[AddressCheck](t, response); response.Code != http.StatusOK || !check.Valid || check.Name != "maison" || check.Hostname != "maison.pi-appliance.com" {
		t.Fatalf("code %d : %+v", response.Code, check)
	}
	response = f.do(http.MethodGet, "/pi-appliance/api/address/check?name=www", "", "", cookie)
	if check := decode[AddressCheck](t, response); check.Valid || check.Reason != "ce nom est réservé au projet" || check.Hostname != "" {
		t.Fatalf("www : %+v", check)
	}
}
