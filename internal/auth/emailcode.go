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
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Double authentification par un code envoyé par e-mail (spécification 8.1,
// TODO-3.15), et vérification de l'adresse e-mail par le même genre de code
// (spécification 5.1, TODO-2.19). Méthode par défaut d'un compte dont
// l'adresse est vérifiée : vérifier l'adresse l'active, sauf refus. Elle peut
// être active en même temps que l'application d'authentification (TOTP) : à
// la connexion, la personne choisit. Les codes de secours valent pour les deux.
//
// Un code : 6 chiffres, valable 10 minutes, une seule fois, 5 essais au plus ;
// un nouveau code remplace le précédent, pas avant une minute ; 5 envois par
// heure et par compte, 20 par client. Seul son HMAC-SHA256 est gardé, en
// mémoire : un redémarrage de la console oublie les codes en cours.

// Méthodes de double authentification (Account.Methods).
const (
	MethodEmail = "email"
	MethodTOTP  = "totp"
)

// CodePurpose : but d'un code envoyé par e-mail.
type CodePurpose string

const (
	// CodeLogin : connexion, double authentification par e-mail.
	CodeLogin CodePurpose = "login"
	// CodeVerify : vérification de l'adresse e-mail (et activation du code par e-mail).
	CodeVerify CodePurpose = "verify"
)

const (
	EmailCodeLifetime   = 10 * time.Minute
	EmailCodeResend     = time.Minute
	emailCodeAttempts   = 5
	emailCodesPerHour   = 5
	emailCodesPerClient = 20
	emailCodeWindow     = time.Hour
)

// CodeMailer envoie un code à 6 chiffres à l'adresse d'un compte : l'envoi par
// le SMTP du Pi en est un.
type CodeMailer interface {
	SendCode(account Public, purpose CodePurpose, code, lang string) error
}

var (
	// ErrNoEmailCode : pas de code par e-mail possible pour ce compte.
	ErrNoEmailCode = errors.New("pas de code par e-mail pour ce compte")
	// ErrEmailCode : code par e-mail faux, expiré ou déjà servi.
	ErrEmailCode = errors.New("code reçu par e-mail incorrect ou expiré")
)

// ResendError : un code vient d'être envoyé ; Wait avant d'en demander un autre.
type ResendError struct {
	Wait time.Duration
}

func (e ResendError) Error() string {
	return fmt.Sprintf("un code vient d'être envoyé : réessayez dans %s", e.Wait.Round(time.Second))
}

type emailCode struct {
	hash     []byte
	expires  time.Time
	sent     time.Time
	attempts int
}

func codeKeyOf(purpose CodePurpose, name string) string {
	return string(purpose) + ":" + name
}

func randomKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

func (s *Store) codeHash(purpose CodePurpose, name, code string) []byte {
	mac := hmac.New(sha256.New, s.codeKey)
	mac.Write([]byte(string(purpose) + "\x00" + name + "\x00" + code))
	return mac.Sum(nil)
}

// LoginEmailCode : à la connexion, après le mot de passe, prépare le code à
// envoyer par e-mail. Le mot de passe est vérifié comme pour Login (les
// mauvais essais comptent) ; refusé si le compte n'a pas le code par e-mail.
func (s *Store) LoginEmailCode(name, password, client string) (Account, string, error) {
	account, _, _, err := s.credentials(name, password, client)
	if err != nil {
		return Account{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[account.Name]
	if !ok || !account.emailFactor() {
		return Account{}, "", ErrNoEmailCode
	}
	code, err := s.issueCode(CodeLogin, account.Name, client)
	return account, code, err
}

// VerifyEmailCode prépare le code qui vérifie l'adresse e-mail de son propre compte.
func (s *Store) VerifyEmailCode(name, client string) (Account, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return Account{}, "", ErrNotFound
	}
	if account.Email == "" {
		return Account{}, "", ErrNoEmailCode
	}
	code, err := s.issueCode(CodeVerify, name, client)
	return account, code, err
}

// ForgetEmailCode retire un code qui n'a pas pu partir : la personne peut en
// redemander un tout de suite (l'envoi reste compté dans la limite par heure).
func (s *Store) ForgetEmailCode(purpose CodePurpose, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.emailCodes, codeKeyOf(purpose, name))
}

// issueCode : verrou tenu.
func (s *Store) issueCode(purpose CodePurpose, name, client string) (string, error) {
	now := s.Now()
	key := codeKeyOf(purpose, name)
	if current, ok := s.emailCodes[key]; ok && now.Sub(current.sent) < EmailCodeResend {
		return "", ResendError{Wait: current.sent.Add(EmailCodeResend).Sub(now)}
	}
	if err := s.takeRequests([]string{"code-client:" + client, "code-account:" + name}, []int{emailCodesPerClient, emailCodesPerHour}, emailCodeWindow, now); err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	s.emailCodes[key] = emailCode{hash: s.codeHash(purpose, name, code), expires: now.Add(EmailCodeLifetime), sent: now}
	for other, current := range s.emailCodes {
		if !now.Before(current.expires) {
			delete(s.emailCodes, other)
		}
	}
	return code, nil
}

// useCode vérifie un code reçu par e-mail et l'use s'il est juste ; un mauvais
// essai compte, et le code ne vaut plus rien après 5. Verrou tenu.
func (s *Store) useCode(purpose CodePurpose, name, code string, now time.Time) bool {
	code = strings.Join(strings.Fields(code), "")
	key := codeKeyOf(purpose, name)
	current, ok := s.emailCodes[key]
	if !ok {
		return false
	}
	if !now.Before(current.expires) {
		delete(s.emailCodes, key)
		return false
	}
	if len(code) == 6 && strings.Trim(code, "0123456789") == "" && hmac.Equal(current.hash, s.codeHash(purpose, name, code)) {
		delete(s.emailCodes, key)
		return true
	}
	current.attempts++
	if current.attempts >= emailCodeAttempts {
		delete(s.emailCodes, key)
	} else {
		s.emailCodes[key] = current
	}
	return false
}

// ConfirmEmail vérifie l'adresse e-mail de son propre compte avec le code reçu.
// enable : active aussi la double authentification par e-mail (le choix par
// défaut). Renvoie les codes de secours s'ils viennent d'être créés (première
// méthode de double authentification), montrés une seule fois.
func (s *Store) ConfirmEmail(name, code string, enable bool) ([]string, Account, error) {
	key := []string{"account:" + name}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	if wait := s.blocked(key[0], now); wait > 0 {
		return nil, Account{}, TooManyError{Wait: wait}
	}
	account, ok := s.accounts[name]
	if !ok {
		return nil, Account{}, ErrNotFound
	}
	if !s.useCode(CodeVerify, name, code, now) {
		s.fail(key, now)
		return nil, Account{}, ErrEmailCode
	}
	account.EmailVerified = true
	var codes []string
	if enable {
		account.EmailCode, account.TwoFactorOffered = true, true
		if len(account.RecoveryHashes) == 0 {
			var hashes []string
			codes, hashes = recoveryCodes()
			account.RecoveryHashes = hashes
		}
	}
	if err := s.put(account); err != nil {
		return nil, Account{}, err
	}
	return codes, account, nil
}

// DisableEmailCode retire la double authentification par e-mail de son propre
// compte, après vérification du mot de passe ; refusé si l'administrateur
// exige la double authentification et que l'application n'est pas activée.
func (s *Store) DisableEmailCode(name, password string) (Account, error) {
	account, err := s.checkOwnPassword(name, password)
	if err != nil {
		return Account{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return Account{}, ErrNotFound
	}
	if account.TOTPRequired && account.TOTPSecret == "" {
		return Account{}, ErrTOTPRequired
	}
	account.EmailCode = false
	if !account.HasTwoFactor() {
		account.RecoveryHashes = nil
	}
	delete(s.emailCodes, codeKeyOf(CodeLogin, name))
	return account, s.put(account)
}

// DismissTwoFactorOffer : la personne a répondu « Plus tard » à la proposition
// de double authentification ; elle reste possible dans « Mon compte ».
func (s *Store) DismissTwoFactorOffer(name string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok {
		return Account{}, ErrNotFound
	}
	if account.TwoFactorOffered {
		return account, nil
	}
	account.TwoFactorOffered = true
	return account, s.put(account)
}

// SetLanguage garde la langue de la console d'un compte, pour ses e-mails.
func (s *Store) SetLanguage(name, lang string) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if len(lang) != 2 || strings.Trim(lang, "abcdefghijklmnopqrstuvwxyz") != "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.accounts[name]
	if !ok || account.Lang == lang {
		return
	}
	account.Lang = lang
	s.put(account)
}

// checkOwnPassword : mot de passe de son propre compte, avec la limitation des
// tentatives du compte ; verrou non tenu au retour.
func (s *Store) checkOwnPassword(name, password string) (Account, error) {
	key := []string{"account:" + name}
	s.mu.Lock()
	now := s.Now()
	if wait := s.blocked(key[0], now); wait > 0 {
		s.mu.Unlock()
		return Account{}, TooManyError{Wait: wait}
	}
	account, ok := s.accounts[name]
	s.mu.Unlock()
	if !ok {
		return Account{}, ErrNotFound
	}
	valid := account.Hash != "" && checkHash(account.Hash, password)
	if !valid {
		s.mu.Lock()
		s.fail(key, now)
		s.mu.Unlock()
		return Account{}, ErrCredentials
	}
	return account, nil
}

// takeRequests compte une demande par clé dans une fenêtre d'une durée window
// (début de la fenêtre dans last) et refuse si l'une des clés a atteint sa
// limite, sans rien compter. Verrou tenu.
func (s *Store) takeRequests(keys []string, limits []int, window time.Duration, now time.Time) error {
	for i, key := range keys {
		r := s.requests[key]
		if now.Sub(r.last) >= window {
			r = failure{}
		}
		if r.count >= limits[i] {
			return TooManyError{Wait: r.last.Add(window).Sub(now)}
		}
	}
	for _, key := range keys {
		r := s.requests[key]
		if now.Sub(r.last) >= window {
			r = failure{}
		}
		if r.count == 0 {
			r.last = now
		}
		r.count++
		s.requests[key] = r
	}
	for key, r := range s.requests {
		if now.Sub(r.last) >= window {
			delete(s.requests, key)
		}
	}
	return nil
}
