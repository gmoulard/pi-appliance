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

package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Comptes créés par un administrateur (spécification 8.1) : il saisit un nom,
// une adresse e-mail et un rôle (date de fin pour un invité) ; la personne
// reçoit un lien d'invitation et choisit elle-même son mot de passe.

// InvitationLifetime : durée de validité d'un lien d'invitation.
const InvitationLifetime = 7 * 24 * time.Hour

// Mailer envoie par e-mail le lien d'invitation et le lien « mot de passe
// oublié » : par le serveur SMTP réglé sur le Pi (mail.SMTP), sinon par le
// service d'envoi du projet (mail.Client, spécification 10.2).
// Si l'invitation ne part pas, la console montre le lien à l'administrateur,
// qui le transmet lui-même. lang : langue de la console de qui fait la demande.
type Mailer interface {
	Invite(account Public, link, lang string) error
	PasswordReset(account Public, link, lang string) error
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

// Get renvoie un compte par son identifiant.
func (s *Store) Get(name string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Account{}, ErrNotFound
	}
	return account, nil
}

// Create crée un compte sans mot de passe et renvoie le jeton de son invitation.
func (s *Store) Create(name, email string, role Role, expires *time.Time) (Account, string, error) {
	return s.CreateAccount(name, email, role, expires, false)
}

// CreateAccount : comme Create ; totpRequired exige la double authentification
// dès la première connexion, enregistré avec le compte (pas en deux temps).
func (s *Store) CreateAccount(name, email string, role Role, expires *time.Time, totpRequired bool) (Account, string, error) {
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
		TOTPRequired: totpRequired,
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
	return s.Edit(name, "", role, expires, totpRequired)
}

// Edit change un compte d'un bloc ; email vide : adresse gardée. Une nouvelle
// adresse (pia user edit --email, console) n'est pas vérifiée : le code par
// e-mail cesse de servir de double authentification jusqu'à sa vérification.
func (s *Store) Edit(name, email string, role Role, expires *time.Time, totpRequired bool) (Account, error) {
	if strings.TrimSpace(email) != "" {
		var err error
		if email, err = checkEmail(email); err != nil {
			return Account{}, err
		}
	}
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
	if email != "" && email != account.Email {
		account.Email, account.EmailVerified, account.EmailCode = email, false, false
	}
	account.Role, account.ExpiresAt, account.TOTPRequired = role, expires, totpRequired
	return account, s.put(account)
}

// SetServices : services que le compte peut ouvrir ; restrict faux : tous.
// Les noms sont gardés triés, sans doublon (64 au plus).
func (s *Store) SetServices(name string, restrict bool, services []string) (Account, error) {
	if len(services) > 64 {
		return Account{}, ErrInvalid
	}
	list := []string{}
	for _, service := range services {
		if !servicePattern.MatchString(service) {
			return Account{}, ErrInvalid
		}
		if !slices.Contains(list, service) {
			list = append(list, service)
		}
	}
	slices.Sort(list)
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return Account{}, ErrNotFound
	}
	account.RestrictServices, account.Services = restrict, list
	if !restrict {
		account.Services = nil
	}
	return account, s.put(account)
}

// ForgetService retire un service désinstallé des listes de services autorisés.
func (s *Store) ForgetService(service string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, account := range s.accounts {
		if !slices.Contains(account.Services, service) {
			continue
		}
		account.Services = slices.DeleteFunc(slices.Clone(account.Services), func(other string) bool { return other == service })
		if err := s.put(account); err != nil {
			return err
		}
	}
	return nil
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
	account.Hash, account.ResetHash, account.ResetExpires = hash, "", time.Time{}
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
