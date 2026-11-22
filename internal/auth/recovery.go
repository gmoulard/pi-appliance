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
	"crypto/subtle"
	"strings"
	"time"
)

// Mot de passe oublié (spécification 8.5) : depuis l'écran de connexion, la
// personne donne son identifiant ou son adresse e-mail ; chaque compte qui
// correspond reçoit par e-mail un lien à usage unique, valable une heure, pour
// choisir un nouveau mot de passe. Le mot de passe actuel reste valable tant
// que le nouveau n'est pas choisi : une demande faite par quelqu'un d'autre ne
// bloque personne. La double authentification n'est pas retirée.

const (
	// ResetLinkLifetime : durée de validité d'un lien « mot de passe oublié ».
	ResetLinkLifetime = time.Hour
	// Demandes de lien par heure : par client (adresse IP) et par compte ou adresse e-mail.
	resetRequestsPerClient  = 10
	resetRequestsPerAccount = 3
	resetRequestWindow      = time.Hour
)

// ResetLink : lien à envoyer par e-mail à un compte (le jeton n'est pas gardé).
type ResetLink struct {
	Account Account
	Token   string
}

// RequestReset prépare un lien « mot de passe oublié » pour chaque compte dont
// l'identifiant ou l'adresse e-mail est identifier. Les comptes invités qui
// n'ont pas encore de mot de passe, expirés ou sans adresse e-mail sont
// ignorés. NE PAS dire à l'appelant si un compte correspond : la console
// répond la même chose dans tous les cas. Seule la limitation des demandes
// peut refuser (TooManyError), selon le client et identifier, jamais selon
// l'existence d'un compte.
func (s *Store) RequestReset(identifier, client string) ([]ResetLink, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	if identifier == "" || len(identifier) > 254 {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	keys := []string{"client:" + client, "who:" + identifier}
	limits := []int{resetRequestsPerClient, resetRequestsPerAccount}
	for i, key := range keys {
		r := s.requests[key]
		if now.Sub(r.last) >= resetRequestWindow {
			r = failure{}
		}
		if r.count >= limits[i] {
			return nil, TooManyError{Wait: r.last.Add(resetRequestWindow).Sub(now)}
		}
	}
	for _, key := range keys {
		r := s.requests[key]
		if now.Sub(r.last) >= resetRequestWindow {
			r = failure{}
		}
		// last : début de la fenêtre d'une heure.
		if r.count == 0 {
			r.last = now
		}
		r.count++
		s.requests[key] = r
	}
	for key, r := range s.requests {
		if now.Sub(r.last) >= resetRequestWindow {
			delete(s.requests, key)
		}
	}
	var links []ResetLink
	for name, account := range s.accounts {
		if name != identifier && strings.ToLower(account.Email) != identifier {
			continue
		}
		if !account.active(now) || account.Email == "" {
			continue
		}
		token := randomToken()
		account.ResetHash, account.ResetExpires = tokenHash(token), now.UTC().Add(ResetLinkLifetime)
		if err := s.put(account); err != nil {
			return nil, err
		}
		links = append(links, ResetLink{Account: account, Token: token})
	}
	return links, nil
}

// ResetLinkAccount renvoie le compte d'un lien « mot de passe oublié » encore valable.
func (s *Store) ResetLinkAccount(token string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resetLink(token)
}

func (s *Store) resetLink(token string) (Account, error) {
	if token == "" {
		return Account{}, ErrNoResetLink
	}
	hash := tokenHash(token)
	now := s.Now()
	for _, account := range s.accounts {
		if account.ResetHash != "" && subtle.ConstantTimeCompare([]byte(account.ResetHash), []byte(hash)) == 1 {
			if !now.Before(account.ResetExpires) || !account.active(now) {
				return Account{}, ErrNoResetLink
			}
			return account, nil
		}
	}
	return Account{}, ErrNoResetLink
}

// UseResetLink enregistre le nouveau mot de passe choisi depuis le lien : le
// lien ne sert qu'une fois, les sessions du compte sont fermées et ses échecs
// de connexion oubliés. La double authentification reste demandée.
func (s *Store) UseResetLink(token, password string) (Account, error) {
	s.mu.Lock()
	account, err := s.resetLink(token)
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
	current, err := s.resetLink(token)
	if err != nil || current.Name != account.Name {
		return Account{}, ErrNoResetLink
	}
	current.Hash, current.ResetHash, current.ResetExpires = hash, "", time.Time{}
	if err := s.put(current); err != nil {
		return Account{}, err
	}
	s.closeSessions(current.Name)
	delete(s.failures, "account:"+current.Name)
	return current, nil
}
