// This file is part of pi-appliance <https://github.com/gmoulard/pi-appliance>.
// Copyright (c) 2026 Guillaume Moulard.
//
// Licensed under the Creative Commons Attribution-NonCommercial-NoDerivatives
// 4.0 International License (CC BY-NC-ND 4.0), with the additional permissions
// of the LICENSE file at the root of the project: this file may be shared and
// integrated into another project unmodified, keeping this notice and a link to
// the project; modified versions may only be published to propose them to the
// official repository. Commercial use requires a separate licence from the
// author: guillaume@moulard.org.
//
// See <https://creativecommons.org/licenses/by-nc-nd/4.0/> and the LICENSE file.

// Package ddns tient à jour le nom DuckDNS de l'appliance avec l'adresse
// publique de la box (spécification 7.1) : <nom>.pi-appliance.com est un alias
// vers <identifiant>.duckdns.org.
package ddns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultUpdateURL : API publique de DuckDNS.
	DefaultUpdateURL = "https://www.duckdns.org/update"
	// DefaultLookupURL renvoie l'adresse IPv4 publique de la box, en texte brut.
	DefaultLookupURL = "https://api.ipify.org"
	// Interval : délai entre deux lectures de l'adresse publique.
	Interval = 5 * time.Minute
	// Refresh : DuckDNS est mis à jour au moins une fois par jour, même sans changement d'adresse.
	Refresh = 24 * time.Hour
	// Le délai après un échec double à chaque échec, de RetryMin à RetryMax.
	RetryMin = time.Minute
	RetryMax = time.Hour
)

var (
	ErrInvalid = errors.New("réglage invalide")
	ErrRefused = errors.New("DuckDNS a refusé la mise à jour : vérifiez l'identifiant et le jeton")
)

// Doer envoie une requête HTTP ; *http.Client en est un, les tests le remplacent.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Settings : réglages DuckDNS, gardés sur le Pi seulement.
// NE PAS journaliser ni renvoyer Token : il donne la main sur le nom DuckDNS.
type Settings struct {
	Domain string `json:"domain"`
	Token  string `json:"token"`
}

// Status : état montré à l'administrateur, sans le jeton.
type Status struct {
	Configured  bool      `json:"configured"`
	Domain      string    `json:"domain,omitempty"`
	Hostname    string    `json:"hostname,omitempty"`
	IP          string    `json:"ip,omitempty"`
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	LastSuccess time.Time `json:"last_success,omitzero"`
	NextAttempt time.Time `json:"next_attempt,omitzero"`
	Error       string    `json:"error,omitempty"`
}

// Updater tient à jour le nom DuckDNS. Path est le fichier des réglages (droits 600).
type Updater struct {
	Path      string
	Client    Doer
	UpdateURL string
	LookupURL string
	Now       func() time.Time
	Logf      func(string, ...any)

	mu       sync.Mutex
	settings Settings
	status   Status
	sentIP   string
	failures int
	wake     chan struct{}
}

// Open lit les réglages s'ils existent.
func Open(path string) (*Updater, error) {
	u := &Updater{Path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return u, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &u.settings); err != nil {
		return nil, fmt.Errorf("%s illisible : %w", path, err)
	}
	return u, nil
}

var (
	domainPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	tokenPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// Check vérifie et normalise les réglages : l'identifiant peut être donné avec
// « .duckdns.org », le jeton est celui affiché sur duckdns.org.
func (s Settings) Check() (Settings, error) {
	s.Domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s.Domain)), ".duckdns.org")
	s.Token = strings.ToLower(strings.TrimSpace(s.Token))
	if !domainPattern.MatchString(s.Domain) {
		return Settings{}, fmt.Errorf("%w : l'identifiant DuckDNS ne contient que des lettres minuscules, des chiffres et des tirets", ErrInvalid)
	}
	if !tokenPattern.MatchString(s.Token) {
		return Settings{}, fmt.Errorf("%w : le jeton DuckDNS a la forme xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx", ErrInvalid)
	}
	return s, nil
}

// Save enregistre de nouveaux réglages et demande une mise à jour immédiate.
func (u *Updater) Save(settings Settings) error {
	settings, err := settings.Check()
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := writeFile(u.Path, settings); err != nil {
		return err
	}
	u.settings = settings
	u.status = Status{}
	u.sentIP = ""
	u.failures = 0
	u.wakeUp()
	return nil
}

// Forget efface les réglages : l'appliance ne met plus DuckDNS à jour.
func (u *Updater) Forget() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if err := os.Remove(u.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	u.settings = Settings{}
	u.status = Status{}
	u.sentIP = ""
	u.failures = 0
	return nil
}

