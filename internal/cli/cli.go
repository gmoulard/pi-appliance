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

// Package cli : ligne de commande du Pi, « pia <commande> » (lien vers le
// binaire de la console) ou « pi-appliance <commande> »
// (spécification 5.2). Pour les personnes à l'aise avec SSH : la console reste
// le moyen prévu pour tout le monde. Les services passent par la console en
// marche (son API sur 127.0.0.1:8080, avec un jeton lisible par root seul) ;
// la version du socle et SSH se lisent et se règlent sur le système.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	// DefaultTokenFile : jeton écrit par la console à chaque démarrage (droits 600).
	DefaultTokenFile = "/var/lib/pi-appliance/system/cli-token"
	// ShortName : nom court de la commande, lien /usr/local/bin/pia vers pi-appliance.
	ShortName = "pia"
	// DefaultConsoleURL : la console, en direct sur le Pi (pas à travers Traefik).
	DefaultConsoleURL = "http://127.0.0.1:8080/pi-appliance"
)

// Env : tout ce dont la ligne de commande a besoin, remplacé dans les tests.
type Env struct {
	Version    string
	ConsoleURL string
	TokenFile  string
	// Root : préfixe des fichiers du système (/etc, /proc) ; vide sur le Pi.
	Root   string
	Out    io.Writer
	Err    io.Writer
	In     io.Reader
	Getenv func(string) string
	Euid   int
	// Command lance un programme du système et rend sa sortie.
	Command func(ctx context.Context, name string, args ...string) (string, error)
	// Elevate relance la commande args avec les droits d'administrateur (sudo) et
	// ne revient qu'en cas d'échec ; nil : pas de relance (tests).
	Elevate func(args []string) error
	// Chown donne un fichier à un compte (clés SSH) ; os.Lchown sur le Pi.
	Chown func(path string, uid, gid int) error
	HTTP  *http.Client
}

// Defaults : l'environnement du vrai Pi.
func Defaults(version string) Env {
	return Env{
		Version:    version,
		ConsoleURL: DefaultConsoleURL,
		TokenFile:  DefaultTokenFile,
		Out:        os.Stdout,
		Err:        os.Stderr,
		In:         os.Stdin,
		Getenv:     os.Getenv,
		Euid:       os.Geteuid(),
		Command: func(ctx context.Context, name string, args ...string) (string, error) {
			output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			return string(output), err
		},
		Chown:   os.Lchown,
		HTTP:    &http.Client{},
		Elevate: elevate,
	}
}

// elevate remplace le programme par « sudo <ce binaire> <args> » : on tape
// toujours « pia … », sudo demande le mot de passe s'il le faut.
func elevate(args []string) error {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	command := []string{"sudo", "--", self}
	// sudo efface l'environnement : SSH_CONNECTION est gardé pour que
	// « pia ssh desactiver » sache encore qu'il est lancé par SSH.
	if connection := os.Getenv("SSH_CONNECTION"); connection != "" {
		if envPath, err := exec.LookPath("env"); err == nil {
			command = []string{"sudo", "--", envPath, "SSH_CONNECTION=" + connection, self}
		}
	}
	return syscall.Exec(sudo, append(command, args...), os.Environ())
}

// asUser : commandes qui ne demandent jamais les droits d'administrateur.
var asUser = map[string]bool{"aide": true, "version": true}

// aliases : quelques équivalents anglais des commandes.
var aliases = map[string]string{
	"help": "aide", "status": "etat", "état": "etat", "catalog": "catalogue",
	"list": "services", "install": "installer", "uninstall": "desinstaller",
	"désinstaller": "desinstaller", "remove": "desinstaller", "start": "demarrer",
	"démarrer": "demarrer", "stop": "arreter", "arrêter": "arreter", "restart": "redemarrer",
	"redémarrer": "redemarrer", "mise-a-jour": "update", "mise-à-jour": "update",
	"mettre-a-jour": "update", "mettre-à-jour": "update", "upgrade": "update",
}

// usageError : mauvaise utilisation ; l'aide de la commande suit le message.
type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

