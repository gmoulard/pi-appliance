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

package addresses

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "address-key")
	key, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadOrCreateKey(path)
	if err != nil || again.Public() != key.Public() {
		t.Fatalf("clé relue différente : %v", err)
	}
	if info, err := os.Stat(path); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("droits : %v %v", info.Mode(), err)
	}

	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	body := []byte(`{"target":"x.duckdns.org"}`)
	request, _ := http.NewRequest(http.MethodPut, "https://adresses.pi-appliance.com/api/names/maison", nil)
	key.Sign(request, body, now)
	if public, err := Verify(request, body, now.Add(time.Minute)); err != nil || public != key.Public() {
		t.Fatalf("signature refusée : %v", err)
	}
	if _, err := Verify(request, []byte(`{"target":"y.duckdns.org"}`), now); !errors.Is(err, ErrSignature) {
		t.Fatal("corps modifié accepté")
	}
	if _, err := Verify(request, body, now.Add(MaxClockSkew+time.Second)); !errors.Is(err, ErrSignature) {
		t.Fatal("signature trop ancienne acceptée")
	}
	request.URL.Path = "/api/names/autre"
	if _, err := Verify(request, body, now); !errors.Is(err, ErrSignature) {
		t.Fatal("signature rejouée sur une autre adresse acceptée")
	}
	if _, err := CheckTarget("Maison.DuckDNS.org."); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckTarget("evil.example.com"); !errors.Is(err, ErrInvalid) {
		t.Fatal("cible hors DuckDNS acceptée")
	}
}
