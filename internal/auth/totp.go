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
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// Double authentification (spécification 8.1, 8.3) : codes TOTP à 6 chiffres
// toutes les 30 secondes (RFC 6238, bibliothèque github.com/pquerna/otp, lue par
// toutes les applications d'authentification), et 10 codes de secours à usage
// unique, communs avec le code par e-mail (emailcode.go).

const (
	totpIssuer    = "pi-appliance"
	totpPeriod    = 30
	recoveryCount = 10
	// recoveryAlphabet : sans 0, 1, i, l, o, faciles à confondre une fois imprimés.
	recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	recoveryLength   = 10
)

var totpOptions = totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

// TOTPSetup : de quoi ajouter le compte dans une application d'authentification,
// en photographiant le QR code ou en recopiant la clé.
type TOTPSetup struct {
	Secret string `json:"secret"`
	URL    string `json:"url"`
	// QR : image PNG du QR code, en adresse data: prête pour une balise <img>.
	QR string `json:"qr"`
}

// BeginTOTP prépare l'activation : un nouveau secret, gardé de côté jusqu'à ce
// que ConfirmTOTP reçoive un premier code juste.
func (s *Store) BeginTOTP(name string) (TOTPSetup, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: name,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return TOTPSetup{}, err
	}
	image, err := key.Image(256, 256)
	if err != nil {
		return TOTPSetup{}, err
	}
	var qr bytes.Buffer
	if err := png.Encode(&qr, image); err != nil {
		return TOTPSetup{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return TOTPSetup{}, ErrNotFound
	}
	if account.TOTPSecret != "" {
		return TOTPSetup{}, fmt.Errorf("%w : la double authentification est déjà activée", ErrInvalid)
	}
	account.TOTPPending = key.Secret()
	if err := s.put(account); err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{
		Secret: key.Secret(),
		URL:    key.URL(),
		QR:     "data:image/png;base64," + base64.StdEncoding.EncodeToString(qr.Bytes()),
	}, nil
}

// ConfirmTOTP active la double authentification si le code est juste et renvoie
// les codes de secours, montrés une seule fois : seuls leurs SHA-256 sont gardés.
func (s *Store) ConfirmTOTP(name, code string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return nil, ErrNotFound
	}
	if account.TOTPPending == "" {
		return nil, fmt.Errorf("%w : aucune activation en cours", ErrInvalid)
	}
	step, ok := totpStep(account.TOTPPending, code, s.Now(), 0)
	if !ok {
		return nil, ErrCode
	}
	codes, hashes := recoveryCodes()
	account.TOTPSecret, account.TOTPPending, account.TOTPLastStep, account.RecoveryHashes = account.TOTPPending, "", step, hashes
	account.TwoFactorOffered = true
	if err := s.put(account); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP désactive l'application d'authentification de son propre compte,
// après vérification du mot de passe ; refusé si l'administrateur exige la
// double authentification et que le code par e-mail n'est pas actif.
func (s *Store) DisableTOTP(name, password string) error {
	key := []string{"account:" + name}
	s.mu.Lock()
	now := s.Now()
	if wait := s.blocked(key[0], now); wait > 0 {
		s.mu.Unlock()
		return TooManyError{Wait: wait}
	}
	account, ok := s.accounts[name]
	s.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	valid := account.Hash != "" && checkHash(account.Hash, password)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !valid {
		s.fail(key, now)
		return ErrCredentials
	}
	account, ok = s.accounts[name]
	if !ok {
		return ErrNotFound
	}
	if account.TOTPRequired && !account.emailFactor() {
		return ErrTOTPRequired
	}
	account = withoutTOTP(account)
	if !account.HasTwoFactor() {
		account.RecoveryHashes = nil
	}
	return s.put(account)
}

// ResetTOTP : un administrateur retire la double authentification d'un compte
// (téléphone perdu ou boîte e-mail perdue, et codes de secours égarés) : les
// deux méthodes et les codes de secours ; elle sera à réactiver.
func (s *Store) ResetTOTP(name string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return Account{}, ErrNotFound
	}
	account = withoutTOTP(account)
	account.EmailCode, account.RecoveryHashes = false, nil
	delete(s.emailCodes, codeKeyOf(CodeLogin, name))
	return account, s.put(account)
}

func withoutTOTP(account Account) Account {
	account.TOTPSecret, account.TOTPPending, account.TOTPLastStep = "", "", 0
	return account
}

// checkSecondFactor vérifie un code TOTP (6 chiffres), un code reçu par e-mail
// ou un code de secours, et l'use : un code TOTP ne resert pas, un code par
// e-mail ou de secours est effacé. Verrou tenu.
func (s *Store) checkSecondFactor(name, code string, now time.Time) (bool, error) {
	account := s.accounts[name]
	if step, ok := totpStep(account.TOTPSecret, code, now, account.TOTPLastStep); ok {
		account.TOTPLastStep = step
		return true, s.put(account)
	}
	if account.emailFactor() && s.useCode(CodeLogin, name, code, now) {
		return true, nil
	}
	hash := recoveryHash(code)
	if hash == "" {
		return false, nil
	}
	for i, candidate := range account.RecoveryHashes {
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(hash)) == 1 {
			account.RecoveryHashes = append(account.RecoveryHashes[:i:i], account.RecoveryHashes[i+1:]...)
			return true, s.put(account)
		}
	}
	return false, nil
}

// totpStep cherche le code parmi le pas de 30 secondes courant et ses deux
// voisins (horloge du téléphone un peu décalée), en ignorant les pas déjà
// utilisés (après last) : un code intercepté ne peut pas resservir.
func totpStep(secret, code string, now time.Time, last int64) (int64, bool) {
	code = strings.Join(strings.Fields(code), "")
	if secret == "" || len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return 0, false
	}
	current := now.Unix() / totpPeriod
	for _, step := range []int64{current - 1, current, current + 1} {
		if step <= last {
			continue
		}
		want, err := totp.GenerateCodeCustom(secret, time.Unix(step*totpPeriod, 0), totpOptions)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func recoveryCodes() ([]string, []string) {
	codes := make([]string, recoveryCount)
	hashes := make([]string, recoveryCount)
	for i := range codes {
		raw := make([]byte, recoveryLength)
		for j := range raw {
			raw[j] = recoveryAlphabet[randomIndex(len(recoveryAlphabet))]
		}
		codes[i] = string(raw[:5]) + "-" + string(raw[5:])
		hashes[i] = recoveryHash(codes[i])
	}
	return codes, hashes
}

// recoveryHash : SHA-256 du code sans tiret ni espace, en minuscules ; "" si ce
// n'est pas un code de secours. Les codes sont aléatoires (50 bits) : un sel est inutile.
func recoveryHash(code string) string {
	code = strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	if len(code) != recoveryLength || strings.Trim(code, recoveryAlphabet) != "" {
		return ""
	}
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// randomIndex : entier uniforme dans [0, n), sans biais de modulo.
func randomIndex(n int) int {
	limit := 256 - 256%n
	b := make([]byte, 1)
	for {
		if _, err := rand.Read(b); err != nil {
			panic(err)
		}
		if int(b[0]) < limit {
			return int(b[0]) % n
		}
	}
}
