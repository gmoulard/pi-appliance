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

// Package auth gère les comptes de la console : création du compte
// administrateur au premier démarrage, connexion, sessions, limitation des
// tentatives et réinitialisation par le fichier reset (spécification 8.1, 8.4, 8.5).
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
	RoleGuest Role = "guest"
)

const (
	AdminPasswordMin = 12
	UserPasswordMin  = 8
	passwordMax      = 256
	SessionLifetime  = 7 * 24 * time.Hour
	freeAttempts     = 5
	maxBlock         = 15 * time.Minute
	forgetFailures   = 24 * time.Hour
)

// Iterations de PBKDF2-SHA256 (recommandation OWASP) ; réduit dans les tests.
var Iterations = 600_000

var (
	ErrInvalid     = errors.New("demande invalide")
	ErrSetupDone   = errors.New("le compte administrateur existe déjà")
	ErrCredentials = errors.New("identifiant ou mot de passe incorrect")
	ErrTooMany     = errors.New("trop de tentatives")
	ErrNoReset     = errors.New("aucune réinitialisation en cours")
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

type Account struct {
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	Role          Role      `json:"role"`
	Hash          string    `json:"hash"`
	CreatedAt     time.Time `json:"created_at"`
}

// Public est la vue d'un compte renvoyée à l'interface, sans le hachage.
type Public struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Role          Role   `json:"role"`
}

func (a Account) Public() Public {
	return Public{Name: a.Name, Email: a.Email, EmailVerified: a.EmailVerified, Role: a.Role}
}

type session struct {
	name    string
	expires time.Time
}

type failure struct {
	count int
	last  time.Time
}

// TooManyError dit combien de temps attendre avant une nouvelle tentative.
type TooManyError struct {
	Wait time.Duration
}

func (e TooManyError) Error() string {
	return fmt.Sprintf("%v : réessayez dans %s", ErrTooMany, e.Wait.Round(time.Second))
}

func (e TooManyError) Unwrap() error {
	return ErrTooMany
}

// Store garde les comptes dans un fichier JSON (droits 600) et les sessions en mémoire :
// un redémarrage de la console déconnecte tout le monde.
type Store struct {
	path     string
	Now      func() time.Time
	mu       sync.Mutex
	accounts map[string]Account
	sessions map[string]session
	failures map[string]failure
	reset    bool
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, Now: time.Now, accounts: map[string]Account{}, sessions: map[string]session{}, failures: map[string]failure{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var accounts []Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		return nil, fmt.Errorf("%s : %w", path, err)
	}
	for _, account := range accounts {
		s.accounts[account.Name] = account
	}
	return s, nil
}

// SetupNeeded est vrai tant qu'aucun compte n'existe : le portail propose alors de créer le compte administrateur.
func (s *Store) SetupNeeded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.accounts) == 0
}

// ConsumeResetFile lit le fichier reset déposé sur la carte SD : s'il existe, il est supprimé
// aussitôt et la réinitialisation du mot de passe administrateur est ouverte (spécification 8.5).
func (s *Store) ConsumeResetFile(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.accounts) == 0 {
		return false, nil
	}
	s.reset = true
	return true, nil
}

func (s *Store) ResetPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reset
}

