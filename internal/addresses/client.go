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

package addresses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	// ErrUnreachable : le service d'adresses ne répond pas.
	ErrUnreachable = errors.New("le service d'adresses ne répond pas, réessayez plus tard")
	// ErrNoTarget : pas encore de nom DuckDNS vers lequel faire pointer l'adresse.
	ErrNoTarget = errors.New("réglez d'abord DuckDNS : votre adresse pointera vers votre nom DuckDNS")
	// ErrNotReserved : l'appliance n'a pas d'adresse.
	ErrNotReserved = errors.New("cette appliance n'a pas d'adresse pi-appliance.com")
)

// ServiceError : refus du service d'adresses, avec son message en langage courant.
type ServiceError struct {
	Status  int
	Message string
}

func (e *ServiceError) Error() string { return e.Message }

// Doer envoie une requête HTTP ; *http.Client en est un, les tests le remplacent.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// State : adresse de l'appliance, gardée sur le Pi et montrée à l'administrateur.
type State struct {
	Name     string    `json:"name,omitempty"`
	Hostname string    `json:"hostname,omitempty"`
	Target   string    `json:"target,omitempty"`
	Reserved time.Time `json:"reserved,omitzero"`
	LastSync time.Time `json:"last_sync,omitzero"`
	Error    string    `json:"error,omitempty"`
}

// Client : côté console du service d'adresses. Il réserve, libère, et tient la
// cible de l'alias à jour avec le nom DuckDNS de l'appliance (spécification 7.1).
type Client struct {
	URL    string
	Key    *Key
	Path   string
	Target func() string // nom DuckDNS actuel, "" s'il n'est pas réglé
	HTTP   Doer
	Now    func() time.Time
	Logf   func(string, ...any)

	mu    sync.Mutex
	state State
	wake  chan struct{}
}

// OpenClient lit l'état gardé dans path (absent : pas encore d'adresse).
func OpenClient(path string, key *Key) (*Client, error) {
	c := &Client{URL: DefaultServiceURL, Key: key, Path: path, wake: make(chan struct{}, 1)}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &c.state); err != nil {
		return nil, fmt.Errorf("%s illisible : %w", path, err)
	}
	return c, nil
}

// Status renvoie l'état de l'adresse.
func (c *Client) Status() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Check dit si un nom suit les règles et s'il est libre.
func (c *Client) Check(ctx context.Context, name string) (Availability, error) {
	name = Normalize(name)
	if err := Check(name); err != nil {
		return Availability{Name: name, Reason: strings.TrimPrefix(err.Error(), ErrInvalid.Error()+" : ")}, nil
	}
	var result Availability
	err := c.call(ctx, http.MethodGet, "/api/names/"+url.PathEscape(name), nil, false, &result)
	return result, err
}

// Reserve réserve name, alias vers le nom DuckDNS actuel.
func (c *Client) Reserve(ctx context.Context, name string) (State, error) {
	name = Normalize(name)
	if err := Check(name); err != nil {
		return State{}, err
	}
	target := c.target()
	if target == "" {
		return State{}, ErrNoTarget
	}
	var record Record
	if err := c.call(ctx, http.MethodPost, "/api/names", Request{Name: name, Target: target}, true, &record); err != nil {
		return State{}, err
	}
	return c.store(record, "")
}

// Release libère l'adresse de l'appliance.
func (c *Client) Release(ctx context.Context) error {
	state := c.Status()
	if state.Name == "" {
		return ErrNotReserved
	}
	err := c.call(ctx, http.MethodDelete, "/api/names/"+url.PathEscape(state.Name), nil, true, nil)
	var refused *ServiceError
	if err != nil && !(errors.As(err, &refused) && refused.Status == http.StatusNotFound) {
		return err
	}
	_, err = c.store(Record{}, "")
	return err
}

// Sync envoie la cible actuelle au service ; c'est aussi le signe de vie.
func (c *Client) Sync(ctx context.Context) error {
	state := c.Status()
	if state.Name == "" {
		return ErrNotReserved
	}
	target := c.target()
	if target == "" {
		// DuckDNS effacé : l'alias garde son ancienne cible, le signe de vie part quand même.
		target = state.Target
	}
	var record Record
	err := c.call(ctx, http.MethodPut, "/api/names/"+url.PathEscape(state.Name), Request{Target: target}, true, &record)
	var refused *ServiceError
	switch {
	case errors.As(err, &refused) && refused.Status == http.StatusNotFound:
		_, storeErr := c.store(Record{}, fmt.Sprintf("L'adresse %s a été libérée par le service d'adresses.", state.Hostname))
		return errors.Join(err, storeErr)
	case err != nil:
		c.setError(err.Error())
		return err
	}
	_, err = c.store(record, "")
	return err
}

// Recover reprend l'adresse connue du service quand le Pi l'a oubliée
// (fichier d'état perdu, clé restaurée d'une sauvegarde).
func (c *Client) Recover(ctx context.Context) error {
	if c.Status().Name != "" {
		return nil
	}
	var record Record
	err := c.call(ctx, http.MethodGet, "/api/mine", nil, true, &record)
	var refused *ServiceError
	if errors.As(err, &refused) && refused.Status == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = c.store(record, "")
	return err
}

// WakeUp demande une synchronisation tout de suite (nouveau nom DuckDNS).
func (c *Client) WakeUp() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Run tient l'adresse à jour jusqu'à la fin de ctx : cible changée, ou signe de vie chaque jour.
func (c *Client) Run(ctx context.Context, interval time.Duration) {
	if err := c.Recover(ctx); err != nil {
		c.logf("adresse : %v", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		state := c.Status()
		target := c.target()
		due := state.Name != "" && (target != "" && target != state.Target || c.now().Sub(state.LastSync) >= Heartbeat || state.Error != "")
		if due {
			if err := c.Sync(ctx); err != nil {
				c.logf("adresse : %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-c.wake:
		}
	}
}

func (c *Client) target() string {
	if c.Target == nil {
		return ""
	}
	return c.Target()
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *Client) setError(message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state.Error = message
}

// store garde l'adresse reçue du service (Record vide : plus d'adresse).
func (c *Client) store(record Record, message string) (State, error) {
	state := State{Error: message}
	if record.Name != "" {
		state = State{Name: record.Name, Hostname: Hostname(record.Name), Target: record.Target, Reserved: record.Reserved, LastSync: c.now()}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return State{}, err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return State{}, err
	}
	temporary := c.Path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return State{}, err
	}
	if err := os.Rename(temporary, c.Path); err != nil {
		return State{}, err
	}
	c.mu.Lock()
	c.state = state
	c.mu.Unlock()
	return state, nil
}

// call envoie une demande au service, signée si signed, et décode la réponse dans result.
func (c *Client) call(ctx context.Context, method, path string, body any, signed bool, result any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.URL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if signed {
		c.Key.Sign(request, payload, c.now())
	}
	doer := c.HTTP
	if doer == nil {
		doer = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := doer.Do(request)
	if err != nil {
		c.logf("service d'adresses : %v", err)
		return ErrUnreachable
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return ErrUnreachable
	}
	if response.StatusCode >= 500 && response.StatusCode != http.StatusServiceUnavailable {
		return ErrUnreachable
	}
	if response.StatusCode >= 300 {
		var refused ErrorBody
		if json.Unmarshal(data, &refused) != nil || refused.Error == "" {
			return ErrUnreachable
		}
		return &ServiceError{Status: response.StatusCode, Message: refused.Error}
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.Unmarshal(data, result); err != nil {
		return ErrUnreachable
	}
	return nil
}
