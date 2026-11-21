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

package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Comptes créés par un administrateur (spécification 8.1) : il saisit un nom,
// une adresse e-mail et un rôle (date de fin pour un invité) ; la personne
// reçoit un lien d'invitation et choisit elle-même son mot de passe.

// InvitationLifetime : durée de validité d'un lien d'invitation.
const InvitationLifetime = 7 * 24 * time.Hour

// Mailer envoie le lien d'invitation par e-mail. Il viendra avec le service
// d'envoi d'e-mails du projet (TODO-3.6) ; en attendant, la console montre le
// lien à l'administrateur, qui le transmet lui-même.
type Mailer interface {
	Invite(account Public, link string) error
}

// List renvoie les comptes, triés par identifiant.
func (s *Store) List() []Public {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]Public, 0, len(s.accounts))
	for _, account := range s.accounts {
		list = append(list, account.Public())
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// Create crée un compte sans mot de passe et renvoie le jeton de son invitation.
func (s *Store) Create(name, email string, role Role, expires *time.Time) (Account, string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !namePattern.MatchString(name) {
		return Account{}, "", fmt.Errorf("%w : l'identifiant doit faire 1 à 32 caractères : lettres minuscules, chiffres, point, tiret ou trait bas", ErrInvalid)
	}
	email, err := checkEmail(email)
	if err != nil {
		return Account{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now().UTC()
	expires, err = checkRole(role, expires, now)
	if err != nil {
		return Account{}, "", err
	}
	if _, exists := s.accounts[name]; exists {
		return Account{}, "", ErrExists
	}
	token := randomToken()
	account := Account{
		Name: name, Email: email, Role: role, CreatedAt: now, ExpiresAt: expires,
		InviteHash: tokenHash(token), InviteExpires: now.Add(InvitationLifetime),
	}
	if err := s.put(account); err != nil {
		return Account{}, "", err
	}
	return account, token, nil
}

// Invite renvoie un nouveau lien pour un compte : invitation expirée, ou mot de
// passe oublié. Le lien précédent ne marche plus ; le mot de passe actuel reste
// valable jusqu'à ce que le nouveau soit choisi.
func (s *Store) Invite(name string) (Account, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return Account{}, "", ErrNotFound
	}
	token := randomToken()
	account.InviteHash, account.InviteExpires = tokenHash(token), s.Now().UTC().Add(InvitationLifetime)
	if err := s.put(account); err != nil {
		return Account{}, "", err
	}
	return account, token, nil
}

// Invitation renvoie le compte d'un lien d'invitation encore valable.
func (s *Store) Invitation(token string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invitation(token)
}

func (s *Store) invitation(token string) (Account, error) {
	if token == "" {
		return Account{}, ErrNoInvitation
	}
	hash := tokenHash(token)
	now := s.Now()
	for _, account := range s.accounts {
		if account.InviteHash != "" && subtle.ConstantTimeCompare([]byte(account.InviteHash), []byte(hash)) == 1 {
			if !now.Before(account.InviteExpires) || account.Expired(now) {
				return Account{}, ErrNoInvitation
			}
			return account, nil
		}
	}
	return Account{}, ErrNoInvitation
}

// AcceptInvitation enregistre le mot de passe choisi par la personne invitée ;
// le lien ne sert qu'une fois et les anciennes sessions du compte sont fermées.
func (s *Store) AcceptInvitation(token, password string) (Account, error) {
	s.mu.Lock()
	account, err := s.invitation(token)
	s.mu.Unlock()
	if err != nil {
		return Account{}, err
	}
	if err := checkPassword(password, account.Role); err != nil {
		return Account{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.invitation(token)
	if err != nil || current.Name != account.Name {
		return Account{}, ErrNoInvitation
	}
	current.Hash, current.InviteHash, current.InviteExpires = hash, "", time.Time{}
	if err := s.put(current); err != nil {
		return Account{}, err
	}
	s.closeSessions(current.Name)
	return current, nil
}

// Update change le rôle, la date de fin (invité) et l'exigence de double
// authentification d'un compte ; le dernier administrateur le reste.
func (s *Store) Update(name string, role Role, expires *time.Time, totpRequired bool) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return Account{}, ErrNotFound
	}
	expires, err := checkRole(role, expires, s.Now().UTC())
	if err != nil {
		return Account{}, err
	}
	if role != RoleAdmin && s.lastAdmin(name) {
		return Account{}, ErrLastAdmin
	}
	account.Role, account.ExpiresAt, account.TOTPRequired = role, expires, totpRequired
	return account, s.put(account)
}

// Delete supprime un compte et ferme ses sessions ; le dernier administrateur reste.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return ErrNotFound
	}
	if s.lastAdmin(name) {
		return ErrLastAdmin
	}
	delete(s.accounts, name)
	if err := s.save(); err != nil {
		s.accounts[name] = account
		return err
	}
	s.closeSessions(name)
	return s.removeDevicesOf(name)
}