// CreateAdmin crée le premier compte, administrateur ; refusé dès qu'un compte existe.
func (s *Store) CreateAdmin(name, email, password string) (Account, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	email, err := checkEmail(email)
	if err != nil {
		return Account{}, err
	}
	if !namePattern.MatchString(name) {
		return Account{}, fmt.Errorf("%w : l'identifiant doit faire 1 à 32 caractères : lettres minuscules, chiffres, point, tiret ou trait bas", ErrInvalid)
	}
	if err := checkPassword(password, RoleAdmin); err != nil {
		return Account{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.accounts) > 0 {
		return Account{}, ErrSetupDone
	}
	account := Account{Name: name, Email: email, Role: RoleAdmin, Hash: hash, CreatedAt: s.Now().UTC()}
	s.accounts[name] = account
	if err := s.save(); err != nil {
		delete(s.accounts, name)
		return Account{}, err
	}
	return account, nil
}

// ResetAdmin change le mot de passe du premier administrateur pendant une réinitialisation
// par le fichier reset, sans toucher aux données ; toutes les sessions sont fermées.
func (s *Store) ResetAdmin(password string) (Account, error) {
	if err := checkPassword(password, RoleAdmin); err != nil {
		return Account{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.reset {
		return Account{}, ErrNoReset
	}
	admin, ok := s.firstAdmin()
	if !ok {
		return Account{}, ErrNoReset
	}
	previous := admin.Hash
	admin.Hash = hash
	s.accounts[admin.Name] = admin
	if err := s.save(); err != nil {
		admin.Hash = previous
		s.accounts[admin.Name] = admin
		return Account{}, err
	}
	s.reset = false
	s.sessions = map[string]session{}
	s.failures = map[string]failure{}
	return admin, nil
}

func (s *Store) firstAdmin() (Account, bool) {
	var admins []Account
	for _, account := range s.accounts {
		if account.Role == RoleAdmin {
			admins = append(admins, account)
		}
	}
	if len(admins) == 0 {
		return Account{}, false
	}
	sort.Slice(admins, func(i, j int) bool { return admins[i].CreatedAt.Before(admins[j].CreatedAt) })
	return admins[0], true
}

// Login vérifie l'identifiant et le mot de passe et ouvre une session. Après 5 échecs pour
// un même client ou un même compte, chaque nouvel essai attend de plus en plus longtemps (15 minutes au plus).
func (s *Store) Login(name, password, client string) (string, Account, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	keys := []string{"client:" + client, "account:" + name}
	s.mu.Lock()
	now := s.Now()
	for _, key := range keys {
		if wait := s.blocked(key, now); wait > 0 {
			s.mu.Unlock()
			return "", Account{}, TooManyError{Wait: wait}
		}
	}
	account, ok := s.accounts[name]
	s.mu.Unlock()
	hash := account.Hash
	if !ok {
		hash = dummyHash()
	}
	valid := checkHash(hash, password) && ok
	s.mu.Lock()
	defer s.mu.Unlock()
	if !valid {
		for _, key := range keys {
			f := s.failures[key]
			s.failures[key] = failure{count: f.count + 1, last: now}
		}
		return "", Account{}, ErrCredentials
	}
	for _, key := range keys {
		delete(s.failures, key)
	}
	token := randomToken()
	s.sessions[token] = session{name: name, expires: now.Add(SessionLifetime)}
	s.cleanup(now)
	return token, account, nil
}

func (s *Store) blocked(key string, now time.Time) time.Duration {
	f, ok := s.failures[key]
	if !ok || f.count < freeAttempts {
		return 0
	}
	block := maxBlock
	if shift := f.count - freeAttempts; shift < 10 {
		block = min(time.Duration(1<<shift)*time.Minute, maxBlock)
	}
	return f.last.Add(block).Sub(now)
}

// StartSession ouvre une session pour un compte, par exemple juste après sa création.
func (s *Store) StartSession(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	token := randomToken()
	s.sessions[token] = session{name: name, expires: s.Now().Add(SessionLifetime)}
	return token
}

// Session renvoie le compte d'une session encore valide et prolonge celle-ci.
func (s *Store) Session(token string) (Account, bool) {
	if token == "" {
		return Account{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	current, ok := s.sessions[token]
	if !ok || now.After(current.expires) {
		delete(s.sessions, token)
		return Account{}, false
	}
	account, ok := s.accounts[current.name]
	if !ok {
		delete(s.sessions, token)
		return Account{}, false
	}
	current.expires = now.Add(SessionLifetime)
	s.sessions[token] = current
	return account, true
}

func (s *Store) Logout(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *Store) cleanup(now time.Time) {
	for token, current := range s.sessions {
		if now.After(current.expires) {
			delete(s.sessions, token)
		}
	}
	for key, f := range s.failures {
		if now.Sub(f.last) > forgetFailures {
			delete(s.failures, key)
		}
	}
}

func (s *Store) save() error {
	accounts := make([]Account, 0, len(s.accounts))
	for _, account := range s.accounts {
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Name < accounts[j].Name })
	data, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.path)
}

func checkEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" {
		return "", fmt.Errorf("%w : l'adresse e-mail « %s » ne semble pas valide", ErrInvalid, email)
	}
	return email, nil
}

func checkPassword(password string, role Role) error {
	minimum := UserPasswordMin
	if role == RoleAdmin {
		minimum = AdminPasswordMin
	}
	length := utf8.RuneCountInString(password)
	if length < minimum {
		return fmt.Errorf("%w : le mot de passe doit faire au moins %d caractères ; une phrase de plusieurs mots est plus facile à retenir", ErrInvalid, minimum)
	}
	if len(password) > passwordMax {
		return fmt.Errorf("%w : le mot de passe est trop long (%d caractères au plus)", ErrInvalid, passwordMax)
	}
	return nil
}

const hashScheme = "pbkdf2-sha256"

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, Iterations, 32)
	if err != nil {
		return "", err
	}
	encode := base64.RawStdEncoding.EncodeToString
	return strings.Join([]string{hashScheme, strconv.Itoa(Iterations), encode(salt), encode(key)}, "$"), nil
}

func checkHash(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash sert à vérifier un mot de passe pour un compte inexistant, en autant de temps
// que pour un vrai compte : la durée de la réponse ne révèle pas quels identifiants existent.
var dummyHash = sync.OnceValue(func() string {
	hash, err := hashPassword(randomToken())
	if err != nil {
		panic(err)
	}
	return hash
})

func randomToken() string {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(token)
}
