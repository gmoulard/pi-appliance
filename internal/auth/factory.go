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
	"errors"
	"os"
	"time"
)

// FactoryReset efface tous les comptes (invitations comprises), les sessions, les
// liens de partage, les appareils appairés et la réinitialisation en attente : la console revient à la création du compte
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
	if err := os.Remove(s.sharesPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.shares = map[string]Share{}
	if err := os.Remove(s.devicesPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.devices = map[string]Device{}
	s.pairings = map[string]pairingCode{}
	s.nonces = map[string]time.Time{}
	s.reset = false
	s.sessions = map[string]session{}
	s.failures = map[string]failure{}
	s.requests = map[string]failure{}
	s.emailCodes = map[string]emailCode{}
	return nil
}
