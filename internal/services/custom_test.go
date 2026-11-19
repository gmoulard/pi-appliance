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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dangerous = `services:
  web:
    image: nginx:1.27
    volumes:
      - site:/usr/share/nginx/html
      - /var/run/docker.sock:/var/run/docker.sock
      - type: bind
        source: /etc
        target: /hote/etc
      - ./config:/config
  outil:
    image: alpine:3.20
    privileged: true
    network_mode: host
    pid: host
    cap_add: [SYS_ADMIN]
    devices: ["/dev/ttyUSB0:/dev/ttyUSB0"]
    security_opt: ["seccomp:unconfined"]
volumes:
  site: {}
`

func codes(risks []Risk) string {
	var list []string
	for _, risk := range risks {
		list = append(list, risk.Service+":"+risk.Code)
	}
	return strings.Join(list, ",")
}

func TestAnalyzeCompose(t *testing.T) {
	names, risks, err := AnalyzeCompose([]byte(dangerous))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "outil,web" {
		t.Fatalf("services %v", names)
	}
	want := "outil:privileged,outil:host-network,outil:host-pid,outil:devices,outil:capabilities,outil:security-off,web:docker-socket,web:host-mount,web:host-mount"
	if got := codes(risks); got != want {
		t.Fatalf("risques %s, attendu %s", got, want)
	}
	if !strings.Contains(risks[7].Message, "dossier système /etc du Pi : il peut lire et modifier") {
		t.Fatalf("%s", risks[7].Message)
	}
	if _, risks, err := AnalyzeCompose([]byte("services:\n  web:\n    image: nginx:1.27\n    volumes: [\"donnees:/data\", \"/data\"]\n")); err != nil || len(risks) != 0 {
		t.Fatalf("volume nommé ou anonyme signalé : %v %v", risks, err)
	}
	for label, text := range map[string]string{
		"illisible":    "services: [",
		"vide":         "version: '3'\n",
		"sans image":   "services:\n  web:\n    restart: always\n",
		"à construire": "services:\n  web:\n    image: x:1\n    build: .\n",
	} {
		if _, _, err := AnalyzeCompose([]byte(text)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s : %v", label, err)
		}
	}
}

func TestInstallCustom(t *testing.T) {
	runner := &fakeRunner{}
	m := manager(t, runner)
	request := CustomRequest{Name: "outils", Compose: []byte(dangerous), Service: "web", Port: 80}
	_, err := m.InstallCustom(context.Background(), request)
	var risksErr RisksError
	if !errors.As(err, &risksErr) || len(risksErr.Risks) != 9 {
		t.Fatalf("options dangereuses non signalées : %v", err)
	}
	if runner.called("up") {
		t.Fatal("installé sans confirmation")
	}
	request.ConfirmRisks = true
	result, err := m.InstallCustom(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Instance.App != CustomApp || result.Instance.Address != "outils.maison.example.org" || result.Warnings[0] != Unsupported || len(result.Warnings) != 10 {
		t.Fatalf("%+v", result)
	}
	override, _ := os.ReadFile(filepath.Join(m.Dir, "services", "outils", overrideName))
	if !strings.Contains(string(override), "pi-outils.loadbalancer.server.port\": \"80\"") {
		t.Fatalf("labels Traefik : %s", override)
	}
	for _, bad := range []CustomRequest{
		{Name: "a", Compose: []byte(dangerous), Service: "absent", Port: 80, ConfirmRisks: true},
		{Name: "b", Compose: []byte(dangerous), Service: "web", Port: 0, ConfirmRisks: true},
	} {
		if _, err := m.InstallCustom(context.Background(), bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%+v : %v", bad, err)
		}
	}
}

func TestAdvanced(t *testing.T) {
	runner := &fakeRunner{}
	m := installed(t, runner)
	settings, services, err := m.AdvancedSettings("cloud")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(services, ",") != "app,db" || settings.Service != "app" || len(settings.Ports) != 0 {
		t.Fatalf("%+v %v", settings, services)
	}
	settings.Environment = map[string]string{"PHP_MEMORY_LIMIT": "1G"}
	settings.Ports = []Port{{Host: 8443, Container: 443}}
	settings.Volumes = []Mount{{Source: "/srv/photos", Target: "/photos", ReadOnly: true}, {Source: "archives", Target: "/archives"}}
	if _, err := m.SetAdvanced(context.Background(), "cloud", settings, false); !errors.As(err, new(RisksError)) {
		t.Fatalf("dossier du Pi monté sans confirmation : %v", err)
	}
	runner.calls = nil
	risks, err := m.SetAdvanced(context.Background(), "cloud", settings, true)
	if err != nil || len(risks) != 1 || risks[0].Code != "host-mount" || !strings.Contains(risks[0].Message, "il peut lire ce qu'il") {
		t.Fatalf("%v %v", risks, err)
	}
	if !runner.called("--file " + advancedName + " up --detach --wait") {
		t.Fatalf("réglages non appliqués : %v", runner.calls)
	}
	data, _ := os.ReadFile(filepath.Join(m.Dir, "services", "cloud", advancedName))
	for _, part := range []string{`"8443:443/tcp"`, `"/srv/photos:/photos:ro"`, `"archives:/archives"`, `"PHP_MEMORY_LIMIT": "1G"`, `"archives": {}`} {
		if !strings.Contains(string(data), part) {
			t.Fatalf("%s manque dans %s", part, data)
		}
	}
	read, _, _ := m.AdvancedSettings("cloud")
	if len(read.Volumes) != 2 || read.Ports[0].Protocol != "tcp" {
		t.Fatalf("%+v", read)
	}
	// Retour à aucun réglage : le fichier disparaît.
	if _, err := m.SetAdvanced(context.Background(), "cloud", Advanced{Service: "app"}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "services", "cloud", advancedName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fichier des réglages avancés gardé")
	}
}

func TestAdvancedInvalid(t *testing.T) {
	m := installed(t, &fakeRunner{})
	for label, settings := range map[string]Advanced{
		"conteneur inconnu": {Service: "autre", Environment: map[string]string{"A": "1"}},
		"variable":          {Service: "app", Environment: map[string]string{"A-B": "1"}},
		"retour à la ligne": {Service: "app", Environment: map[string]string{"A": "1\n2"}},
		"port de Traefik":   {Service: "app", Ports: []Port{{Host: 443, Container: 443}}},
		"port en double":    {Service: "app", Ports: []Port{{Host: 9000, Container: 1}, {Host: 9000, Container: 2}}},
		"protocole":         {Service: "app", Ports: []Port{{Host: 9000, Container: 1, Protocol: "sctp"}}},
		"cible relative":    {Service: "app", Volumes: []Mount{{Source: "donnees", Target: "data"}}},
		"source relative":   {Service: "app", Volumes: []Mount{{Source: "../etc", Target: "/data"}}},
		"source pas propre": {Service: "app", Volumes: []Mount{{Source: "/srv/../etc", Target: "/data"}}},
	} {
		if _, err := m.SetAdvanced(context.Background(), "cloud", settings, true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s : %v", label, err)
		}
	}
}

func TestAdvancedRestoresOnFailure(t *testing.T) {
	runner := &fakeRunner{}
	m := installed(t, runner)
	runner.fail = map[string]bool{advancedName + " up": true}
	_, err := m.SetAdvanced(context.Background(), "cloud", Advanced{Service: "app", Ports: []Port{{Host: 9000, Container: 80}}}, false)
	if !errors.Is(err, ErrSettingsRestored) {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Dir, "services", "cloud", advancedName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("réglages ratés gardés")
	}
}
