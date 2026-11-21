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

// Package auth est le fournisseur d'identité intégré à la console : création du
// compte administrateur au premier démarrage, comptes utilisateur et invité,
// invitations, connexion, double authentification (TOTP, codes de secours),
// sessions de la console et sessions déléguées (*forward auth* de Traefik),
// limitation des tentatives et réinitialisation par le fichier reset
// (spécification 8.1, 8.3, 8.4, 8.5).
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
	// LoginCodeLifetime : le temps d'aller de la console au service.
	LoginCodeLifetime = 2 * time.Minute
	freeAttempts      = 5
	maxBlock          = 15 * time.Minute
	forgetFailures    = 24 * time.Hour
)

// Iterations de PBKDF2-SHA256 (recommandation OWASP) ; réduit dans les tests.
var Iterations = 600_000

var (
	ErrInvalid     = errors.New("demande invalide")
	ErrSetupDone   = errors.New("le compte administrateur existe déjà")
	ErrCredentials = errors.New("identifiant ou mot de passe incorrect")
	ErrTooMany     = errors.New("trop de tentatives")
	ErrNoReset     = errors.New("aucune réinitialisation en cours")
	// ErrCodeNeeded : mot de passe juste, il faut encore le code de vérification.
	ErrCodeNeeded   = errors.New("code de vérification demandé")
	ErrCode         = errors.New("code de vérification incorrect")
	ErrExpired      = errors.New("compte invité expiré")
	ErrNotFound     = errors.New("compte inconnu")
	ErrExists       = errors.New("ce compte existe déjà")
	ErrLastAdmin    = errors.New("dernier administrateur")
	ErrNoInvitation = errors.New("invitation inconnue ou expirée")
	ErrTOTPRequired = errors.New("double authentification exigée")
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

type Account struct {
	Name          string `json:"name"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Role          Role   `json:"role"`
	// Hash est vide tant que la personne invitée n'a pas choisi son mot de passe.
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt : fin d'un compte invité ; nil pour les autres rôles.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Invitation : SHA-256 du lien envoyé (jamais le lien lui-même) et sa fin.
	InviteHash    string    `json:"invite_hash,omitempty"`
	InviteExpires time.Time `json:"invite_expires,omitzero"`
	// Double authentification : secret TOTP actif, secret en cours d'activation,
	// dernier pas de 30 secondes accepté (un code ne sert qu'une fois),
	// SHA-256 des codes de secours restants.
	TOTPSecret     string   `json:"totp_secret,omitempty"`
	TOTPPending    string   `json:"totp_pending,omitempty"`
	TOTPLastStep   int64    `json:"totp_last_step,omitempty"`
	RecoveryHashes []string `json:"recovery_hashes,omitempty"`
	// TOTPRequired : l'administrateur exige la double authentification pour ce compte.
	TOTPRequired bool `json:"totp_required,omitempty"`
}

// Public est la vue d'un compte renvoyée à l'interface, sans hachage ni secret.
type Public struct {
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	EmailVerified bool       `json:"email_verified"`
	Role          Role       `json:"role"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Invited       bool       `json:"invited"`
	TOTP          bool       `json:"totp"`
	TOTPRequired  bool       `json:"totp_required"`
	RecoveryCodes int        `json:"recovery_codes"`
}

func (a Account) Public() Public {
	return Public{
		Name: a.Name, Email: a.Email, EmailVerified: a.EmailVerified, Role: a.Role,
		ExpiresAt: a.ExpiresAt, Invited: a.Hash == "", TOTP: a.TOTPSecret != "",
		TOTPRequired: a.TOTPRequired, RecoveryCodes: len(a.RecoveryHashes),
	}
}

// Expired : compte invité dont la date de fin est passée.
func (a Account) Expired(now time.Time) bool {
	return a.ExpiresAt != nil && !now.Before(*a.ExpiresAt)
}

// NeedsTOTPSetup : l'administrateur exige la double authentification et elle
// n'est pas encore activée ; le compte ne peut alors que l'activer.
func (a Account) NeedsTOTPSetup() bool {
	return a.TOTPRequired && a.TOTPSecret == ""
}

func (a Account) active(now time.Time) bool {
	return a.Hash != "" && !a.Expired(now)
}

// session : session de la console (cookie du chemin /pi-appliance) ou session
// déléguée (forward), qui n'ouvre que les services protégés par Traefik.
// Une session déléguée vaut pour une seule adresse (host) et se ferme avec la
// session de la console qui l'a ouverte (parent).
type session struct {
	name    string
	expires time.Time
	forward bool
	host    string
	parent  string
}

// loginCode : code à usage unique qui porte une connexion de la console vers
// l'adresse d'un service (state : valeur du cookie posé par ce service sur le
// navigateur qui a demandé la connexion).
type loginCode struct {
	parent  string
	host    string
	state   string
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
// un redémarrage de la console déconnecte tout le monde. Une réinitialisation par le
// fichier reset est gardée dans reset-pending, à côté des comptes, jusqu'au nouveau
// mot de passe : elle survit à un redémarrage.
type Store struct {
	path     string
	Now      func() time.Time
	mu       sync.Mutex
	accounts map[string]Account
	sessions map[string]session
	codes    map[string]loginCode
	failures map[string]failure
	reset    bool
	shares   map[string]Share
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, Now: time.Now, accounts: map[string]Account{}, sessions: map[string]session{}, codes: map[string]loginCode{}, failures: map[string]failure{}, shares: map[string]Share{}}
	if err := s.loadShares(); err != nil {
		return nil, err
	}
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
	if _, err := os.Stat(s.pendingPath()); err == nil {
		s.reset = len(s.accounts) > 0
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s, nil
}

func (s *Store) pendingPath() string {
	return filepath.Join(filepath.Dir(s.path), "reset-pending")
}

// SetupNeeded est vrai tant qu'aucun compte n'existe : le portail propose alors de créer le compte administrateur.
func (s *Store) SetupNeeded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.accounts) == 0
}

