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
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/marketplace"
)

const sampleProcDevices = `Character devices:
  1 mem
  5 /dev/tty
 81 video4linux
226 drm
236 media
253 dma_heap

Block devices:
  8 sd
237 media
`

func withProcDevices(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "devices")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := procDevices
	procDevices = path
	t.Cleanup(func() { procDevices = previous })
}

func cameraApp(t *testing.T) marketplace.App {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	app, ok := catalog.Get(CameraApp)
	if !ok {
		t.Fatal("application camera absente du catalogue")
	}
	return app
}

// Seuls les pilotes des caméras (vidéo, media, dma_heap), en mode caractère.
func TestCameraRules(t *testing.T) {
	withProcDevices(t, sampleProcDevices)
	if got := strings.Join(cameraRules(), ","); got != "c 81:* rmw,c 236:* rmw,c 253:* rmw" {
		t.Fatalf("règles %s", got)
	}
	procDevices = filepath.Join(t.TempDir(), "absent")
	if got := strings.Join(cameraRules(), ","); got != "c 81:* rmw" {
		t.Fatalf("règles sans /proc/devices : %s", got)
	}
}

// Les deux services des caméras reçoivent /dev et les seules règles des caméras.
func TestCameraOverride(t *testing.T) {
	withProcDevices(t, sampleProcDevices)
	app := cameraApp(t)
	for _, name := range cameraServices {
		if !strings.Contains(string(app.Compose), "\n  "+name+":\n") {
			t.Fatalf("service %s absent du manifeste", name)
		}
	}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: &fakeRunner{}}
	if _, err := m.Install(context.Background(), app, "camera", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.serviceDir("camera"), overrideName))
	if err != nil {
		t.Fatal(err)
	}
	var override struct {
		Services map[string]struct {
			Labels            map[string]string `json:"labels"`
			Volumes           []string          `json:"volumes"`
			DeviceCgroupRules []string          `json:"device_cgroup_rules"`
		} `json:"services"`
	}
	if err := json.Unmarshal(data, &override); err != nil {
		t.Fatal(err)
	}
	for _, name := range cameraServices {
		service := override.Services[name]
		if !slices.Equal(service.Volumes, []string{"/dev:/dev"}) || len(service.DeviceCgroupRules) != 3 {
			t.Errorf("%s : %+v", name, service)
		}
	}
	if override.Services["motioneye"].Labels["traefik.enable"] != "true" || override.Services["camera-pi"].Labels != nil {
		t.Errorf("labels Traefik : %+v", override.Services)
	}
	// Le changement d'accès garde les caméras.
	updated, err := withAccess(data, "camera", CameraApp, AccessHome)
	if err != nil || !strings.Contains(string(updated), "device_cgroup_rules") {
		t.Fatalf("accès changé : %v %s", err, updated)
	}
}

// L'installation des caméras est confirmée, sans être réservée aux administrateurs.
func TestCameraRisks(t *testing.T) {
	app := cameraApp(t)
	risks, err := MarketplaceRisks(app)
	if err != nil {
		t.Fatal(err)
	}
	if len(risks) != 1 || risks[0].Code != cameraRisk {
		t.Fatalf("confirmations %+v", risks)
	}
	if app.Access != AccessUsers {
		t.Fatalf("accès par défaut %s", app.Access)
	}
}

// Les vidéos (medias, pi-appliance.update-copy=false) ne sont pas copiées avant une mise à jour.
func TestWithoutNoCopy(t *testing.T) {
	copies := []volumeCopy{{volume: "pi-camera_config"}, {volume: "pi-camera_medias"}}
	kept := withoutNoCopy(copies, "camera", cameraApp(t).Compose)
	if len(kept) != 1 || kept[0].volume != "pi-camera_config" || len(copies) != 2 {
		t.Fatalf("volumes copiés %+v", kept)
	}
	if kept := withoutNoCopy(copies, "camera", []byte("services: [")); len(kept) != 2 {
		t.Fatalf("manifeste illisible : %+v", kept)
	}
}
