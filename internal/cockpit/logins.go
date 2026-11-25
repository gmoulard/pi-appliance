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

package cockpit

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Connexion unique à Cockpit (spécification 6.4) : chaque administrateur de
// la console a un compte système (cockpit.sh sync), avec un mot de passe
// tiré au hasard, gardé par la console seule (fichier lisible par root) ;
// Traefik le donne à Cockpit (en-tête Authorization, posé par
// api/auth/forward-cockpit) : on ouvre Cockpit sans se reconnecter, avec les
// droits d'administrateur du Pi (sudo, X-Superuser). Un administrateur
// supprimé ou devenu simple utilisateur perd son compte système à la
// synchronisation suivante (au plus une minute, Run).

// ErrNotReady : pas encore de compte système pour cet administrateur
// (Cockpit pas installé, nom déjà pris sur le Pi, script en échec).
var ErrNotReady = errors.New("compte système de Cockpit pas prêt")

const passwordLength = 32

// Logins tient les comptes système de Cockpit.
type Logins struct {
	Client *Client
	// File : mots de passe et comptes système, lisible par root seul.
	File string
	// Admins : noms des administrateurs de la console.
	Admins func() []string
	Logf   func(format string, args ...any)

	mu     sync.Mutex
	loaded bool
	state  loginState
}

type loginState struct {
	// Passwords : mot de passe du compte système de chaque administrateur.
	Passwords map[string]string `json:"passwords"`
	// Users : compte système de chaque administrateur (son nom, ou pia-<nom>).
	Users map[string]string `json:"users"`
	// Synced : administrateurs à la dernière synchronisation réussie.
	Synced []string `json:"synced"`
}

func (l *Logins) logf(format string, args ...any) {
	if l.Logf != nil {
		l.Logf(format, args...)
	}
}

func (l *Logins) load() {
	if l.loaded {
		return
	}
	l.loaded = true
	l.state = loginState{Passwords: map[string]string{}, Users: map[string]string{}}
	data, err := os.ReadFile(l.File)
	if err != nil {
		if !os.IsNotExist(err) {
			l.logf("Cockpit : %v", err)
		}
		return
	}
	if err := json.Unmarshal(data, &l.state); err != nil {
		l.logf("Cockpit : %s illisible : %v", l.File, err)
	}
	if l.state.Passwords == nil {
		l.state.Passwords = map[string]string{}
	}
	if l.state.Users == nil {
		l.state.Users = map[string]string{}
	}
}

func (l *Logins) save() error {
	data, err := json.Marshal(l.state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.File), 0o700); err != nil {
		return err
	}
	tmp := l.File + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.File)
}

func randomPassword() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, passwordLength)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	for i, b := range buf {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf)
}

func (l *Logins) admins() []string {
	names := slices.Clone(l.Admins())
	slices.Sort(names)
	return slices.Compact(names)
}

// sync : un compte système par administrateur ; appelée verrou pris.
func (l *Logins) sync(ctx context.Context, admins []string) error {
	passwords := map[string]string{}
	for _, name := range admins {
		if l.state.Passwords[name] == "" {
			l.state.Passwords[name] = randomPassword()
		}
		passwords[name] = l.state.Passwords[name]
	}
	// Mot de passe gardé avant l'appel : un compte créé n'est jamais orphelin.
	for name := range l.state.Passwords {
		if _, ok := passwords[name]; !ok {
			delete(l.state.Passwords, name)
		}
	}
	if err := l.save(); err != nil {
		return err
	}
	result, err := l.Client.Sync(ctx, passwords)
	if err != nil {
		return err
	}
	l.state.Users = result.Accounts
	if l.state.Users == nil {
		l.state.Users = map[string]string{}
	}
	l.state.Synced = admins
	for _, name := range result.Conflicts {
		l.logf("Cockpit : pas de compte système pour %s : son nom et pia-%s sont déjà pris sur le Pi", name, name)
	}
	return l.save()
}

// Credentials : compte système et mot de passe de cet administrateur, après
// une synchronisation si la liste des administrateurs a changé.
func (l *Logins) Credentials(ctx context.Context, name string) (user, password string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	admins := l.admins()
	if !slices.Contains(admins, name) {
		return "", "", ErrNotReady
	}
	if !slices.Equal(admins, l.state.Synced) || l.state.Users[name] == "" {
		if err := l.sync(ctx, admins); err != nil {
			return "", "", fmt.Errorf("%w : %v", ErrNotReady, err)
		}
	}
	user, password = l.state.Users[name], l.state.Passwords[name]
	if user == "" || password == "" {
		return "", "", ErrNotReady
	}
	return user, password, nil
}

// User : compte système de cet administrateur, vide s'il n'existe pas encore.
func (l *Logins) User(name string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	return l.state.Users[name]
}

// Run synchronise les comptes chaque minute tant que Cockpit est installé :
// un administrateur ajouté, retiré ou devenu simple utilisateur est suivi sans
// attendre qu'il ouvre Cockpit. Après un retrait de Cockpit, tout est oublié.
func (l *Logins) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		l.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (l *Logins) tick(ctx context.Context) {
	status, err := l.Client.Status(ctx)
	if err != nil || status.Busy() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	if !status.Installed {
		if len(l.state.Synced) > 0 || len(l.state.Users) > 0 {
			l.state = loginState{Passwords: map[string]string{}, Users: map[string]string{}}
			if err := l.save(); err != nil {
				l.logf("Cockpit : %v", err)
			}
		}
		return
	}
	admins := l.admins()
	wanted := slices.Clone(admins)
	slices.Sort(status.Accounts)
	known := make([]string, 0, len(l.state.Users))
	for _, name := range admins {
		if user := l.state.Users[name]; user != "" {
			known = append(known, user)
		}
	}
	slices.Sort(known)
	if slices.Equal(wanted, l.state.Synced) && slices.Equal(known, status.Accounts) {
		return
	}
	if err := l.sync(ctx, admins); err != nil {
		l.logf("Cockpit : comptes système : %s", strings.TrimSpace(err.Error()))
	}
}
