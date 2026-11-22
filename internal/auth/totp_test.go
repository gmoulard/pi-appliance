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
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func code(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	value, err := totp.GenerateCodeCustom(secret, at, totpOptions)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// enableTOTP active la double authentification de marie et renvoie le secret et les codes de secours.
func enableTOTP(t *testing.T, store *Store, c *clock) (string, []string) {
	t.Helper()
	setup, err := store.BeginTOTP("marie")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmTOTP("marie", "000000x"); !errors.Is(err, ErrCode) {
		t.Fatalf("code faux : %v", err)
	}
	codes, err := store.ConfirmTOTP("marie", code(t, setup.Secret, c.now))
	if err != nil {
		t.Fatal(err)
	}
	return setup.Secret, codes
}

func TestTOTPSetup(t *testing.T) {
	store, c := withAdmin(t)
	setup, err := store.BeginTOTP("marie")
	if err != nil {
		t.Fatal(err)
	}
	link, err := url.Parse(setup.URL)
	if err != nil || link.Scheme != "otpauth" || link.Host != "totp" || link.Query().Get("secret") != setup.Secret || link.Query().Get("issuer") != "pi-appliance" {
		t.Fatalf("adresse otpauth : %q", setup.URL)
	}
	if !strings.HasPrefix(setup.QR, "data:image/png;base64,") {
		t.Fatalf("QR code : %.40s", setup.QR)
	}
	account, _ := store.Session(store.StartSession("marie"))
	if account.Public().TOTP {
		t.Fatal("pas encore activée tant qu'aucun code n'est confirmé")
	}
	// Le téléphone retarde de 30 secondes : accepté.
	codes, err := store.ConfirmTOTP("marie", code(t, setup.Secret, c.now.Add(-30*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != recoveryCount {
		t.Fatalf("%d codes de secours", len(codes))
	}
	data, _ := os.ReadFile(store.path)
	for _, recovery := range codes {
		if len(recovery) != 11 || strings.Contains(string(data), recovery) || strings.Contains(string(data), strings.ReplaceAll(recovery, "-", "")) {
			t.Fatalf("code de secours %q mal formé ou enregistré en clair", recovery)
		}
	}
	if _, err := store.BeginTOTP("marie"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("déjà activée : %v", err)
	}
	if public := store.List()[0]; !public.TOTP || public.RecoveryCodes != recoveryCount {
		t.Fatalf("compte %+v", public)
	}
}

func TestLoginWithTOTP(t *testing.T) {
	store, c := withAdmin(t)
	secret, _ := enableTOTP(t, store, c)
	c.now = c.now.Add(time.Minute)
	if _, _, err := store.Login("marie", password, "", "10.0.0.2"); !errors.Is(err, ErrCodeNeeded) {
		t.Fatalf("sans code : %v", err)
	}
	if _, _, err := store.Login("marie", "faux", code(t, secret, c.now), "10.0.0.2"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("mauvais mot de passe, bon code : %v", err)
	}
	if _, _, err := store.Login("marie", password, "123456", "10.0.0.2"); !errors.Is(err, ErrCode) {
		t.Fatalf("mauvais code : %v", err)
	}
	if _, _, err := store.Login("marie", password, code(t, secret, c.now.Add(-2*time.Minute)), "10.0.0.2"); !errors.Is(err, ErrCode) {
		t.Fatalf("code trop ancien : %v", err)
	}
	now := code(t, secret, c.now)
	if _, _, err := store.Login("marie", password, " "+now[:3]+" "+now[3:], "10.0.0.2"); err != nil {
		t.Fatalf("bon code, avec espaces : %v", err)
	}
	if _, _, err := store.Login("marie", password, now, "10.0.0.3"); !errors.Is(err, ErrCode) {
		t.Fatalf("un code ne sert qu'une fois : %v", err)
	}
	// Un code juste après le dernier utilisé reste accepté ; le dernier pas est gardé au redémarrage.
	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Now = c.Now
	if _, _, err := reopened.Login("marie", password, now, "10.0.0.2"); !errors.Is(err, ErrCode) {
		t.Fatalf("code rejoué après redémarrage : %v", err)
	}
	if _, _, err := reopened.Login("marie", password, code(t, secret, c.now.Add(30*time.Second)), "10.0.0.2"); err != nil {
		t.Fatalf("code suivant : %v", err)
	}
}

func TestWrongCodesAreLimited(t *testing.T) {
	store, c := withAdmin(t)
	enableTOTP(t, store, c)
	for range freeAttempts {
		if _, _, err := store.Login("marie", password, "000000", "10.0.0.2"); !errors.Is(err, ErrCode) {
			t.Fatalf("mauvais code : %v", err)
		}
	}
	if _, _, err := store.Login("marie", password, "000000", "10.0.0.4"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("après 5 mauvais codes : %v", err)
	}
}

func TestRecoveryCodes(t *testing.T) {
	store, c := withAdmin(t)
	_, codes := enableTOTP(t, store, c)
	if _, _, err := store.Login("marie", password, strings.ToUpper(strings.ReplaceAll(codes[3], "-", " ")), "10.0.0.2"); err != nil {
		t.Fatalf("code de secours en majuscules, avec espace : %v", err)
	}
	if _, _, err := store.Login("marie", password, codes[3], "10.0.0.2"); !errors.Is(err, ErrCode) {
		t.Fatalf("un code de secours ne sert qu'une fois : %v", err)
	}
	if left := store.List()[0].RecoveryCodes; left != recoveryCount-1 {
		t.Fatalf("%d codes de secours restants", left)
	}
	if _, _, err := store.Login("marie", password, codes[4], "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
}

func TestDisableAndResetTOTP(t *testing.T) {
	store, c := withAdmin(t)
	enableTOTP(t, store, c)
	if err := store.DisableTOTP("marie", "faux"); !errors.Is(err, ErrCredentials) {
		t.Fatalf("désactiver sans le bon mot de passe : %v", err)
	}
	if _, err := store.Update("marie", RoleAdmin, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := store.DisableTOTP("marie", password); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("exigée par l'administrateur : %v", err)
	}
	if _, err := store.Update("marie", RoleAdmin, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := store.DisableTOTP("marie", password); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Login("marie", password, "", "10.0.0.2"); err != nil {
		t.Fatalf("connexion sans code après désactivation : %v", err)
	}

	enableTOTP(t, store, c)
	account, err := store.ResetTOTP("marie")
	if err != nil || account.TOTPSecret != "" || account.RecoveryHashes != nil {
		t.Fatalf("remise à zéro : %+v, %v", account, err)
	}
}

func TestRequiredTOTP(t *testing.T) {
	store, c := withAdmin(t)
	if _, err := store.Update("marie", RoleAdmin, nil, true); err != nil {
		t.Fatal(err)
	}
	session, account, err := store.Login("marie", password, "", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if !account.NeedsTwoFactorSetup() {
		t.Fatal("double authentification exigée et pas activée")
	}
	if _, ok := store.Session(session); !ok {
		t.Fatal("la session de la console sert à l'activer")
	}
	forward := forwardSession(t, store, session, "wiki-maison.local")
	if _, ok := store.ForwardSession(forward, "wiki-maison.local"); ok {
		t.Fatal("pas de service tant qu'elle n'est pas activée")
	}
	enableTOTP(t, store, c)
	if _, ok := store.ForwardSession(forward, "wiki-maison.local"); !ok {
		t.Fatal("activée : services ouverts")
	}
}

func TestRecoveryHash(t *testing.T) {
	for _, bad := range []string{"", "123456", "abcde-fghij-k", "abcde-fghi0", "abcde-fghil"} {
		if recoveryHash(bad) != "" {
			t.Errorf("%q n'est pas un code de secours", bad)
		}
	}
	if recoveryHash("ABCDE fghjk") != recoveryHash("abcde-fghjk") {
		t.Error("casse et séparateurs ignorés")
	}
}
