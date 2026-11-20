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

// Package stats construit et envoie les statistiques anonymes de l'appliance
// (spécification 8.4, 10.2) : désactivées par défaut, envoyées au plus une fois
// par jour si l'administrateur les accepte. L'envoi contient la version, le
// modèle de Pi, les applications de la marketplace installées et les plantages
// de la console ; l'administrateur voit exactement ce qui part (Preview).
//
// NE PAS ajouter à Report d'adresse IP, de nom (compte, service, Wi-Fi),
// d'adresse de l'appliance, de contenu, ni d'identifiant stable : rien ne doit
// permettre de relier deux envois entre eux, à la clé du Pi ou à l'adresse
// e-mail de l'administrateur. L'envoi n'est pas signé avec la clé du Pi.
package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultURL : service de réception du projet (cmd/pi-appliance-stats).
	DefaultURL = "https://stats.pi-appliance.com/api/report"
	// DefaultModelFile : modèle du Pi, écrit par le micrologiciel.
	DefaultModelFile = "/proc/device-tree/model"
	// Format : version du format de Report.
	Format = 1
	// Period : un envoi réussi au plus par période.
	Period = 24 * time.Hour
	// Interval : délai entre deux vérifications ; un envoi échoué est retenté à la suivante.
	Interval = time.Hour
	// UserAgent : le même pour toutes les appliances, sans version.
	UserAgent = "pi-appliance"
)

// Bornes d'un envoi, vérifiées aussi par le service de réception.
const (
	MaxServices = 100
	MaxCrashes  = 20
	MaxBytes    = 16 << 10
)

// Report : tout ce qui part, et rien d'autre.
type Report struct {
	Format  int    `json:"format"`
	Version string `json:"version"`
	Model   string `json:"model"`
	// Services : identifiants des applications de la marketplace installées
	// (« nextcloud »), triés, sans le nom donné au service.
	Services []string `json:"services"`
	// CustomServices : nombre de services installés hors marketplace, sans
	// rien de leur fichier Docker Compose.
	CustomServices int `json:"custom_services"`
	// Crashes : plantages de la console depuis le dernier envoi.
	Crashes []Crash `json:"crashes"`
}

// Crash : où la console a planté (fonction du code de pi-appliance, ou
// CrashUnclean), et combien de fois. Jamais le message d'erreur, qui peut
// contenir un nom ou un contenu.
type Crash struct {
	Where string `json:"where"`
	Count int    `json:"count"`
}

// CrashUnclean : la console s'est arrêtée sans passer par sa fin normale
// (plantage hors d'une requête, coupure de courant, manque de mémoire).
const CrashUnclean = "arret-brutal"

