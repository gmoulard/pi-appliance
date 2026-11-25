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

package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Client : un service qui s'appuie sur la connexion de la console. Le secret
// n'est montré qu'une fois, à l'enregistrement ; seul son SHA-256 est gardé.
type Client struct {
	ID           string    `json:"id"`
	SecretHash   string    `json:"secret_hash"`
	Service      string    `json:"service"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

// PublicClient : un client sans le hachage de son secret.
type PublicClient struct {
	ID           string    `json:"id"`
	Service      string    `json:"service"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}

// Clients : clients enregistrés, dans <données>/oidc-clients.json (droits 600).
type Clients struct {
	Path string

	mu      sync.Mutex
	loaded  bool
	clients []Client
}

// ErrInvalidClient : enregistrement refusé (service, adresse de retour).
var ErrInvalidClient = errors.New("client OpenID Connect invalide")

func (c *Clients) load() error {
	if c.loaded {
		return nil
	}
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		c.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &c.clients); err != nil {
		return fmt.Errorf("%s : %w", c.Path, err)
	}
	c.loaded = true
	return nil
}

func (c *Clients) save() error {
	data, err := json.MarshalIndent(c.clients, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}

func random(n int) string {
	data := make([]byte, n)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return b64(data)
}

func hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// validRedirect : adresse absolue en https, sans fragment ni identifiant.
func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(raw, " \r\n\t#") && len(raw) <= 400
}

// Register enregistre un client pour un service ; un client déjà enregistré
// pour ce service est remplacé (nouveau secret). Renvoie le client et son
// secret, à donner au service une seule fois.
func (c *Clients) Register(service string, redirects []string, now time.Time) (PublicClient, string, error) {
	if service == "" || len(service) > 63 || len(redirects) == 0 || len(redirects) > 10 {
		return PublicClient{}, "", ErrInvalidClient
	}
	for _, r := range redirects {
		if !validRedirect(r) {
			return PublicClient{}, "", fmt.Errorf("%w : adresse de retour %q", ErrInvalidClient, r)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		return PublicClient{}, "", err
	}
	secret := random(32)
	client := Client{ID: service + "-" + random(9), SecretHash: hash(secret), Service: service, RedirectURIs: slices.Clone(redirects), CreatedAt: now.UTC()}
	kept := slices.DeleteFunc(slices.Clone(c.clients), func(old Client) bool { return old.Service == service })
	previous := c.clients
	c.clients = append(kept, client)
	if err := c.save(); err != nil {
		c.clients = previous
		return PublicClient{}, "", err
	}
	return client.public(), secret, nil
}

func (cl Client) public() PublicClient {
	return PublicClient{ID: cl.ID, Service: cl.Service, RedirectURIs: slices.Clone(cl.RedirectURIs), CreatedAt: cl.CreatedAt}
}

// List : les clients, sans les secrets.
func (c *Clients) List() ([]PublicClient, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		return nil, err
	}
	list := make([]PublicClient, 0, len(c.clients))
	for _, client := range c.clients {
		list = append(list, client.public())
	}
	return list, nil
}

// RemoveService retire le client d'un service (désinstallé) ; sans effet s'il n'y en a pas.
func (c *Clients) RemoveService(service string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.load(); err != nil {
		return err
	}
	kept := slices.DeleteFunc(slices.Clone(c.clients), func(cl Client) bool { return cl.Service == service })
	if len(kept) == len(c.clients) {
		return nil
	}
	previous := c.clients
	c.clients = kept
	if err := c.save(); err != nil {
		c.clients = previous
		return err
	}
	return nil
}

// Get : le client de cet identifiant.
func (c *Clients) Get(id string) (Client, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.load() != nil {
		return Client{}, false
	}
	for _, client := range c.clients {
		if client.ID == id {
			return client, true
		}
	}
	return Client{}, false
}

// Authenticate vérifie l'identifiant et le secret d'un client (temps constant
// sur le secret).
func (c *Clients) Authenticate(id, secret string) (Client, bool) {
	client, ok := c.Get(id)
	if !ok || secret == "" {
		return Client{}, false
	}
	if subtle.ConstantTimeCompare([]byte(hash(secret)), []byte(client.SecretHash)) != 1 {
		return Client{}, false
	}
	return client, true
}