// Run exécute la commande args et rend le code de sortie du programme.
func Run(args []string, env Env) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		args = []string{"aide"}
	}
	command := strings.ToLower(args[0])
	if alias, ok := aliases[command]; ok {
		command = alias
	}
	commands := map[string]func(Env, []string) error{
		"aide":         func(env Env, _ []string) error { fmt.Fprint(env.Out, help); return nil },
		"version":      version,
		"etat":         state,
		"catalogue":    catalogue,
		"services":     list,
		"installer":    install,
		"desinstaller": uninstall,
		"demarrer":     func(env Env, args []string) error { return control(env, args, "start") },
		"arreter":      func(env Env, args []string) error { return control(env, args, "stop") },
		"redemarrer":   func(env Env, args []string) error { return control(env, args, "restart") },
		"ssh":          sshCommand,
		"update":       updateCommand,
		"user":         userCommand,
	}
	run, ok := commands[command]
	if !ok {
		fmt.Fprintf(env.Err, "Commande inconnue : %s\n\n%s", args[0], help)
		return 2
	}
	// Toutes les autres commandes lisent le jeton de la console ou le système :
	// pia se relance lui-même avec sudo, jamais besoin de le taper.
	if env.Euid != 0 && !asUser[command] && env.Elevate != nil {
		err := env.Elevate(args)
		fmt.Fprintf(env.Err, "Impossible de demander les droits d'administrateur du Pi (%v) : connectez-vous avec un compte qui peut utiliser sudo.\n", err)
		return 1
	}
	if err := run(env, args[1:]); err != nil {
		fmt.Fprintln(env.Err, err)
		var usage usageError
		var userUsage userUsageError
		if errors.As(err, &userUsage) {
			return 2
		}
		if errors.As(err, &usage) {
			fmt.Fprint(env.Err, "\n"+help)
			return 2
		}
		return 1
	}
	return 0
}

const help = `pia : ligne de commande du Pi (la console reste le moyen le plus simple) ;
« pi-appliance <commande> » marche aussi.

  pia version                     version du socle : pi-appliance, système, Docker, Traefik
  pia etat                        la console répond-elle, état des services
  pia catalogue [application]     applications installables (détail d'une application)
  pia services                    services installés et leur état
  pia installer <application> [--nom NOM] [--acces public|users|home]
                  [--param nom=valeur]... [--confirmer]
  pia desinstaller <service> [--effacer-donnees] [--oui]
                                  données gardées sauf --effacer-donnees
  pia demarrer|arreter|redemarrer <service>

  pia update [--oui]              installer la dernière version de pi-appliance
  pia update list                 versions publiées, celle du Pi signalée
  pia update <version> [--oui]    installer cette version (plus récente ou plus ancienne)

  pia user list|show|add|edit|reset|2fa-off|delete
                                  comptes de la console (pia user : aide détaillée)

  pia ssh [etat]                  SSH en marche ? mots de passe ? clés ?
  pia ssh activer|desactiver [--oui]
  pia ssh cle liste [--utilisateur NOM]
  pia ssh cle ajouter "<clé publique>" | --fichier FICHIER [--utilisateur NOM]
  pia ssh cle retirer <numéro> [--utilisateur NOM]
  pia ssh mot-de-passe activer|desactiver

Inutile de taper sudo : pia demande lui-même les droits d'administrateur du Pi
quand une commande en a besoin.
`

// flags : options --nom valeur (ou --nom=valeur) et options sans valeur.
type flags struct {
	positional []string
	values     map[string][]string
	set        map[string]bool
}

func parseFlags(args []string, withValue, without []string) (flags, error) {
	f := flags{values: map[string][]string{}, set: map[string]bool{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			f.positional = append(f.positional, arg)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch {
		case contains(without, name) && !hasValue:
			f.set[name] = true
		case contains(withValue, name):
			if !hasValue {
				if i+1 >= len(args) {
					return f, usageError{"Il manque la valeur de --" + name + "."}
				}
				i++
				value = args[i]
			}
			f.values[name] = append(f.values[name], value)
		default:
			return f, usageError{"Option inconnue : " + arg}
		}
	}
	return f, nil
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func (f flags) value(name string) string {
	if list := f.values[name]; len(list) > 0 {
		return list[len(list)-1]
	}
	return ""
}

// errNeedRoot : commande qui change le système ou lit le jeton de la console.
func errNeedRoot(args ...string) error {
	return fmt.Errorf("Cette commande a besoin des droits d'administrateur du Pi (pia %s) : connectez-vous avec un compte qui peut utiliser sudo.", strings.Join(args, " "))
}

// confirm pose une question oui/non ; « o », « oui », « y », « yes » valent oui.
func confirm(env Env, question string) bool {
	fmt.Fprint(env.Out, question+" [o/N] ")
	line, _ := bufio.NewReader(env.In).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "o", "oui", "y", "yes":
		return true
	}
	return false
}

// WriteToken tire un nouveau jeton et l'écrit pour root seul.
func WriteToken(path string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return "", err
	}
	return token, os.Rename(temporary, path)
}