// lastAdmin : ce compte est le seul administrateur ayant un mot de passe.
func (s *Store) lastAdmin(name string) bool {
	if account := s.accounts[name]; account.Role != RoleAdmin || account.Hash == "" {
		return false
	}
	for other, account := range s.accounts {
		if other != name && account.Role == RoleAdmin && account.Hash != "" {
			return false
		}
	}
	return true
}

// checkRole : un invité a une date de fin à venir ; les autres rôles n'en ont pas.
func checkRole(role Role, expires *time.Time, now time.Time) (*time.Time, error) {
	switch role {
	case RoleAdmin, RoleUser:
		return nil, nil
	case RoleGuest:
		if expires == nil || !expires.After(now) {
			return nil, fmt.Errorf("%w : un compte invité a besoin d'une date de fin à venir", ErrInvalid)
		}
		end := expires.UTC()
		return &end, nil
	default:
		return nil, fmt.Errorf("%w : rôle inconnu « %s »", ErrInvalid, role)
	}
}

// put enregistre un compte (verrou tenu) et revient en arrière si l'écriture échoue.
func (s *Store) put(account Account) error {
	previous, existed := s.accounts[account.Name]
	s.accounts[account.Name] = account
	if err := s.save(); err != nil {
		if existed {
			s.accounts[account.Name] = previous
		} else {
			delete(s.accounts, account.Name)
		}
		return err
	}
	return nil
}

// tokenHash : un jeton aléatoire de 256 bits n'a pas besoin d'être ralenti ni salé.
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ChangePassword change le mot de passe de son propre compte, après
// vérification de l'actuel ; les autres sessions du compte sont fermées et une
// nouvelle est rendue.
func (s *Store) ChangePassword(name, current, next string) (string, Account, error) {
	key := []string{"account:" + name}
	s.mu.Lock()
	now := s.Now()
	if wait := s.blocked(key[0], now); wait > 0 {
		s.mu.Unlock()
		return "", Account{}, TooManyError{Wait: wait}
	}
	account, ok := s.accounts[name]
	s.mu.Unlock()
	if !ok {
		return "", Account{}, ErrNotFound
	}
	if err := checkPassword(next, account.Role); err != nil {
		return "", Account{}, err
	}
	valid := account.Hash != "" && checkHash(account.Hash, current)
	hash, err := hashPassword(next)
	if err != nil {
		return "", Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if latest, still := s.accounts[name]; !still || latest.Hash != account.Hash {
		valid = false
	}
	if !valid {
		s.fail(key, now)
		return "", Account{}, ErrCredentials
	}
	account = s.accounts[name]
	account.Hash = hash
	if err := s.put(account); err != nil {
		return "", Account{}, err
	}
	delete(s.failures, key[0])
	s.closeSessions(name)
	token := randomToken()
	s.sessions[token] = session{name: name, expires: now.Add(SessionLifetime)}
	return token, account, nil
}

// FirstAdmin dit si name est le premier administrateur, celui de l'assistant de
// premier démarrage et du fichier reset : son mot de passe protège la clé des
// sauvegardes (spécification 9.1).
func (s *Store) FirstAdmin(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	admin, ok := s.firstAdmin()
	return ok && admin.Name == name
}
