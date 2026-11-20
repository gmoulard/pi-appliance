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
	"errors"
	"os"
)

// FactoryReset efface tous les comptes (invitations comprises), les sessions et
// la réinitialisation en attente : la console revient à la création du compte
// administrateur (spécification 8.5, TODO-2.52).
func (s *Store) FactoryReset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.accounts
	s.accounts = map[string]Account{}
	if err := s.save(); err != nil {
		s.accounts = previous
		return err
	}
	if err := os.Remove(s.pendingPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.reset = false
	s.sessions = map[string]session{}
	s.failures = map[string]failure{}
	return nil
}