// ConsumeResetFile lit le fichier reset déposé sur la carte SD : s'il existe, la
// réinitialisation du mot de passe administrateur est enregistrée dans reset-pending,
// puis le fichier reset est supprimé (spécification 8.5).
func (s *Store) ConsumeResetFile(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.accounts) == 0 {
		return false, os.Remove(path)
	}
	if err := s.writePending(); err != nil {
		return false, err
	}
	s.reset = true
	return true, os.Remove(path)
}

// NE PAS supprimer le fichier reset avant d'avoir écrit reset-pending : une coupure
// entre les deux perdrait la réinitialisation.
func (s *Store) writePending() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.pendingPath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
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
	if err := os.Remove(s.pendingPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
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

// Login vérifie l'identifiant, le mot de passe et, si la double authentification
// est activée, le code de vérification ou un code de secours, puis ouvre une
// session. Sans code, ErrCodeNeeded dit que le mot de passe est juste et qu'il
// faut le code. Après 5 échecs pour un même client ou un même compte, chaque
// nouvel essai attend de plus en plus longtemps (15 minutes au plus).
func (s *Store) Login(name, password, code, client string) (string, Account, error) {
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
	if !ok || hash == "" {
		hash = dummyHash()
	}
	valid := checkHash(hash, password) && ok && account.Hash != ""
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, still := s.accounts[name]; !still || current.Hash != account.Hash {
		valid = false
	}
	if !valid {
		s.fail(keys, now)
		return "", Account{}, ErrCredentials
	}
	account = s.accounts[name]
	if account.Expired(now) {
		return "", Account{}, ErrExpired
	}
	if account.TOTPSecret != "" {
		if strings.TrimSpace(code) == "" {
			return "", Account{}, ErrCodeNeeded
		}
		ok, err := s.checkSecondFactor(name, code, now)
		if err != nil {
			return "", Account{}, err
		}
		if !ok {
			s.fail(keys, now)
			return "", Account{}, ErrCode
		}
		account = s.accounts[name]
	}
	for _, key := range keys {
		delete(s.failures, key)
	}
	token := randomToken()
	s.sessions[token] = session{name: name, expires: now.Add(SessionLifetime)}
	s.cleanup(now)
	return token, account, nil
}

func (s *Store) fail(keys []string, now time.Time) {
	for _, key := range keys {
		f := s.failures[key]
		s.failures[key] = failure{count: f.count + 1, last: now}
	}
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

// Session renvoie le compte d'une session de la console encore valide et prolonge celle-ci.
// Une session déléguée n'ouvre pas la console.
func (s *Store) Session(token string) (Account, bool) {
	return s.lookup(token, false, "")
}

// StartLoginCode ouvre, pour une personne connectée à la console (session
// consoleToken), un code à usage unique vers l'adresse host d'un service,
// valable LoginCodeLifetime et seulement pour le navigateur qui porte state.
func (s *Store) StartLoginCode(consoleToken, host, state string) (string, bool) {
	if host == "" || state == "" {
		return "", false
	}
	if _, ok := s.lookup(consoleToken, false, ""); !ok {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	s.cleanup(now)
	code := randomToken()
	s.codes[code] = loginCode{parent: consoleToken, host: host, state: state, expires: now.Add(LoginCodeLifetime)}
	return code, true
}

// UseLoginCode échange un code contre une session déléguée limitée à host. Le
// code ne sert qu'une fois, même refusé.
func (s *Store) UseLoginCode(code, host, state string) (string, bool) {
	if code == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	current, ok := s.codes[code]
	delete(s.codes, code)
	if !ok || now.After(current.expires) || current.host != host || subtle.ConstantTimeCompare([]byte(current.state), []byte(state)) != 1 {
		return "", false
	}
	parent, ok := s.sessions[current.parent]
	if !ok || parent.forward || now.After(parent.expires) {
		return "", false
	}
	token := randomToken()
	s.sessions[token] = session{name: parent.name, expires: now.Add(SessionLifetime), forward: true, host: host, parent: current.parent}
	return token, true
}

// ForwardSession renvoie le compte d'une session déléguée valide pour l'adresse
// host (forward auth de Traefik), refusée tant que la double authentification
// exigée n'est pas activée.
func (s *Store) ForwardSession(token, host string) (Account, bool) {
	account, ok := s.lookup(token, true, host)
	if !ok || account.NeedsTOTPSetup() {
		return Account{}, false
	}
	return account, true
}

// lookup : host, pour une session déléguée, l'adresse demandée ; vide pour la console.
func (s *Store) lookup(token string, forward bool, host string) (Account, bool) {
	if token == "" {
		return Account{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	current, ok := s.sessions[token]
	if !ok || current.forward != forward || current.host != host {
		return Account{}, false
	}
	if now.After(current.expires) {
		delete(s.sessions, token)
		return Account{}, false
	}
	account, ok := s.accounts[current.name]
	if !ok || !account.active(now) {
		delete(s.sessions, token)
		return Account{}, false
	}
	current.expires = now.Add(SessionLifetime)
	s.sessions[token] = current
	return account, true
}

// closeSessions ferme toutes les sessions d'un compte (mot de passe changé, compte supprimé).
func (s *Store) closeSessions(name string) {
	for token, current := range s.sessions {
		if current.name == name {
			delete(s.sessions, token)
		}
	}
}

// Logout ferme une session et, pour la console, les sessions déléguées qu'elle a ouvertes.
func (s *Store) Logout(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
	for other, current := range s.sessions {
		if current.parent == token {
			delete(s.sessions, other)
		}
	}
}

func (s *Store) cleanup(now time.Time) {
	for token, current := range s.sessions {
		if now.After(current.expires) {
			delete(s.sessions, token)
		}
	}
	for code, current := range s.codes {
		if now.After(current.expires) {
			delete(s.codes, code)
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
