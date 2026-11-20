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
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// imageRunner donne la liste des images du service, mêlée d'un avertissement de Compose.
type imageRunner struct {
	fakeRunner
	images string
}

func (r *imageRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	output, err := r.fakeRunner.Run(ctx, dir, args...)
	if err == nil && strings.HasSuffix(strings.Join(args, " "), "config --images") {
		return []byte(r.images), nil
	}
	return output, err
}

type fakePuller struct {
	manager  *Manager
	present  map[string]bool
	fail     error
	pulled   []string
	snapshot Progress
}

func (f *fakePuller) Present(_ context.Context, image string) (bool, error) {
	return f.present[image], nil
}

func (f *fakePuller) Pull(_ context.Context, image string, report func(PullEvent)) error {
	f.pulled = append(f.pulled, image)
	if f.fail != nil {
		return f.fail
	}
	report(PullEvent{ID: "a1", Status: "Pulling fs layer"})
	report(PullEvent{ID: "b2", Status: "Already exists"})
	report(PullEvent{ID: "a1", Status: "Downloading", Current: 300, Total: 1000})
	report(PullEvent{ID: "c3", Status: "Downloading", Current: 100, Total: 500})
	f.snapshot, _ = f.manager.InstallProgress("cloud")
	report(PullEvent{ID: "a1", Status: "Extracting", Current: 5000, Total: 9000})
	report(PullEvent{ID: "c3", Status: "Pull complete"})
	return nil
}

func TestInstallProgress(t *testing.T) {
	runner := &imageRunner{images: "WARN[0000] the attribute `version` is obsolete\nnextcloud:30\nmariadb:11\nnextcloud:30\n"}
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner}
	puller := &fakePuller{manager: m, present: map[string]bool{"mariadb:11": true}}
	m.Images = puller
	if _, ok := m.InstallProgress("cloud"); ok {
		t.Fatal("avancement d'une installation jamais lancée")
	}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if len(puller.pulled) != 1 || puller.pulled[0] != "nextcloud:30" {
		t.Fatalf("images téléchargées : %v", puller.pulled)
	}
	during := puller.snapshot
	if during.Step != StepDownload || during.Images != 2 || during.Image != 1 || during.Received != 400 || during.Total != 1500 || during.Finished {
		t.Fatalf("pendant le téléchargement : %+v", during)
	}
	after, ok := m.InstallProgress("cloud")
	if !ok || after.Step != StepDone || !after.Finished || after.Error != "" || after.Received != 1500 || after.Total != 1500 || after.AppName != "Nextcloud" {
		t.Fatalf("après l'installation : %+v", after)
	}
	var steps []string
	for _, call := range runner.calls {
		for _, step := range []string{"config --images", " create", " start", "up --detach --wait"} {
			if strings.HasSuffix(call, step) {
				steps = append(steps, step)
			}
		}
	}
	if strings.Join(steps, ",") != "config --images, create, start,up --detach --wait" {
		t.Fatalf("étapes %v", runner.calls)
	}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); !errors.Is(err, ErrExists) {
		t.Fatalf("doublon : %v", err)
	}
	if progress, ok := m.InstallProgress("cloud"); ok {
		t.Fatalf("avancement de l'installation précédente encore montré : %+v", progress)
	}
}

func TestInstallProgressFailure(t *testing.T) {
	for label, test := range map[string]struct {
		fail    string
		pullErr error
		step    string
		message string
	}{
		"téléchargement raté": {fail: " create", pullErr: errors.New("dial tcp: no route to host"), step: StepCreate, message: "relié à internet"},
		"démarrage raté":      {fail: "up --detach --wait", step: StepCheck, message: "erreur docker"},
	} {
		t.Run(label, func(t *testing.T) {
			runner := &imageRunner{fakeRunner: fakeRunner{fail: map[string]bool{test.fail: true}}, images: "nextcloud:30\n"}
			m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner}
			m.Images = &fakePuller{manager: m, fail: test.pullErr}
			_, err := m.Install(context.Background(), nextcloud(t), "cloud", nil)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("erreur %v", err)
			}
			progress, _ := m.InstallProgress("cloud")
			if !progress.Finished || progress.Step != test.step || progress.Error != err.Error() {
				t.Fatalf("avancement %+v", progress)
			}
			if !runner.called("down") {
				t.Fatalf("service pas arrêté : %v", runner.calls)
			}
		})
	}
}

func TestInstallWithoutPuller(t *testing.T) {
	runner := &fakeRunner{}
	if _, err := manager(t, runner).Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	if runner.called("config --images") {
		t.Fatalf("images listées sans moyen de les télécharger : %v", runner.calls)
	}
}

func TestPullReference(t *testing.T) {
	for image, want := range map[string]string{
		"nginx":                         "nginx:latest",
		"nginx:1.27":                    "nginx:1.27",
		"localhost:5000/site":           "localhost:5000/site:latest",
		"ghcr.io/org/app:2":             "ghcr.io/org/app:2",
		"nginx@sha256:0123456789abcdef": "nginx@sha256:0123456789abcdef",
	} {
		if got := pullReference(image); got != want {
			t.Errorf("%s : %s au lieu de %s", image, got, want)
		}
	}
}

// dockerSocket sert une fausse API Docker sur une prise Unix.
func dockerSocket(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "docker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("prise Unix impossible : %v", err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return socket
}

func TestDockerAPIPull(t *testing.T) {
	var asked string
	socket := dockerSocket(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/images/nginx:1.27/json":
			w.Write([]byte(`{}`))
		case r.Method == http.MethodGet:
			http.Error(w, `{"message":"No such image"}`, http.StatusNotFound)
		case r.URL.Path == "/images/create" && r.URL.Query().Get("fromImage") == "cassee:latest":
			w.Write([]byte(`{"status":"Pulling from library/cassee"}` + "\n" + `{"errorDetail":{"message":"toomanyrequests"},"error":"toomanyrequests: limite atteinte"}` + "\n"))
		case r.URL.Path == "/images/create":
			asked = r.URL.Query().Get("fromImage")
			encoder := json.NewEncoder(w)
			encoder.Encode(map[string]any{"status": "Pulling fs layer", "id": "a1"})
			encoder.Encode(map[string]any{"status": "Downloading", "id": "a1", "progressDetail": map[string]int{"current": 10, "total": 20}})
			encoder.Encode(map[string]any{"status": "Pull complete", "id": "a1"})
		default:
			http.NotFound(w, r)
		}
	})
	api := DockerAPI{Socket: socket}
	if present, err := api.Present(context.Background(), "nginx:1.27"); err != nil || !present {
		t.Fatalf("image présente : %v %v", present, err)
	}
	if present, err := api.Present(context.Background(), "redis:7"); err != nil || present {
		t.Fatalf("image absente : %v %v", present, err)
	}
	progress := Progress{layers: map[string]*layer{}}
	if err := api.Pull(context.Background(), "nginx", progress.pulled); err != nil {
		t.Fatal(err)
	}
	if asked != "nginx:latest" || progress.Received != 20 || progress.Total != 20 {
		t.Fatalf("demande %q, avancement %+v", asked, progress)
	}
	if err := api.Pull(context.Background(), "cassee", func(PullEvent) {}); err == nil || !strings.Contains(err.Error(), "limite atteinte") {
		t.Fatalf("erreur du flux : %v", err)
	}
	if err := (DockerAPI{Socket: filepath.Join(t.TempDir(), "absente.sock")}).Pull(context.Background(), "nginx", func(PullEvent) {}); err == nil || !strings.Contains(err.Error(), "Docker ne répond pas") {
		t.Fatalf("Docker absent : %v", err)
	}
}