// Status renvoie l'état de la dernière mise à jour.
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	status := u.status
	status.Configured = u.settings.Domain != ""
	if status.Configured {
		status.Domain = u.settings.Domain
		status.Hostname = u.settings.Domain + ".duckdns.org"
	}
	return status
}

func (u *Updater) wakeUp() {
	if u.wake == nil {
		u.wake = make(chan struct{}, 1)
	}
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

// Run vérifie l'adresse publique jusqu'à l'arrêt de ctx.
func (u *Updater) Run(ctx context.Context) {
	u.mu.Lock()
	if u.wake == nil {
		u.wake = make(chan struct{}, 1)
	}
	wake := u.wake
	u.mu.Unlock()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-wake:
			timer.Stop()
		}
		timer.Reset(u.Update(ctx))
	}
}

// Update lit l'adresse publique et met DuckDNS à jour si elle a changé, si la
// dernière mise à jour date de plus d'un jour ou si la précédente a échoué.
// Elle renvoie le délai avant la prochaine vérification.
func (u *Updater) Update(ctx context.Context) time.Duration {
	u.mu.Lock()
	settings := u.settings
	sentIP := u.sentIP
	lastSuccess := u.status.LastSuccess
	u.mu.Unlock()
	if settings.Domain == "" {
		return Interval
	}
	now := u.now()
	ip, err := u.lookup(ctx)
	if err == nil {
		if ip == sentIP && now.Sub(lastSuccess) < Refresh {
			return u.finish(settings, now, ip, nil, false)
		}
		err = u.send(ctx, settings, ip)
	}
	return u.finish(settings, now, ip, err, true)
}

// finish note le résultat, sauf si les réglages ont changé entre-temps.
func (u *Updater) finish(settings Settings, now time.Time, ip string, err error, sent bool) time.Duration {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.settings != settings {
		return 0
	}
	delay := Interval
	if err != nil {
		u.failures++
		delay = RetryMin << min(u.failures-1, 6)
		if delay > RetryMax {
			delay = RetryMax
		}
		u.status.LastAttempt = now
		u.status.Error = err.Error()
		if u.Logf != nil {
			u.Logf("DuckDNS : %v (nouvel essai dans %s)", err, delay)
		}
	} else if sent {
		u.failures = 0
		u.sentIP = ip
		u.status.IP = ip
		u.status.LastAttempt = now
		u.status.LastSuccess = now
		u.status.Error = ""
		if u.Logf != nil {
			u.Logf("DuckDNS : %s.duckdns.org pointe vers %s", settings.Domain, ip)
		}
	}
	u.status.NextAttempt = now.Add(delay)
	return delay
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func (u *Updater) client() Doer {
	if u.Client != nil {
		return u.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// PublicIP : adresse IPv4 publique de la box, vue d'internet (test de l'accès
// direct, spécification 7.2).
func (u *Updater) PublicIP(ctx context.Context) (string, error) {
	return u.lookup(ctx)
}

// lookup renvoie l'adresse IPv4 publique de la box.
func (u *Updater) lookup(ctx context.Context) (string, error) {
	address := u.LookupURL
	if address == "" {
		address = DefaultLookupURL
	}
	body, err := u.get(ctx, address)
	if err != nil {
		return "", fmt.Errorf("adresse publique introuvable : %w", err)
	}
	ip := net.ParseIP(firstLine(body))
	if ip == nil || ip.To4() == nil {
		return "", errors.New("adresse publique introuvable : réponse inattendue")
	}
	return ip.String(), nil
}

// send transmet l'adresse à DuckDNS, qui répond OK ou KO.
func (u *Updater) send(ctx context.Context, settings Settings, ip string) error {
	address := u.UpdateURL
	if address == "" {
		address = DefaultUpdateURL
	}
	query := url.Values{"domains": {settings.Domain}, "token": {settings.Token}, "ip": {ip}}
	body, err := u.get(ctx, address+"?"+query.Encode())
	if err != nil {
		return fmt.Errorf("DuckDNS ne répond pas : %w", err)
	}
	switch firstLine(body) {
	case "OK":
		return nil
	case "KO":
		return ErrRefused
	}
	return errors.New("DuckDNS a fait une réponse inattendue")
}

// get lit une petite réponse texte. NE PAS renvoyer l'erreur de Do telle quelle :
// elle contient l'URL, donc le jeton.
func (u *Updater) get(ctx context.Context, address string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", errors.New("adresse du service invalide")
	}
	response, err := u.client().Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "", errors.New("délai dépassé")
		}
		return "", errors.New("pas de connexion à internet")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("code HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	if err != nil {
		return "", errors.New("réponse interrompue")
	}
	return string(data), nil
}

func firstLine(body string) string {
	line, _, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(line)
}

func writeFile(path string, settings Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// Le fichier temporaire a pu exister avec d'autres droits.
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