var (
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,39}$`)
	modelPattern   = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z .+()/-]{0,79}$`)
	appPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	wherePattern   = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z_./()*-]{0,119}$`)
)

// ErrInvalid : envoi hors du format attendu.
var ErrInvalid = errors.New("statistiques invalides")

// Check vérifie qu'un envoi respecte le format, champ par champ : le service
// de réception refuse tout le reste.
func (r Report) Check() error {
	invalid := func(what string) error { return fmt.Errorf("%w : %s", ErrInvalid, what) }
	switch {
	case r.Format != Format:
		return invalid("format")
	case !versionPattern.MatchString(r.Version):
		return invalid("version")
	case r.Model != "" && !modelPattern.MatchString(r.Model):
		return invalid("modèle")
	case r.Services == nil || len(r.Services) > MaxServices:
		return invalid("services")
	case r.CustomServices < 0 || r.CustomServices > MaxServices:
		return invalid("services hors marketplace")
	case r.Crashes == nil || len(r.Crashes) > MaxCrashes:
		return invalid("plantages")
	}
	for _, app := range r.Services {
		if !appPattern.MatchString(app) {
			return invalid("services")
		}
	}
	for _, crash := range r.Crashes {
		if !wherePattern.MatchString(crash.Where) || crash.Count < 1 || crash.Count > 1_000_000 {
			return invalid("plantages")
		}
	}
	return nil
}

// Doer envoie une requête HTTP ; *http.Client en est un, les tests le remplacent.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Status : état montré à l'administrateur, avec l'envoi tel qu'il partirait.
type Status struct {
	Enabled bool `json:"enabled"`
	// Decided : l'administrateur a déjà répondu (l'assistant ne repose pas la question).
	Decided     bool      `json:"decided"`
	LastSent    time.Time `json:"last_sent,omitzero"`
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	NextAttempt time.Time `json:"next_attempt,omitzero"`
	Error       string    `json:"error,omitempty"`
	Report      Report    `json:"report"`
}

// state : fichier des statistiques sur le Pi.
type state struct {
	Enabled  bool           `json:"enabled"`
	Decided  bool           `json:"decided"`
	LastSent time.Time      `json:"last_sent,omitzero"`
	Crashes  map[string]int `json:"crashes,omitempty"`
}

// Reporter tient le réglage, les plantages et l'envoi quotidien.
type Reporter struct {
	// Path : fichier du réglage et des plantages pas encore envoyés.
	Path string
	// RunningFile : présent pendant que la console tourne ; retrouvé au
	// démarrage, il signale un arrêt brutal. Vide : pas de détection.
	RunningFile string
	Version     string
	ModelFile   string
	// Installed renvoie les identifiants d'application des services installés
	// et dit lesquels viennent de la marketplace ; nil : aucun service.
	Installed func() (apps []string, known func(string) bool, err error)
	Client    Doer
	URL       string
	Now       func() time.Time
	Logf      func(string, ...any)

	mu          sync.Mutex
	state       state
	lastAttempt time.Time
	nextAttempt time.Time
	lastError   string
	wake        chan struct{}
}

// Open lit le réglage s'il existe ; désactivé sinon.
func Open(path string) (*Reporter, error) {
	r := &Reporter{Path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &r.state); err != nil {
		return nil, fmt.Errorf("%s illisible : %w", path, err)
	}
	return r, nil
}

// Start note un arrêt brutal de la fois précédente et pose RunningFile.
func (r *Reporter) Start() error {
	if r.RunningFile == "" {
		return nil
	}
	if _, err := os.Stat(r.RunningFile); err == nil {
		r.record(CrashUnclean)
	}
	return os.WriteFile(r.RunningFile, nil, 0o600)
}

// Stop retire RunningFile : la console s'arrête normalement.
func (r *Reporter) Stop() error {
	if r.RunningFile == "" {
		return nil
	}
	if err := os.Remove(r.RunningFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// SetEnabled enregistre la réponse de l'administrateur. Les plantages notés
// avant l'accord ne partent pas ; un premier envoi suit peu après l'accord.
func (r *Reporter) SetEnabled(enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.state
	next.Decided = true
	if enabled != next.Enabled {
		next.Crashes = nil
	}
	next.Enabled = enabled
	if err := writeFile(r.Path, next); err != nil {
		return err
	}
	r.state = next
	r.lastError = ""
	r.nextAttempt = time.Time{}
	if enabled {
		r.wakeUp()
	}
	return nil
}

// Status renvoie le réglage et l'envoi tel qu'il partirait maintenant.
func (r *Reporter) Status() Status {
	report := r.Preview()
	r.mu.Lock()
	defer r.mu.Unlock()
	status := Status{
		Enabled:  r.state.Enabled,
		Decided:  r.state.Decided,
		LastSent: r.state.LastSent,
		Error:    r.lastError,
		Report:   report,
	}
	if r.state.Enabled {
		status.LastAttempt = r.lastAttempt
		status.NextAttempt = r.nextAttempt
	}
	return status
}

// Preview construit l'envoi, exactement comme il partirait.
func (r *Reporter) Preview() Report {
	report := Report{
		Format:   Format,
		Version:  r.Version,
		Model:    r.model(),
		Services: []string{},
		Crashes:  []Crash{},
	}
	if !versionPattern.MatchString(report.Version) {
		report.Version = "inconnue"
	}
	if r.Installed != nil {
		apps, known, err := r.Installed()
		if err != nil && r.Logf != nil {
			r.Logf("statistiques : services illisibles : %v", err)
		}
		for _, app := range apps {
			if known != nil && known(app) && appPattern.MatchString(app) {
				report.Services = append(report.Services, app)
			} else {
				report.CustomServices++
			}
		}
		slices.Sort(report.Services)
		if len(report.Services) > MaxServices {
			report.Services = report.Services[:MaxServices]
		}
		report.CustomServices = min(report.CustomServices, MaxServices)
	}
	r.mu.Lock()
	for where, count := range r.state.Crashes {
		report.Crashes = append(report.Crashes, Crash{Where: where, Count: count})
	}
	r.mu.Unlock()
	slices.SortFunc(report.Crashes, func(a, b Crash) int { return strings.Compare(a.Where, b.Where) })
	if len(report.Crashes) > MaxCrashes {
		report.Crashes = report.Crashes[:MaxCrashes]
	}
	return report
}

// model lit le modèle du Pi (« Raspberry Pi 4 Model B Rev 1.4 ») ; vide si inconnu.
// NE PAS lire le numéro de série : il identifierait le Pi.
func (r *Reporter) model() string {
	path := r.ModelFile
	if path == "" {
		path = DefaultModelFile
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	model := strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
	if !modelPattern.MatchString(model) {
		return ""
	}
	return model
}

// Guard note les plantages des requêtes de la console, puis laisse net/http
// les traiter comme d'habitude (journal, connexion fermée).
func (r *Reporter) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				if value != http.ErrAbortHandler {
					r.record(crashSite())
				}
				panic(value)
			}
		}()
		next.ServeHTTP(w, request)
	})
}

// modulePrefix : seules les fonctions de pi-appliance sont nommées dans un plantage.
const modulePrefix = "github.com/gmoulard/pi-appliance/"

// crashSite renvoie la fonction de pi-appliance la plus proche du plantage,
// sans son chemin de module (« internal/services.(*Manager).Install »).
func crashSite() string {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	for {
		frame, more := frames.Next()
		name := strings.TrimPrefix(frame.Function, modulePrefix)
		if name != frame.Function && !strings.HasPrefix(name, "internal/stats.") && wherePattern.MatchString(name) {
			return name
		}
		if !more {
			return "inconnu"
		}
	}
}

// record compte un plantage, seulement si les statistiques sont acceptées.
func (r *Reporter) record(where string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.state.Enabled {
		return
	}
	next := r.state
	next.Crashes = make(map[string]int, len(r.state.Crashes)+1)
	for key, count := range r.state.Crashes {
		next.Crashes[key] = count
	}
	if _, found := next.Crashes[where]; !found && len(next.Crashes) >= MaxCrashes {
		return
	}
	next.Crashes[where]++
	if err := writeFile(r.Path, next); err != nil {
		if r.Logf != nil {
			r.Logf("statistiques : %v", err)
		}
		return
	}
	r.state = next
}

func (r *Reporter) wakeUp() {
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run envoie les statistiques jusqu'à l'arrêt de ctx. La première vérification
// attend un délai tiré au hasard, pour que l'envoi ne suive pas toujours le
// démarrage ou les autres échanges avec le projet.
func (r *Reporter) Run(ctx context.Context) {
	r.mu.Lock()
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	wake := r.wake
	r.mu.Unlock()
	timer := time.NewTimer(jitter())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			r.Send(ctx)
			timer.Reset(Interval)
		case <-wake:
			timer.Reset(jitter())
		}
	}
}

// jitter : entre 5 et 65 minutes.
func jitter() time.Duration {
	return 5*time.Minute + rand.N(time.Hour)
}

// Send envoie les statistiques si elles sont acceptées et si le dernier envoi
// réussi date d'au moins Period. Elle dit si un envoi a réussi.
func (r *Reporter) Send(ctx context.Context) bool {
	now := r.now()
	r.mu.Lock()
	due := r.state.Enabled && now.Sub(r.state.LastSent) >= Period
	r.mu.Unlock()
	if !due {
		return false
	}
	report := r.Preview()
	err := report.Check()
	if err == nil {
		err = r.post(ctx, report)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastAttempt = now
	r.nextAttempt = now.Add(Interval)
	if !r.state.Enabled {
		return false
	}
	if err != nil {
		r.lastError = err.Error()
		if r.Logf != nil {
			r.Logf("statistiques : %v", err)
		}
		return false
	}
	next := r.state
	next.LastSent = now
	next.Crashes = nil
	// Les plantages notés pendant l'envoi restent pour le suivant.
	for where, count := range r.state.Crashes {
		for _, sent := range report.Crashes {
			if sent.Where == where {
				count -= sent.Count
			}
		}
		if count > 0 {
			if next.Crashes == nil {
				next.Crashes = map[string]int{}
			}
			next.Crashes[where] = count
		}
	}
	if err := writeFile(r.Path, next); err != nil && r.Logf != nil {
		r.Logf("statistiques : %v", err)
	}
	r.state = next
	r.lastError = ""
	r.nextAttempt = now.Add(Period)
	return true
}

// post envoie le rapport, sans cookie, sans signature et sans autre en-tête
// que le type et un User-Agent commun à toutes les appliances.
func (r *Reporter) post(ctx context.Context, report Report) error {
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	address := r.URL
	if address == "" {
		address = DefaultURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return errors.New("adresse du service invalide")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", UserAgent)
	response, err := r.client().Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return errors.New("le service des statistiques ne répond pas")
		}
		return errors.New("pas de connexion à internet")
	}
	defer response.Body.Close()
	io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("le service des statistiques a répondu %d", response.StatusCode)
	}
	return nil
}

func (r *Reporter) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reporter) client() Doer {
	if r.Client != nil {
		return r.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func writeFile(path string, value state) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