// apiError : réponse d'erreur de la console ({"error": "…"}).
type apiError struct {
	status  int
	message string
	body    []byte
}

func (e apiError) Error() string { return e.message }

// call appelle l'API de la console ; out reçoit la réponse JSON (nil : ignorée).
func call(ctx context.Context, env Env, method, path string, body, out any) error {
	token, err := os.ReadFile(env.TokenFile)
	if errors.Is(err, fs.ErrPermission) || (err != nil && env.Euid != 0) {
		return errNeedRoot(strings.Join(os.Args[1:], " "))
	}
	if err != nil {
		return fmt.Errorf("La console n'a pas encore écrit son jeton (%s) : est-elle démarrée ? sudo systemctl status pi-appliance", env.TokenFile)
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, env.ConsoleURL+"/api/"+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	// Exigé par la console sur ce qui modifie (apiHeaderGuard).
	request.Header.Set("X-Pi-Appliance", "1")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := env.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("La console ne répond pas (%v) : sudo systemctl status pi-appliance", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		var reply struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &reply)
		if reply.Error == "" {
			reply.Error = fmt.Sprintf("La console a répondu %s.", response.Status)
		}
		if response.StatusCode == http.StatusUnauthorized {
			reply.Error = "La console refuse le jeton : redémarrez-la (sudo systemctl restart pi-appliance), puis réessayez."
		}
		return apiError{status: response.StatusCode, message: reply.Error, body: data}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// health : version de la console en marche, sans jeton ; vide si elle ne répond pas.
func health(ctx context.Context, env Env) string {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, env.ConsoleURL+"/api/health", nil)
	if err != nil {
		return ""
	}
	response, err := env.HTTP.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	var reply struct {
		Version string `json:"version"`
	}
	if json.NewDecoder(response.Body).Decode(&reply) != nil {
		return ""
	}
	return reply.Version
}

func readSystem(env Env, path string) string {
	data, err := os.ReadFile(env.Root + path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
}

func osName(env Env) string {
	for _, line := range strings.Split(readSystem(env, "/etc/os-release"), "\n") {
		if value, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(value, `"`)
		}
	}
	return ""
}

func orUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "inconnu"
	}
	return strings.TrimSpace(value)
}

func commandOutput(ctx context.Context, env Env, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := env.Command(ctx, name, args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

// version : version du socle.
func version(env Env, args []string) error {
	if len(args) > 0 {
		return usageError{"pia version ne prend pas d'argument."}
	}
	ctx := context.Background()
	running := health(ctx, env)
	console := "ne répond pas"
	if running != "" {
		console = running
	}
	fmt.Fprintf(env.Out, "pi-appliance : %s (console en marche : %s)\n", env.Version, console)
	fmt.Fprintf(env.Out, "Système : %s, noyau %s\n", orUnknown(osName(env)), orUnknown(readSystem(env, "/proc/sys/kernel/osrelease")))
	fmt.Fprintf(env.Out, "Matériel : %s\n", orUnknown(readSystem(env, "/proc/device-tree/model")))
	fmt.Fprintf(env.Out, "Docker : %s\n", orUnknown(commandOutput(ctx, env, "docker", "version", "--format", "{{.Server.Version}}")))
	fmt.Fprintf(env.Out, "Traefik : %s\n", orUnknown(commandOutput(ctx, env, "docker", "inspect", "--format", "{{.Config.Image}}", "pi-appliance-traefik")))
	return nil
}

type service struct {
	Name    string `json:"name"`
	App     string `json:"app"`
	AppName string `json:"app_name"`
	Address string `json:"address"`
	Access  string `json:"access"`
	NoWeb   bool   `json:"no_web_page"`
	Update  bool   `json:"update_available"`
	Status  struct {
		Color   string `json:"color"`
		Message string `json:"message"`
	} `json:"status"`
}

var colorWords = map[string]string{"green": "en marche", "orange": "à surveiller", "red": "arrêté ou en panne"}

func services(ctx context.Context, env Env) ([]service, error) {
	var reply struct {
		Services []service `json:"services"`
	}
	err := call(ctx, env, http.MethodGet, "services", nil, &reply)
	return reply.Services, err
}

// state : la console répond-elle, et l'état des services en une phrase.
func state(env Env, args []string) error {
	if len(args) > 0 {
		return usageError{"pia etat ne prend pas d'argument."}
	}
	ctx := context.Background()
	running := health(ctx, env)
	if running == "" {
		return errors.New("La console ne répond pas : sudo systemctl status pi-appliance")
	}
	fmt.Fprintf(env.Out, "Console en service (version %s).\n", running)
	items, err := services(ctx, env)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Status.Color]++
	}
	fmt.Fprintf(env.Out, "%d service(s) installé(s) : %d en marche, %d à surveiller, %d arrêté(s) ou en panne.\n", len(items), counts["green"], counts["orange"], counts["red"])
	return nil
}

