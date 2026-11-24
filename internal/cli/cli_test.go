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

package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const token = "jeton-de-test"

// fakePi : système de test (racine, commandes lancées) et fausse console.
type fakePi struct {
	t        *testing.T
	root     string
	out, err bytes.Buffer
	mu       sync.Mutex
	commands []string
	// answers : sortie et erreur de chaque commande (« systemctl is-active ssh »…).
	answers  map[string]string
	failures map[string]bool
	requests []string
	// routes : réponses en plus de la fausse console (« GET updates »…).
	routes map[string]func(w http.ResponseWriter, body []byte)
	server *httptest.Server
	env    Env
}

func newFakePi(t *testing.T, input string) *fakePi {
	t.Helper()
	pi := &fakePi{t: t, root: t.TempDir(), answers: map[string]string{}, failures: map[string]bool{}}
	pi.write("/etc/passwd", "root:x:0:0:root:/root:/bin/bash\nnobody:x:65534:65534::/nonexistent:/usr/sbin/nologin\nmarie:x:1000:1000:Marie:/home/marie:/bin/bash\n")
	pi.write("/etc/hostname", "controltower\n")
	pi.write("/etc/os-release", "NAME=\"Debian GNU/Linux\"\nPRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n")
	pi.write("/proc/sys/kernel/osrelease", "6.6.51+rpt-rpi-v8\n")
	pi.write("/proc/device-tree/model", "Raspberry Pi 4 Model B Rev 1.5\x00")
	tokenFile := filepath.Join(pi.root, "cli-token")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pi.server = httptest.NewServer(http.HandlerFunc(pi.console))
	t.Cleanup(pi.server.Close)
	pi.env = Env{
		Version:    "v0.1.6",
		ConsoleURL: pi.server.URL + "/pi-appliance",
		TokenFile:  tokenFile,
		Root:       pi.root,
		Out:        &pi.out,
		Err:        &pi.err,
		In:         strings.NewReader(input),
		Getenv:     func(name string) string { return map[string]string{"SUDO_USER": "marie"}[name] },
		Euid:       0,
		Command:    pi.command,
		Chown:      func(string, int, int) error { return nil },
		HTTP:       pi.server.Client(),
	}
	return pi
}

func (pi *fakePi) write(path, content string) {
	pi.t.Helper()
	if err := os.MkdirAll(filepath.Dir(pi.root+path), 0o755); err != nil {
		pi.t.Fatal(err)
	}
	if err := os.WriteFile(pi.root+path, []byte(content), 0o644); err != nil {
		pi.t.Fatal(err)
	}
}

func (pi *fakePi) command(_ context.Context, name string, args ...string) (string, error) {
	line := strings.Join(append([]string{name}, args...), " ")
	pi.mu.Lock()
	defer pi.mu.Unlock()
	pi.commands = append(pi.commands, line)
	if pi.failures[line] {
		return pi.answers[line], errors.New("échec")
	}
	return pi.answers[line], nil
}

