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
	"slices"
	"strings"
	"testing"
	"time"
)

// enableEmailCode vérifie l'adresse de marie et active le code par e-mail ;
// renvoie les codes de secours.
func enableEmailCode(t *testing.T, store *Store) []string {
	t.Helper()
	_, code, err := store.VerifyEmailCode("marie", "client")
	if err != nil {
		t.Fatal(err)
	}
	codes, account, err := store.ConfirmEmail("marie", code, true)
	if err != nil {
		t.Fatal(err)
	}
	if !account.EmailVerified || !account.Public().EmailCode || !account.HasTwoFactor() {
		t.Fatalf("compte après vérification : %+v", account.Public())
	}
	return codes
}

func TestEmailVerificationEnablesEmailCode(t *testing.T) {
	store, _ := withAdmin(t)
	account, _ := store.Get("marie")
	if !account.Public().TwoFactorOffer {
		t.Fatal("la double authentification doit être proposée au nouveau compte")
	}
	_, code, err := store.VerifyEmailCode("marie", "client")
	if err != nil || len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Fatalf("code %q : %v", code, err)
	}
	// Nouveau code avant une minute : refusé, avec l'attente.
	var resend ResendError
	if _, _, err := store.VerifyEmailCode("marie", "client"); !errors.As(err, &resend) || resend.Wait != time.Minute {
		t.Fatalf("nouvel envoi trop tôt : %v", err)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if _, _, err := store.ConfirmEmail("marie", wrong, true); !errors.Is(err, ErrEmailCode) {
		t.Fatalf("code faux : %v", err)
	}
	codes, account, err := store.ConfirmEmail("marie", " "+code[:3]+" "+code[3:]+" ", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCount || !account.EmailVerified || !account.EmailCode || account.Public().TwoFactorOffer {
		t.Fatalf("après vérification : %d codes, %+v", len(codes), account.Public())
	}
	// Le code ne sert qu'une fois.
	if _, _, err := store.ConfirmEmail("marie", code, true); !errors.Is(err, ErrEmailCode) {
		t.Fatalf("code réutilisé : %v", err)
	}
	// Code gardé haché, en mémoire seulement.
	data, _ := os.ReadFile(store.path)
	if strings.Contains(string(data), code) {
		t.Fatal("code en clair dans accounts.json")
	}
}

func TestEmailVerificationWithoutTwoFactor(t *testing.T) {
	store, _ := withAdmin(t)
	_, code, _ := store.VerifyEmailCode("marie", "client")
	codes, account, err := store.ConfirmEmail("marie", code, false)
	if err != nil || codes != nil || !account.EmailVerified || account.HasTwoFactor() {
		t.Fatalf("vérification seule : %v %v %+v", codes, err, account.Public())
	}
}

func TestEmailCodeExpiresAndAttempts(t *testing.T) {
	store, c := withAdmin(t)
	_, code, _ := store.VerifyEmailCode("marie", "client")
	c.now = c.now.Add(EmailCodeLifetime)
	if _, _, err := store.ConfirmEmail("marie", code, true); !errors.Is(err, ErrEmailCode) {
		t.Fatalf("code de plus de 10 minutes : %v", err)
	}
	_, code, err := store.VerifyEmailCode("marie", "client")
	if err != nil {
		t.Fatal(err)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range emailCodeAttempts {
		store.ConfirmEmail("marie", wrong, true)
	}
	// Après 5 essais, le code ne vaut plus rien (même juste) ; les essais
	// comptent aussi dans la limitation du compte.
	c.now = c.now.Add(maxBlock)
	if _, _, err := store.ConfirmEmail("marie", code, true); !errors.Is(err, ErrEmailCode) {
		t.Fatalf("code après 5 essais : %v", err)
	}
}

func TestEmailCodeSendLimit(t *testing.T) {
	store, c := withAdmin(t)
	for i := range emailCodesPerHour {
		if _, _, err := store.VerifyEmailCode("marie", "client"); err != nil {
			t.Fatalf("envoi %d : %v", i+1, err)
		}
		c.now = c.now.Add(EmailCodeResend)
	}
	var tooMany TooManyError
	if _, _, err := store.VerifyEmailCode("marie", "client"); !errors.As(err, &tooMany) {
		t.Fatalf("6e envoi dans l'heure : %v", err)
	}
	c.now = c.now.Add(time.Hour)
	if _, _, err := store.VerifyEmailCode("marie", "client"); err != nil {
		t.Fatalf("une heure après : %v", err)
	}
}

func TestLoginWithEmailCode(t *testing.T) {
	store, c := withAdmin(t)
	recovery := enableEmailCode(t, store)
	var needed CodeNeededError
	if _, _, err := store.Login("marie", password, "", "client"); !errors.As(err, &needed) || !slices.Equal(needed.Methods, []string{MethodEmail}) {
		t.Fatalf("sans code : %v", err)
	}
	if _, _, err := store.LoginEmailCode("marie", "faux mot de passe", "client"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("code demandé avec un mauvais mot de passe : %v", err)
	}
	_, code, err := store.LoginEmailCode("marie", password, "client")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Login("marie", password, "999999x", "client"); !errors.Is(err, ErrCode) {
		t.Fatalf("code faux : %v", err)
	}
	if _, _, err := store.Login("marie", password, code, "client"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Login("marie", password, code, "client"); !errors.Is(err, ErrCode) {
		t.Fatalf("code réutilisé : %v", err)
	}
	// Code de secours : toujours accepté.
	if _, _, err := store.Login("marie", password, recovery[0], "client"); err != nil {
		t.Fatal(err)
	}
	// Code expiré.
	c.now = c.now.Add(EmailCodeResend)
	_, code, _ = store.LoginEmailCode("marie", password, "client")
	c.now = c.now.Add(EmailCodeLifetime)
	if _, _, err := store.Login("marie", password, code, "client"); !errors.Is(err, ErrCode) {
		t.Fatalf("code expiré : %v", err)
	}
	// Un code de vérification ne sert pas à se connecter.
	c.now = c.now.Add(EmailCodeResend)
	_, verify, _ := store.VerifyEmailCode("marie", "client")
	if _, _, err := store.Login("marie", password, verify, "client"); !errors.Is(err, ErrCode) {
		t.Fatalf("code de vérification à la connexion : %v", err)
	}
}

func TestLoginEmailCodeNeedsMethod(t *testing.T) {
	store, _ := withAdmin(t)
	if _, _, err := store.LoginEmailCode("marie", password, "client"); !errors.Is(err, ErrNoEmailCode) {
		t.Fatalf("compte sans code par e-mail : %v", err)
	}
}

func TestBothMethods(t *testing.T) {
	store, c := withAdmin(t)
	enableEmailCode(t, store)
	secret, _ := enableTOTP(t, store, c)
	var needed CodeNeededError
	if _, _, err := store.Login("marie", password, "", "client"); !errors.As(err, &needed) || !slices.Equal(needed.Methods, []string{MethodEmail, MethodTOTP}) {
		t.Fatalf("deux méthodes : %v", err)
	}
	c.now = c.now.Add(time.Minute)
	if _, _, err := store.Login("marie", password, code(t, secret, c.now), "client"); err != nil {
		t.Fatalf("code de l'application : %v", err)
	}
	_, emailed, _ := store.LoginEmailCode("marie", password, "client")
	if _, _, err := store.Login("marie", password, emailed, "client"); err != nil {
		t.Fatalf("code par e-mail : %v", err)
	}
	// Désactiver l'application garde le code par e-mail, et les codes de secours.
	if err := store.DisableTOTP("marie", password); err != nil {
		t.Fatal(err)
	}
	account, _ := store.Get("marie")
	if !account.HasTwoFactor() || len(account.RecoveryHashes) == 0 {
		t.Fatalf("après l'application retirée : %+v", account.Public())
	}
	// Désactiver le code par e-mail : plus de double authentification, plus de codes de secours.
	if _, err := store.DisableEmailCode("marie", "faux"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("mauvais mot de passe : %v", err)
	}
	account, err := store.DisableEmailCode("marie", password)
	if err != nil || account.HasTwoFactor() || len(account.RecoveryHashes) != 0 || !account.EmailVerified {
		t.Fatalf("après le code par e-mail retiré : %v %+v", err, account.Public())
	}
	if _, _, err := store.Login("marie", password, "", "client"); err != nil {
		t.Fatalf("connexion sans double authentification : %v", err)
	}
}

func TestRequiredSatisfiedByEmailCode(t *testing.T) {
	store, _ := withAdmin(t)
	account, _, err := store.CreateAccount("paul", "paul@example.org", RoleUser, nil, true)
	if err != nil || !account.TOTPRequired || !account.NeedsTwoFactorSetup() {
		t.Fatalf("création avec exigence : %v %+v", err, account.Public())
	}
	if _, err := store.Update("marie", RoleAdmin, nil, true); err != nil {
		t.Fatal(err)
	}
	account, _ = store.Get("marie")
	if !account.NeedsTwoFactorSetup() {
		t.Fatal("exigée sans méthode")
	}
	enableEmailCode(t, store)
	account, _ = store.Get("marie")
	if account.NeedsTwoFactorSetup() {
		t.Fatal("le code par e-mail satisfait l'exigence")
	}
	// Exigée : la dernière méthode ne se retire pas.
	if _, err := store.DisableEmailCode("marie", password); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("dernière méthode retirée : %v", err)
	}
	// L'administrateur retire tout (boîte e-mail perdue) : il faudra la réactiver.
	account, err = store.ResetTOTP("marie")
	if err != nil || account.HasTwoFactor() || len(account.RecoveryHashes) != 0 || !account.NeedsTwoFactorSetup() {
		t.Fatalf("après retrait par l'administrateur : %v %+v", err, account.Public())
	}
}

func TestDismissOffer(t *testing.T) {
	store, _ := withAdmin(t)
	account, err := store.DismissTwoFactorOffer("marie")
	if err != nil || account.Public().TwoFactorOffer {
		t.Fatalf("« Plus tard » : %v %+v", err, account.Public())
	}
	reopened, _ := Open(store.path)
	if account, _ := reopened.Get("marie"); account.Public().TwoFactorOffer {
		t.Fatal("réponse oubliée au redémarrage")
	}
}

func TestSetLanguage(t *testing.T) {
	store, _ := withAdmin(t)
	store.SetLanguage("marie", "DE")
	store.SetLanguage("marie", "../x")
	if account, _ := store.Get("marie"); account.Lang != "de" {
		t.Fatalf("langue : %q", account.Lang)
	}
}
