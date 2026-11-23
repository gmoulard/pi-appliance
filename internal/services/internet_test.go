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

package services

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

// Applications du catalogue sous un chemin de l'adresse internet du Pi : les
// générateurs « build » ont bien SITE_PATH dans leur manifeste, FileBrowser
// (« base ») son chemin de base.
func TestInternetPath(t *testing.T) {
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{Dir: t.TempDir()}
	for id, mode := range pathApps {
		app, ok := catalog.Get(id)
		if !ok {
			t.Fatalf("%s absent du catalogue", id)
		}
		dir := m.serviceDir("site")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, composeName), app.Compose, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := m.InternetPath(Instance{Name: "site", App: id}); got != PathReady {
			t.Errorf("%s (%s) : %q", id, mode, got)
		}
	}
	for _, id := range []string{"nextcloud", "etherpad", "dokuwiki", "astro", "nextjs", CustomApp} {
		if got := m.InternetPath(Instance{Name: "site", App: id}); got != PathUnsupported {
			t.Errorf("%s : %q", id, got)
		}
	}
	// Installé avant le chemin de base : à mettre à jour.
	if err := os.WriteFile(filepath.Join(m.serviceDir("site"), composeName), []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"hugo", "jekyll", "filebrowser"} {
		if got := m.InternetPath(Instance{Name: "site", App: id}); got != PathUpdate {
			t.Errorf("%s installé avant : %q", id, got)
		}
	}
	if !KeepsPath("filebrowser") || KeepsPath("hugo") || KeepsPath("site-web") || KeepsPath(CustomApp) {
		t.Error("KeepsPath : seul FileBrowser garde le chemin")
	}
	if got := m.InternetPath(Instance{Name: "cockpit", App: "site-web"}); got != PathReserved {
		t.Errorf("nom réservé : %q", got)
	}
	if !slices.Equal(RouterMiddlewares("dokuwiki", AccessUsers), []string{errorsMiddleware, authMiddleware}) {
		t.Errorf("middlewares : %v", RouterMiddlewares("dokuwiki", AccessUsers))
	}
}