// console : fausse API de la console, jeton exigé sauf pour api/health.
func (pi *fakePi) console(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/pi-appliance/api/")
	if path == "health" {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": "v0.1.6"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	pi.mu.Lock()
	pi.requests = append(pi.requests, r.Method+" "+r.URL.RequestURI()+" "+string(body))
	route := pi.routes[r.Method+" "+path]
	pi.mu.Unlock()
	if route != nil {
		route(w, body)
		return
	}
	switch r.Method + " " + path {
	case "GET catalog":
		io.WriteString(w, `{"apps": [{"id": "portainer", "name": "Portainer", "description": "Gère Docker. Pour les personnes à l'aise.", "subdomain": "portainer", "access": "admins"},
			{"id": "nextcloud", "name": "Nextcloud", "description": "Vos fichiers. Partout.", "subdomain": "cloud", "access": "public", "memory_mb": 512, "disk_mb": 2048,
			 "parameters": [{"name": "admin_password", "label": "Mot de passe de l'administrateur", "generate": true}]}]}`)
	case "GET services":
		io.WriteString(w, `{"services": [{"name": "cloud", "app": "nextcloud", "app_name": "Nextcloud", "address": "cloud-controltower.local", "status": {"color": "green", "message": "En marche."}},
			{"name": "vpn", "app": "wireguard", "app_name": "WireGuard", "no_web_page": true, "status": {"color": "red", "message": "Arrêté."}}]}`)
	case "POST services":
		var request map[string]any
		json.Unmarshal(body, &request)
		if request["app"] == "portainer" && request["confirm_risks"] != true {
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"error": "Ces options donnent la main sur tout le Pi : confirmez pour continuer.", "risks": ["accès à Docker"]}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"instance": {"name": "cloud", "app_name": "Nextcloud", "address": "cloud-controltower.local"}, "secrets": {"admin_password": "s3cret"}}`)
	case "DELETE services/cloud":
		w.WriteHeader(http.StatusNoContent)
	case "POST services/cloud/restart":
		io.WriteString(w, `{"color": "green", "message": "En marche."}`)
	case "GET services/cloud/progress":
		io.WriteString(w, `{"step": "download", "images": 2, "image": 1}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"error": "Ce service n'existe pas."}`)
	}
}

func (pi *fakePi) run(args ...string) int {
	return Run(args, pi.env)
}

func TestVersion(t *testing.T) {
	pi := newFakePi(t, "")
	pi.answers["docker version --format {{.Server.Version}}"] = "27.5.1\n"
	pi.answers["docker inspect --format {{.Config.Image}} pi-appliance-traefik"] = "traefik:v3.7.19\n"
	if code := pi.run("version"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	for _, want := range []string{"pi-appliance : v0.1.6 (console en marche : v0.1.6)", "Debian GNU/Linux 12 (bookworm), noyau 6.6.51+rpt-rpi-v8", "Raspberry Pi 4 Model B Rev 1.5\n", "Docker : 27.5.1", "Traefik : traefik:v3.7.19"} {
		if !strings.Contains(pi.out.String(), want) {
			t.Errorf("sans %q :\n%s", want, pi.out.String())
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	pi := newFakePi(t, "")
	if code := pi.run("voler"); code != 2 || !strings.Contains(pi.err.String(), "Commande inconnue") {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	pi.err.Reset()
	if code := pi.run("help"); code != 0 || !strings.Contains(pi.out.String(), "pia installer") {
		t.Fatalf("aide : code %d", code)
	}
	// « pia » seul, ou avec --help : l'aide.
	for _, args := range [][]string{nil, {"--help"}, {"-h"}} {
		pi.out.Reset()
		if code := Run(args, pi.env); code != 0 || !strings.HasPrefix(pi.out.String(), "pia : ligne de commande du Pi") {
			t.Fatalf("%v : code %d :\n%s", args, code, pi.out.String())
		}
	}
}

func TestServicesAndState(t *testing.T) {
	pi := newFakePi(t, "")
	if code := pi.run("services"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	for _, want := range []string{"cloud", "https://cloud-controltower.local", "(pas de page web)", "arrêté ou en panne", "Arrêté."} {
		if !strings.Contains(pi.out.String(), want) {
			t.Errorf("sans %q :\n%s", want, pi.out.String())
		}
	}
	pi.out.Reset()
	if code := pi.run("etat"); code != 0 || !strings.Contains(pi.out.String(), "2 service(s) installé(s) : 1 en marche, 0 à surveiller, 1 arrêté(s) ou en panne.") {
		t.Fatalf("état : code %d :\n%s%s", code, pi.out.String(), pi.err.String())
	}
}

func TestCatalogue(t *testing.T) {
	pi := newFakePi(t, "")
	if code := pi.run("catalogue"); code != 0 || !strings.Contains(pi.out.String(), "nextcloud") {
		t.Fatalf("code %d :\n%s", code, pi.out.String())
	}
	pi.out.Reset()
	if code := pi.run("catalogue", "nextcloud"); code != 0 || !strings.Contains(pi.out.String(), "--param admin_password=") || !strings.Contains(pi.out.String(), "créé par le Pi") {
		t.Fatalf("détail : code %d :\n%s", code, pi.out.String())
	}
	if code := pi.run("catalogue", "inconnue"); code != 1 {
		t.Fatalf("application inconnue : code %d", code)
	}
}

func TestInstall(t *testing.T) {
	pi := newFakePi(t, "")
	if code := pi.run("installer", "nextcloud", "--param", "admin_password=abc", "--acces=home"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	if !strings.Contains(pi.out.String(), "Adresse : https://cloud-controltower.local") || !strings.Contains(pi.out.String(), "admin_password : s3cret") {
		t.Fatalf("sortie :\n%s", pi.out.String())
	}
	found := false
	for _, request := range pi.requests {
		if strings.HasPrefix(request, "POST /pi-appliance/api/services ") {
			found = true
			var body map[string]any
			json.Unmarshal([]byte(strings.SplitN(request, " ", 3)[2]), &body)
			if body["name"] != "cloud" || body["access"] != "home" || body["parameters"].(map[string]any)["admin_password"] != "abc" || body["confirm_risks"] != false {
				t.Fatalf("demande %v", body)
			}
		}
	}
	if !found {
		t.Fatal("pas de demande d'installation")
	}
	// Application qui a la main sur tout le Pi : confirmation demandée.
	pi.err.Reset()
	if code := pi.run("install", "portainer"); code != 1 || !strings.Contains(pi.err.String(), "accès à Docker") || !strings.Contains(pi.err.String(), "--confirmer") {
		t.Fatalf("risques : code %d : %s", code, pi.err.String())
	}
	if code := pi.run("installer", "portainer", "--confirmer"); code != 0 {
		t.Fatalf("confirmé : code %d : %s", code, pi.err.String())
	}
	if code := pi.run("installer"); code != 2 {
		t.Fatalf("sans application : code %d", code)
	}
	if code := pi.run("installer", "nextcloud", "--param", "sansvaleur"); code != 2 {
		t.Fatalf("paramètre mal écrit : code %d", code)
	}
}

func TestUninstall(t *testing.T) {
	pi := newFakePi(t, "non\n")
	if code := pi.run("desinstaller", "cloud"); code != 0 || !strings.Contains(pi.out.String(), "données sont gardées") {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	if last := pi.requests[len(pi.requests)-1]; !strings.HasPrefix(last, "DELETE /pi-appliance/api/services/cloud?data=keep") {
		t.Fatalf("demande %q", last)
	}
	count := len(pi.requests)
	if code := pi.run("desinstaller", "cloud", "--effacer-donnees"); code != 1 || len(pi.requests) != count {
		t.Fatalf("refusé à la question : code %d, %d demandes", code, len(pi.requests)-count)
	}
	pi.env.In = strings.NewReader("oui\n")
	if code := pi.run("uninstall", "cloud", "--effacer-donnees"); code != 0 {
		t.Fatalf("confirmé : code %d : %s", code, pi.err.String())
	}
	if last := pi.requests[len(pi.requests)-1]; !strings.HasPrefix(last, "DELETE /pi-appliance/api/services/cloud?data=delete") {
		t.Fatalf("demande %q", last)
	}
	pi.err.Reset()
	if code := pi.run("desinstaller", "inconnu"); code != 1 || !strings.Contains(pi.err.String(), "Ce service n'existe pas.") {
		t.Fatalf("service inconnu : code %d : %s", code, pi.err.String())
	}
}

func TestControlAndToken(t *testing.T) {
	pi := newFakePi(t, "")
	if code := pi.run("redemarrer", "cloud"); code != 0 || !strings.Contains(pi.out.String(), "cloud est redémarré.") {
		t.Fatalf("code %d : %s%s", code, pi.out.String(), pi.err.String())
	}
	// Sans droit de lire le jeton : sudo demandé.
	pi.env.Euid = 1000
	pi.env.TokenFile = filepath.Join(pi.root, "absent")
	pi.err.Reset()
	if code := pi.run("services"); code != 1 || !strings.Contains(pi.err.String(), "sudo") {
		t.Fatalf("sans root : code %d : %s", code, pi.err.String())
	}
	// Jeton périmé (console redémarrée) : message clair.
	pi.env.Euid = 0
	pi.env.TokenFile = filepath.Join(pi.root, "vieux")
	os.WriteFile(pi.env.TokenFile, []byte("ancien"), 0o600)
	pi.err.Reset()
	if code := pi.run("services"); code != 1 || !strings.Contains(pi.err.String(), "refuse le jeton") {
		t.Fatalf("jeton périmé : code %d : %s", code, pi.err.String())
	}
}

func TestWriteToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "system", "cli-token")
	first, err := WriteToken(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteToken(path)
	if err != nil || first == second || len(second) < 40 {
		t.Fatalf("jetons %q %q %v", first, second, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %v %v", info, err)
	}
	data, _ := os.ReadFile(path)
	if strings.TrimSpace(string(data)) != second {
		t.Fatalf("fichier %q", data)
	}
}

// sshKey : clé publique ed25519 au format OpenSSH.
func sshKey(t *testing.T, comment string) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	var blob []byte
	for _, part := range [][]byte{[]byte("ssh-ed25519"), public} {
		blob = binary.BigEndian.AppendUint32(blob, uint32(len(part)))
		blob = append(blob, part...)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " " + comment
}

func TestSSHKeys(t *testing.T) {
	pi := newFakePi(t, "")
	keyFile := pi.root + "/home/marie/.ssh/authorized_keys"
	first, second := sshKey(t, "marie@portable"), sshKey(t, "marie@téléphone")
	if code := pi.run("ssh", "cle", "ajouter", first); code != 0 {
		t.Fatalf("ajout : code %d : %s", code, pi.err.String())
	}
	pubFile := filepath.Join(pi.root, "id.pub")
	os.WriteFile(pubFile, []byte(second+"\n"), 0o644)
	if code := pi.run("ssh", "clé", "ajouter", "--fichier", pubFile); code != 0 {
		t.Fatalf("ajout par fichier : code %d : %s", code, pi.err.String())
	}
	pi.out.Reset()
	if code := pi.run("ssh", "cle", "ajouter", first); code != 0 || !strings.Contains(pi.out.String(), "déjà là") {
		t.Fatalf("doublon : %d %s", code, pi.out.String())
	}
	info, err := os.Stat(keyFile)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys : %v %v", info, err)
	}
	if dir, _ := os.Stat(filepath.Dir(keyFile)); dir.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh : %v", dir.Mode())
	}
	pi.out.Reset()
	if code := pi.run("ssh", "cle", "liste"); code != 0 || !strings.Contains(pi.out.String(), "1. ssh-ed25519 SHA256:") || !strings.Contains(pi.out.String(), "2. ssh-ed25519") {
		t.Fatalf("liste : code %d :\n%s", code, pi.out.String())
	}
	for _, bad := range []string{"bonjour", "ssh-ed25519 pasdubase64!", "-----BEGIN OPENSSH PRIVATE KEY-----", "ssh-rsa " + strings.Fields(first)[1]} {
		if code := pi.run("ssh", "cle", "ajouter", bad); code == 0 {
			t.Errorf("clé refusée attendue : %q (code %d)", bad, code)
		}
	}
	// Une ligne que la commande ne comprend pas (options) est gardée.
	data, _ := os.ReadFile(keyFile)
	os.WriteFile(keyFile, append([]byte("# mes clés\n"), data...), 0o600)
	if code := pi.run("ssh", "cle", "retirer", "1"); code != 0 {
		t.Fatalf("retrait : code %d : %s", code, pi.err.String())
	}
	data, _ = os.ReadFile(keyFile)
	if strings.Contains(string(data), "marie@portable") || !strings.Contains(string(data), "marie@téléphone") || !strings.HasPrefix(string(data), "# mes clés\n") {
		t.Fatalf("après retrait :\n%s", data)
	}
	if code := pi.run("ssh", "cle", "retirer", "5"); code != 1 {
		t.Fatalf("numéro inconnu : code %d", code)
	}
	// Dernière clé, mots de passe refusés : retrait impossible.
	pi.answers["sshd -T"] = "port 22\npasswordauthentication no\n"
	if code := pi.run("ssh", "cle", "retirer", "1"); code != 1 || !strings.Contains(pi.err.String(), "dernière clé") {
		t.Fatalf("dernière clé : code %d : %s", code, pi.err.String())
	}
	// Sans root : refusé.
	pi.env.Euid = 1000
	if code := pi.run("ssh", "cle", "ajouter", first); code != 1 || !strings.Contains(pi.err.String(), "sudo") {
		t.Fatalf("sans root : code %d", code)
	}
}

func TestSSHPassword(t *testing.T) {
	pi := newFakePi(t, "")
	dropIn := pi.root + SSHDropIn
	if code := pi.run("ssh", "mot-de-passe", "desactiver"); code != 1 || !strings.Contains(pi.err.String(), "aucune clé") {
		t.Fatalf("sans clé : code %d : %s", code, pi.err.String())
	}
	if _, err := os.Stat(dropIn); !os.IsNotExist(err) {
		t.Fatal("réglage écrit sans clé")
	}
	pi.run("ssh", "cle", "ajouter", sshKey(t, "marie"))
	if code := pi.run("ssh", "mot-de-passe", "desactiver"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	data, _ := os.ReadFile(dropIn)
	if !strings.Contains(string(data), "PasswordAuthentication no\n") || !strings.Contains(string(data), "KbdInteractiveAuthentication no\n") {
		t.Fatalf("réglage :\n%s", data)
	}
	if got := strings.Join(pi.commands, "\n"); !strings.Contains(got, "sshd -t") || !strings.Contains(got, "systemctl try-reload-or-restart ssh") {
		t.Fatalf("commandes :\n%s", got)
	}
	// sshd -t refuse : l'ancien réglage revient.
	pi.failures["sshd -t"] = true
	if code := pi.run("ssh", "mot-de-passe", "activer"); code != 1 {
		t.Fatalf("configuration refusée : code %d", code)
	}
	if again, _ := os.ReadFile(dropIn); string(again) != string(data) {
		t.Fatalf("ancien réglage non remis :\n%s", again)
	}
	delete(pi.failures, "sshd -t")
	if code := pi.run("ssh", "password", "on"); code != 0 {
		t.Fatalf("activer : code %d : %s", code, pi.err.String())
	}
	if data, _ := os.ReadFile(dropIn); !strings.Contains(string(data), "PasswordAuthentication yes\n") {
		t.Fatalf("réglage :\n%s", data)
	}
}

func TestSSHServiceAndState(t *testing.T) {
	pi := newFakePi(t, "non\n")
	pi.answers["systemctl is-active ssh"] = "active\n"
	pi.answers["systemctl is-enabled ssh"] = "enabled\n"
	pi.answers["sshd -T"] = "passwordauthentication yes\n"
	if code := pi.run("ssh"); code != 0 {
		t.Fatalf("code %d : %s", code, pi.err.String())
	}
	for _, want := range []string{"SSH : en marche.", "Au démarrage du Pi : allumé.", "mot de passe : acceptée.", "Clés SSH de marie : 0.", "ssh marie@controltower.local"} {
		if !strings.Contains(pi.out.String(), want) {
			t.Errorf("sans %q :\n%s", want, pi.out.String())
		}
	}
	pi.env.Getenv = func(name string) string {
		return map[string]string{"SSH_CONNECTION": "192.168.1.20 5000 192.168.1.196 22"}[name]
	}
	pi.out.Reset()
	if code := pi.run("ssh", "desactiver"); code != 1 || !strings.Contains(pi.out.String(), "Vous êtes connecté par SSH") {
		t.Fatalf("refus à la question : code %d :\n%s", code, pi.out.String())
	}
	if strings.Contains(strings.Join(pi.commands, "\n"), "disable") {
		t.Fatal("SSH éteint sans confirmation")
	}
	if code := pi.run("ssh", "desactiver", "--oui"); code != 0 || !strings.Contains(strings.Join(pi.commands, "\n"), "systemctl disable --now ssh") {
		t.Fatalf("désactiver : code %d", code)
	}
	if code := pi.run("ssh", "activer"); code != 0 || !strings.Contains(strings.Join(pi.commands, "\n"), "systemctl enable --now ssh") {
		t.Fatalf("activer : code %d", code)
	}
}

// Sans root, pia se relance lui-même avec sudo, sauf pour aide et version.
func TestElevate(t *testing.T) {
	pi := newFakePi(t, "")
	pi.env.Euid = 1000
	var relaunched [][]string
	pi.env.Elevate = func(args []string) error {
		relaunched = append(relaunched, args)
		return errors.New("sudo absent")
	}
	for _, args := range [][]string{{"aide"}, {"version"}, {"help"}} {
		if code := pi.run(args...); code != 0 {
			t.Fatalf("%v sans root : code %d : %s", args, code, pi.err.String())
		}
	}
	if len(relaunched) != 0 {
		t.Fatalf("aide et version relancées : %v", relaunched)
	}
	if code := pi.run("ssh", "cle", "ajouter", "x"); code != 1 || !strings.Contains(pi.err.String(), "droits d'administrateur") {
		t.Fatalf("échec de sudo : code %d : %s", code, pi.err.String())
	}
	pi.run("etat")
	pi.run("services")
	want := [][]string{{"ssh", "cle", "ajouter", "x"}, {"etat"}, {"services"}}
	if fmt.Sprint(relaunched) != fmt.Sprint(want) {
		t.Fatalf("relances : %v, attendu %v", relaunched, want)
	}
	// Déjà root : pas de relance.
	pi.env.Euid = 0
	pi.run("services")
	if len(relaunched) != 3 {
		t.Fatalf("relancé alors que root : %v", relaunched)
	}
}

// -force (TODO-5.20) : aucune question, à toute place de la ligne, gardé par la relance sudo.
func TestForce(t *testing.T) {
	pi := newFakePi(t, "non\n")
	for _, args := range [][]string{
		{"-force", "desinstaller", "cloud", "--effacer-donnees"},
		{"desinstaller", "cloud", "--effacer-donnees", "--force"},
		{"desinstaller", "--forcer", "cloud", "--effacer-donnees"},
	} {
		count := len(pi.requests)
		pi.out.Reset()
		if code := pi.run(args...); code != 0 || len(pi.requests) == count {
			t.Fatalf("%v : code %d : %s", args, code, pi.err.String())
		}
		if last := pi.requests[len(pi.requests)-1]; !strings.HasPrefix(last, "DELETE /pi-appliance/api/services/cloud?data=delete") {
			t.Fatalf("%v : demande %q", args, last)
		}
		if !strings.Contains(pi.out.String(), "sauf sauvegarde. oui (-force)") {
			t.Fatalf("%v : question non affichée : %s", args, pi.out.String())
		}
	}
	// Sans -force, la réponse lue reste « non ».
	count := len(pi.requests)
	if code := pi.run("desinstaller", "cloud", "--effacer-donnees"); code != 1 || len(pi.requests) != count {
		t.Fatalf("sans -force : code %d", code)
	}
	// -force vaut aussi --confirmer pour les options à risque.
	if code := pi.run("-force", "installer", "portainer"); code != 0 {
		t.Fatalf("installer -force : code %d : %s", code, pi.err.String())
	}
	if !strings.Contains(help, "-force") || !ForceFlag("-Force") || ForceFlag("-f") {
		t.Fatal("-force absent de l'aide")
	}
	// La relance avec sudo garde -force.
	pi.env.Euid = 1000
	var relaunched []string
	pi.env.Elevate = func(args []string) error { relaunched = args; return errors.New("sudo absent") }
	pi.run("-force", "desinstaller", "cloud")
	if fmt.Sprint(relaunched) != "[-force desinstaller cloud]" {
		t.Fatalf("relance : %v", relaunched)
	}
}
