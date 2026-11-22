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
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Liens de partage d'un document Etherpad (spécification 6.3, 8.2) : une
// personne connectée donne à quelqu'un sans compte l'accès à un seul document,
// jusqu'à une date. Comme pour les invitations, seul le SHA-256 du lien est
// enregistré ; un lien cesse de marcher à sa date, quand il est retiré, ou dès
// que le compte qui l'a créé ne peut plus se connecter (supprimé, invité expiré).

const (
	ShareMaxLifetime = 30 * 24 * time.Hour
	maxShares        = 500
)

var ErrTooManyShares = errors.New("trop de liens de partage")

type Share struct {
	ID        string    `json:"id"`
	Hash      string    `json:"hash"`
	Service   string    `json:"service"`
	Pad       string    `json:"pad"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// SharePublic : un lien vu par l'interface, sans son jeton ni son hachage.
type SharePublic struct {
	ID        string    `json:"id"`
	Service   string    `json:"service"`
	Pad       string    `json:"pad"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s Share) Public() SharePublic {
	return SharePublic{ID: s.ID, Service: s.Service, Pad: s.Pad, Owner: s.Owner, CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt}
}

func (s *Store) sharesPath() string {
	return filepath.Join(filepath.Dir(s.path), "shares.json")
}

func (s *Store) loadShares() error {
	data, err := os.ReadFile(s.sharesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var shares []Share
	if err := json.Unmarshal(data, &shares); err != nil {
		return fmt.Errorf("%s : %w", s.sharesPath(), err)
	}
	for _, share := range shares {
		s.shares[share.ID] = share
	}
	return nil
}

// shareValid : lien pas encore expiré, dont le compte créateur peut encore ouvrir les services.
func (s *Store) shareValid(share Share, now time.Time) bool {
	if !now.Before(share.ExpiresAt) {
		return false
	}
	owner, ok := s.accounts[share.Owner]
	return ok && owner.active(now) && !owner.NeedsTOTPSetup()
}

// saveShares garde les liens encore valables ; les autres sont oubliés.
func (s *Store) saveShares(now time.Time) error {
	shares := []Share{}
	for id, share := range s.shares {
		if !s.shareValid(share, now) {
			delete(s.shares, id)
			continue
		}
		shares = append(shares, share)
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].ID < shares[j].ID })
	data, err := json.MarshalIndent(shares, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.sharesPath() + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.sharesPath())
}

// CreateShare crée un lien de partage du document pad du service, valable
// lifetime (30 jours au plus), et renvoie son jeton, montré une seule fois. Le
// lien d'un invité s'arrête au plus tard à la fin de son compte.
func (s *Store) CreateShare(owner, service, pad string, lifetime time.Duration) (SharePublic, string, error) {
	if service == "" || pad == "" || lifetime <= 0 || lifetime > ShareMaxLifetime {
		return SharePublic{}, "", fmt.Errorf("%w : document ou durée du lien incorrects", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now().UTC()
	account, ok := s.accounts[owner]
	if !ok || !account.active(now) {
		return SharePublic{}, "", ErrNotFound
	}
	if len(s.shares) >= maxShares {
		return SharePublic{}, "", ErrTooManyShares
	}
	expires := now.Add(lifetime)
	if account.ExpiresAt != nil && account.ExpiresAt.Before(expires) {
		expires = account.ExpiresAt.UTC()
	}
	token := randomToken()
	share := Share{ID: shareID(), Hash: tokenHash(token), Service: service, Pad: pad, Owner: owner, CreatedAt: now, ExpiresAt: expires}
	s.shares[share.ID] = share
	if err := s.saveShares(now); err != nil {
		delete(s.shares, share.ID)
		return SharePublic{}, "", err
	}
	return share.Public(), token, nil
}

// Shares renvoie les liens valables d'un service : ceux de viewer, ou tous pour un administrateur.
func (s *Store) Shares(service string, viewer Account) []SharePublic {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	list := []SharePublic{}
	for _, share := range s.shares {
		if share.Service == service && s.shareValid(share, now) && (viewer.Role == RoleAdmin || share.Owner == viewer.Name) {
			list = append(list, share.Public())
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	return list
}

// RevokeShare retire un lien : son créateur ou un administrateur.
func (s *Store) RevokeShare(service, id string, by Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	share, ok := s.shares[id]
	if !ok || share.Service != service || (by.Role != RoleAdmin && share.Owner != by.Name) {
		return ErrNotFound
	}
	delete(s.shares, id)
	if err := s.saveShares(s.Now()); err != nil {
		s.shares[id] = share
		return err
	}
	return nil
}

// RevokeServiceShares retire tous les liens d'un service (désinstallé).
func (s *Store) RevokeServiceShares(service string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := false
	for id, share := range s.shares {
		if share.Service == service {
			delete(s.shares, id)
			removed = true
		}
	}
	if !removed {
		return nil
	}
	return s.saveShares(s.Now())
}

// Share renvoie le lien d'un jeton s'il est valable pour ce service.
func (s *Store) Share(token, service string) (SharePublic, bool) {
	if token == "" || service == "" {
		return SharePublic{}, false
	}
	hash := tokenHash(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for _, share := range s.shares {
		if subtle.ConstantTimeCompare([]byte(share.Hash), []byte(hash)) == 1 {
			if share.Service != service || !s.shareValid(share, now) {
				return SharePublic{}, false
			}
			return share.Public(), true
		}
	}
	return SharePublic{}, false
}

func shareID() string {
	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return hex.EncodeToString(id)
}