func list(env Env, args []string) error {
	if len(args) > 0 {
		return usageError{"pia services ne prend pas d'argument."}
	}
	items, err := services(context.Background(), env)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(env.Out, "Aucun service installé. Voir : pia catalogue")
		return nil
	}
	for _, item := range items {
		address := "https://" + item.Address
		if item.NoWeb || item.Address == "" {
			address = "(pas de page web)"
		}
		line := fmt.Sprintf("%-16s %-14s %-18s %s", item.Name, item.AppName, orUnknown(colorWords[item.Status.Color]), address)
		if item.Update {
			line += "  (mise à jour disponible)"
		}
		fmt.Fprintln(env.Out, line)
		if item.Status.Message != "" && item.Status.Color != "green" {
			fmt.Fprintf(env.Out, "%16s %s\n", "", item.Status.Message)
		}
	}
	return nil
}

type app struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Subdomain   string `json:"subdomain"`
	Access      string `json:"access"`
	MemoryMB    int    `json:"memory_mb"`
	DiskMB      int    `json:"disk_mb"`
	Parameters  []struct {
		Name     string `json:"name"`
		Label    string `json:"label"`
		Generate bool   `json:"generate"`
		Default  string `json:"default"`
	} `json:"parameters"`
}

func apps(ctx context.Context, env Env) ([]app, error) {
	var reply struct {
		Apps []app `json:"apps"`
	}
	err := call(ctx, env, http.MethodGet, "catalog", nil, &reply)
	sort.Slice(reply.Apps, func(i, j int) bool { return reply.Apps[i].ID < reply.Apps[j].ID })
	return reply.Apps, err
}

func catalogue(env Env, args []string) error {
	if len(args) > 1 {
		return usageError{"pia catalogue prend au plus le nom d'une application."}
	}
	items, err := apps(context.Background(), env)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		for _, item := range items {
			fmt.Fprintf(env.Out, "%-12s %-22s %s\n", item.ID, item.Name, firstSentence(item.Description))
		}
		fmt.Fprintln(env.Out, "\nDétail : pia catalogue <application> ; installation : pia installer <application>")
		return nil
	}
	for _, item := range items {
		if item.ID != args[0] {
			continue
		}
		fmt.Fprintf(env.Out, "%s (%s)\n%s\n\n", item.Name, item.ID, strings.TrimSpace(item.Description))
		fmt.Fprintf(env.Out, "Nom proposé : %s ; mémoire : %d Mo ; disque : %d Mo ; accès par défaut : %s\n", item.Subdomain, item.MemoryMB, item.DiskMB, orUnknown(item.Access))
		for _, parameter := range item.Parameters {
			note := ""
			switch {
			case parameter.Generate:
				note = " (créé par le Pi si absent)"
			case parameter.Default != "":
				note = " (par défaut : " + parameter.Default + ")"
			}
			fmt.Fprintf(env.Out, "  --param %s=…   %s%s\n", parameter.Name, parameter.Label, note)
		}
		return nil
	}
	return fmt.Errorf("Application inconnue : %s. Voir : pia catalogue", args[0])
}

func firstSentence(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if i := strings.Index(text, ". "); i > 0 {
		text = text[:i+1]
	}
	if runes := []rune(text); len(runes) > 70 {
		text = string(runes[:69]) + "…"
	}
	return text
}

var stepWords = map[string]string{
	"prepare": "Préparation", "download": "Téléchargement", "create": "Création",
	"start": "Démarrage", "check": "Vérification", "done": "Terminé",
}

// install : installe une application et suit l'avancement.
func install(env Env, args []string) error {
	f, err := parseFlags(args, []string{"nom", "acces", "param"}, []string{"confirmer"})
	if err != nil {
		return err
	}
	if len(f.positional) != 1 {
		return usageError{"Indiquez une application : pia installer <application> (voir pia catalogue)."}
	}
	ctx := context.Background()
	items, err := apps(ctx, env)
	if err != nil {
		return err
	}
	var chosen *app
	for i := range items {
		if items[i].ID == f.positional[0] {
			chosen = &items[i]
		}
	}
	if chosen == nil {
		return fmt.Errorf("Application inconnue : %s. Voir : pia catalogue", f.positional[0])
	}
	name := f.value("nom")
	if name == "" {
		name = chosen.Subdomain
	}
	parameters := map[string]string{}
	for _, pair := range f.values["param"] {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return usageError{"Paramètre à écrire --param nom=valeur : " + pair}
		}
		parameters[key] = value
	}
	request := map[string]any{"app": chosen.ID, "name": name, "parameters": parameters, "confirm_risks": f.set["confirmer"]}
	if access := f.value("acces"); access != "" {
		request["access"] = access
	}
	fmt.Fprintf(env.Out, "Installation de %s sous le nom « %s »…\n", chosen.Name, name)
	done := make(chan struct{})
	go followProgress(ctx, env, name, done)
	var result struct {
		Instance service           `json:"instance"`
		Warnings []string          `json:"warnings"`
		Secrets  map[string]string `json:"secrets"`
	}
	err = call(ctx, env, http.MethodPost, "services", request, &result)
	close(done)
	var failure apiError
	if errors.As(err, &failure) && failure.status == http.StatusConflict {
		var risks struct {
			Risks []string `json:"risks"`
		}
		if json.Unmarshal(failure.body, &risks) == nil && len(risks.Risks) > 0 {
			return fmt.Errorf("%s\n  - %s\nRelancez avec --confirmer si vous acceptez.", failure.message, strings.Join(risks.Risks, "\n  - "))
		}
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "%s est installé.\n", chosen.Name)
	if result.Instance.Address != "" && !result.Instance.NoWeb {
		fmt.Fprintf(env.Out, "Adresse : https://%s\n", result.Instance.Address)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(env.Out, "Attention : "+warning)
	}
	if len(result.Secrets) > 0 {
		fmt.Fprintln(env.Out, "Mots de passe créés par le Pi, à noter maintenant (ils ne seront plus affichés) :")
		keys := make([]string, 0, len(result.Secrets))
		for key := range result.Secrets {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(env.Out, "  %s : %s\n", key, result.Secrets[key])
		}
	}
	return nil
}

// followProgress affiche chaque nouvelle étape de l'installation.
func followProgress(ctx context.Context, env Env, name string, done <-chan struct{}) {
	last := ""
	for {
		select {
		case <-done:
			return
		case <-time.After(time.Second):
		}
		var progress struct {
			Step     string `json:"step"`
			Images   int    `json:"images"`
			Image    int    `json:"image"`
			Received int64  `json:"received_bytes"`
			Total    int64  `json:"total_bytes"`
		}
		if call(ctx, env, http.MethodGet, "services/"+url.PathEscape(name)+"/progress", nil, &progress) != nil {
			continue
		}
		line := orUnknown(stepWords[progress.Step])
		if progress.Step == "download" && progress.Images > 0 {
			line = fmt.Sprintf("%s (image %d sur %d)", line, progress.Image, progress.Images)
		}
		if line != last {
			fmt.Fprintln(env.Out, "  "+line+"…")
			last = line
		}
	}
}

func uninstall(env Env, args []string) error {
	f, err := parseFlags(args, nil, []string{"effacer-donnees", "oui"})
	if err != nil {
		return err
	}
	if len(f.positional) != 1 {
		return usageError{"Indiquez un service : pia desinstaller <service> (voir pia services)."}
	}
	name := f.positional[0]
	data := "keep"
	if f.set["effacer-donnees"] {
		data = "delete"
		if !f.set["oui"] && !confirm(env, fmt.Sprintf("Désinstaller %s et EFFACER ses données ? Elles seront perdues, sauf sauvegarde.", name)) {
			return errors.New("Rien n'a été fait.")
		}
	}
	if err := call(context.Background(), env, http.MethodDelete, "services/"+url.PathEscape(name)+"?data="+data, nil, nil); err != nil {
		return err
	}
	if data == "keep" {
		fmt.Fprintf(env.Out, "%s est désinstallé ; ses données sont gardées sur le Pi (une nouvelle installation sous le même nom les retrouve).\n", name)
	} else {
		fmt.Fprintf(env.Out, "%s est désinstallé et ses données sont effacées.\n", name)
	}
	return nil
}

var actionWords = map[string]string{"start": "démarré", "stop": "arrêté", "restart": "redémarré"}

func control(env Env, args []string, action string) error {
	if len(args) != 1 {
		return usageError{"Indiquez un service (voir pia services)."}
	}
	var status struct {
		Message string `json:"message"`
	}
	if err := call(context.Background(), env, http.MethodPost, "services/"+url.PathEscape(args[0])+"/"+action, nil, &status); err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "%s est %s. %s\n", args[0], actionWords[action], status.Message)
	return nil
}
