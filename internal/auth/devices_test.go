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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const piID = "f2b4ee79f5ba587b"

// phone : clé d'un téléphone, comme la fait l'application (WebCrypto).
type phone struct {
	key *ecdsa.PrivateKey
}

func newPhone(t *testing.T) phone {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return phone{key: key}
}

func (p phone) jwk() string {
	x := base64.RawURLEncoding.EncodeToString(p.key.PublicKey.X.FillBytes(make([]byte, 32)))
	y := base64.RawURLEncoding.EncodeToString(p.key.PublicKey.Y.FillBytes(make([]byte, 32)))
	return fmt.Sprintf(`{"kty":"EC","crv":"P-256","x":%q,"y":%q,"ext":true}`, x, y)
}

func (p phone) id() string {
	key, id, err := parseDeviceKey(p.jwk())
	if err != nil || key.Thumbprint() != id {
		panic(err)
	}
	return id
}

// sign : signature r‖s en base64url, format de WebCrypto.
func (p phone) sign(t *testing.T, message string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(message))
	r, s, err := ecdsa.Sign(rand.Reader, p.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	raw := append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func nonce(n int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%016d", n)))
}

func signedMessage(action string, at time.Time, n int) string {
	return action + "|" + piID + "|" + strconv.FormatInt(at.Unix(), 10) + "|" + nonce(n)
}

func loginMessage(at time.Time, n int) string {
	return signedMessage("connexion", at, n)
}

func pair(t *testing.T, store *Store, p phone, owner string) string {
	t.Helper()
	code, _ := store.PairingCode(owner)
	message := "appairer|" + piID + "|" + code
	token, account, err := store.Pair(code, p.jwk(), "Téléphone de Marie", message, p.sign(t, message), piID, "192.168.1.20")
	if err != nil || token == "" || account.Name != owner {
		t.Fatalf("appairage : %q %+v %v", token, account, err)
	}
	return token
}

func TestThumbprintRFC7638(t *testing.T) {
	// Clé EC d'exemple de la RFC 7517 (annexe A.1) ; empreinte vérifiée à part
	// (Python : SHA-256 du JSON {"crv","kty","x","y"}, base64url sans « = »).
	key := DeviceKey{X: "f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU", Y: "x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0"}
	if got := key.Thumbprint(); got != "oKIywvGUpTVTyxMQ3bwIIeQUudfr_CkLMjCE19ECD-U" {
		t.Fatalf("empreinte %q", got)
	}
}

func TestPairAndLogin(t *testing.T) {
	store, c := withAdmin(t)
	p := newPhone(t)
	if token := pair(t, store, p, "marie"); token == "" {
		t.Fatal("pas de session")
	}
	devices := store.Devices("marie")
	if len(devices) != 1 || devices[0].ID != p.id() || devices[0].Name != "Téléphone de Marie" {
		t.Fatalf("appareils %+v", devices)
	}
	info, err := os.Stat(store.devicesPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("fichier des appareils : %v %v", info, err)
	}

	c.now = c.now.Add(time.Hour)
	message := loginMessage(c.now, 1)
	token, account, err := store.DeviceLogin(p.id(), message, p.sign(t, message), piID, "192.168.1.20")
	if err != nil || account.Name != "marie" {
		t.Fatalf("connexion : %v", err)
	}
	if got, ok := store.Session(token); !ok || got.Name != "marie" {
		t.Fatal("session de l'appareil refusée")
	}
	// Le même message ne sert qu'une fois.
	if _, _, err := store.DeviceLogin(p.id(), message, p.sign(t, message), piID, "192.168.1.20"); !errors.Is(err, ErrDeviceSignature) {
		t.Fatalf("message rejoué : %v", err)
	}
	// Gardé après un redémarrage de la console.
	reopened, err := Open(store.path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Now = c.Now
	message = loginMessage(c.now, 2)
	if _, _, err := reopened.DeviceLogin(p.id(), message, p.sign(t, message), piID, "192.168.1.21"); err != nil {
		t.Fatalf("après redémarrage : %v", err)
	}
	if devices := reopened.Devices("marie"); !devices[0].LastUsed.Equal(c.now.UTC()) {
		t.Fatalf("dernière utilisation %v", devices[0].LastUsed)
	}
}

func TestDeviceLoginRefused(t *testing.T) {
	store, c := withAdmin(t)
	p := newPhone(t)
	pair(t, store, p, "marie")
	other := newPhone(t)
	// Dans la fenêtre de 5 minutes : accepté.
	message := loginMessage(c.now.Add(-4*time.Minute), 7)
	if _, _, err := store.DeviceLogin(p.id(), message, p.sign(t, message), piID, "ok"); err != nil {
		t.Fatalf("4 minutes d'écart : %v", err)
	}
	cases := map[string]struct {
		id, message, signature string
		want                   error
	}{
		"appareil inconnu": {other.id(), loginMessage(c.now, 1), other.sign(t, loginMessage(c.now, 1)), ErrDeviceUnknown},
		"mauvaise clé":     {p.id(), loginMessage(c.now, 2), other.sign(t, loginMessage(c.now, 2)), ErrDeviceSignature},
		"autre Pi":         {p.id(), strings.Replace(loginMessage(c.now, 3), piID, "0000000000000000", 1), p.sign(t, strings.Replace(loginMessage(c.now, 3), piID, "0000000000000000", 1)), ErrDeviceSignature},
		"trop tard":        {p.id(), loginMessage(c.now.Add(-6*time.Minute), 4), p.sign(t, loginMessage(c.now.Add(-6*time.Minute), 4)), ErrDeviceClock},
		"trop tôt":         {p.id(), loginMessage(c.now.Add(6*time.Minute), 5), p.sign(t, loginMessage(c.now.Add(6*time.Minute), 5)), ErrDeviceClock},
		"numéro court":     {p.id(), "connexion|" + piID + "|" + strconv.FormatInt(c.now.Unix(), 10) + "|abc", p.sign(t, "connexion|"+piID+"|"+strconv.FormatInt(c.now.Unix(), 10)+"|abc"), ErrDeviceSignature},
		"message appairer": {p.id(), "appairer|" + piID + "|x", p.sign(t, "appairer|"+piID+"|x"), ErrDeviceSignature},
		"signature vide":   {p.id(), loginMessage(c.now, 6), "", ErrDeviceSignature},
	}
	for name, test := range cases {
		// Compteurs remis à zéro à chaque cas : l'ordre de parcours d'une map
		// change d'un essai à l'autre, la limite des essais ne doit pas s'en mêler.
		store.failures = map[string]failure{}
		if _, _, err := store.DeviceLogin(test.id, test.message, test.signature, piID, name); !errors.Is(err, test.want) {
			t.Errorf("%s : %v, attendu %v", name, err, test.want)
		}
	}
	// Cinq refus pour cet appareil, de cinq clients : il attend avant un nouvel essai.
	store.failures = map[string]failure{}
	for i := range 5 {
		bad := loginMessage(c.now, 20+i)
		if _, _, err := store.DeviceLogin(p.id(), bad, other.sign(t, bad), piID, fmt.Sprint("client ", i)); !errors.Is(err, ErrDeviceSignature) {
			t.Fatalf("refus %d : %v", i, err)
		}
	}
	message = loginMessage(c.now, 8)
	if _, _, err := store.DeviceLogin(p.id(), message, p.sign(t, message), piID, "ok"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("après 5 refus : %v", err)
	}
}

func TestPairingCode(t *testing.T) {
	store, c := withAdmin(t)
	p := newPhone(t)
	code, expires := store.PairingCode("marie")
	if raw, err := base64.RawURLEncoding.DecodeString(code); err != nil || len(raw) != 16 || !expires.Equal(c.now.Add(10*time.Minute)) {
		t.Fatalf("code %q, fin %v", code, expires)
	}
	// Remontré tant qu'il lui reste 5 minutes, remplacé ensuite.
	c.now = c.now.Add(4 * time.Minute)
	if again, _ := store.PairingCode("marie"); again != code {
		t.Fatal("code changé trop tôt")
	}
	c.now = c.now.Add(2 * time.Minute)
	replaced, _ := store.PairingCode("marie")
	if replaced == code {
		t.Fatal("code non remplacé")
	}
	tries := 0
	try := func(code, piInMessage string, signer phone) error {
		tries++
		message := "appairer|" + piInMessage + "|" + code
		_, _, err := store.Pair(code, p.jwk(), "", message, signer.sign(t, message), piID, fmt.Sprint("192.168.1.", tries))
		return err
	}
	if err := try(code, piID, p); !errors.Is(err, ErrPairingCode) {
		t.Fatalf("ancien code : %v", err)
	}
	if err := try(replaced, piID, newPhone(t)); !errors.Is(err, ErrDeviceSignature) {
		t.Fatalf("signature d'une autre clé : %v", err)
	}
	// Refusé une fois, le code ne sert plus.
	if err := try(replaced, piID, p); !errors.Is(err, ErrPairingCode) {
		t.Fatalf("code réutilisé : %v", err)
	}
	fresh, _ := store.PairingCode("marie")
	if err := try(fresh, "0000000000000000", p); !errors.Is(err, ErrDeviceSignature) {
		t.Fatalf("autre Pi : %v", err)
	}
	expired, _ := store.PairingCode("marie")
	c.now = c.now.Add(10*time.Minute + time.Second)
	if err := try(expired, piID, p); !errors.Is(err, ErrPairingCode) {
		t.Fatalf("code expiré : %v", err)
	}
	if len(store.Devices("marie")) != 0 {
		t.Fatal("appareil enregistré malgré les refus")
	}
	last, _ := store.PairingCode("marie")
	if err := try(last, piID, p); err != nil {
		t.Fatal(err)
	}
	if devices := store.Devices("marie"); len(devices) != 1 || devices[0].Name != "Téléphone" {
		t.Fatalf("nom par défaut : %+v", devices)
	}
}

func TestPairBadKeys(t *testing.T) {
	store, _ := withAdmin(t)
	p := newPhone(t)
	for _, jwk := range []string{
		``, `{}`, `pas du json`,
		`{"kty":"RSA","crv":"P-256","x":"AA","y":"AA"}`,
		`{"kty":"EC","crv":"P-384","x":"AA","y":"AA"}`,
		`{"kty":"EC","crv":"P-256","x":"` + strings.Repeat("A", 43) + `","y":"` + strings.Repeat("A", 43) + `"}`,
	} {
		code, _ := store.PairingCode("marie")
		message := "appairer|" + piID + "|" + code
		if _, _, err := store.Pair(code, jwk, "", message, p.sign(t, message), piID, jwk); !errors.Is(err, ErrDeviceSignature) {
			t.Errorf("clé %q : %v", jwk, err)
		}
	}
}

func TestDeviceManagement(t *testing.T) {
	store, c := withAdmin(t)
	p := newPhone(t)
	pair(t, store, p, "marie")
	if _, err := store.RenameDevice("marie", p.id(), "  Mon\ttéléphone\n "); err != nil {
		t.Fatal(err)
	}
	if devices := store.Devices("marie"); devices[0].Name != "Mon téléphone" {
		t.Fatalf("nom %q", devices[0].Name)
	}
	if _, err := store.RenameDevice("marie", p.id(), strings.Repeat("é", 100)); err != nil {
		t.Fatal(err)
	}
	if devices := store.Devices("marie"); len([]rune(devices[0].Name)) != DeviceNameMax {
		t.Fatalf("nom trop long gardé : %d caractères", len([]rune(devices[0].Name)))
	}
	if _, err := store.RenameDevice("paul", p.id(), "x"); !errors.Is(err, ErrDeviceUnknown) {
		t.Fatalf("appareil d'un autre compte renommé : %v", err)
	}
	if err := store.RemoveDevice("paul", p.id()); !errors.Is(err, ErrDeviceUnknown) {
		t.Fatalf("appareil d'un autre compte retiré : %v", err)
	}
	if err := store.RemoveDevice("marie", p.id()); err != nil {
		t.Fatal(err)
	}
	message := loginMessage(c.now, 1)
	if _, _, err := store.DeviceLogin(p.id(), message, p.sign(t, message), piID, "x"); !errors.Is(err, ErrDeviceUnknown) {
		t.Fatalf("appareil retiré accepté : %v", err)
	}
}

func TestDevicesRemovedWithAccountAndFactoryReset(t *testing.T) {
	store, c := withAdmin(t)
	_, invitation, err := store.Create("paul", "paul@example.org", RoleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcceptInvitation(invitation, password); err != nil {
		t.Fatal(err)
	}
	p := newPhone(t)
	pair(t, store, p, "paul")
	if err := store.Delete("paul"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.devices[p.id()]; ok {
		t.Fatal("appareil gardé après la suppression du compte")
	}
	m := newPhone(t)
	pair(t, store, m, "marie")
	if err := store.FactoryReset(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.devicesPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fichier des appareils gardé : %v", err)
	}
	message := loginMessage(c.now, 9)
	if _, _, err := store.DeviceLogin(m.id(), message, m.sign(t, message), piID, "x"); !errors.Is(err, ErrDeviceUnknown) {
		t.Fatalf("après remise à zéro : %v", err)
	}
}

func TestDeviceRevoke(t *testing.T) {
	store, c := withAdmin(t)
	p := newPhone(t)
	pair(t, store, p, "marie")
	// Message de connexion présenté au retrait, ancien message, mauvaise clé : refusés.
	for name, test := range map[string]struct {
		message, signature string
		want               error
	}{
		"connexion":    {loginMessage(c.now, 1), p.sign(t, loginMessage(c.now, 1)), ErrDeviceSignature},
		"trop tard":    {signedMessage("retirer", c.now.Add(-time.Hour), 2), p.sign(t, signedMessage("retirer", c.now.Add(-time.Hour), 2)), ErrDeviceClock},
		"mauvaise clé": {signedMessage("retirer", c.now, 3), newPhone(t).sign(t, signedMessage("retirer", c.now, 3)), ErrDeviceSignature},
		"retour ligne": {strings.ReplaceAll(signedMessage("retirer", c.now, 4), "|", "\n"), p.sign(t, strings.ReplaceAll(signedMessage("retirer", c.now, 4), "|", "\n")), ErrDeviceSignature},
	} {
		if err := store.DeviceRevoke(p.id(), test.message, test.signature, piID, name); !errors.Is(err, test.want) {
			t.Errorf("%s : %v, attendu %v", name, err, test.want)
		}
	}
	if len(store.Devices("marie")) != 1 {
		t.Fatal("appareil retiré malgré les refus")
	}
	message := signedMessage("retirer", c.now, 5)
	if err := store.DeviceRevoke(p.id(), message, p.sign(t, message), piID, "ok"); err != nil {
		t.Fatal(err)
	}
	if len(store.Devices("marie")) != 0 {
		t.Fatal("appareil gardé")
	}
	// Déjà retiré : pas une erreur.
	message = signedMessage("retirer", c.now, 6)
	if err := store.DeviceRevoke(p.id(), message, p.sign(t, message), piID, "ok"); err != nil {
		t.Fatalf("déjà retiré : %v", err)
	}
	message = loginMessage(c.now, 7)
	if _, _, err := store.DeviceLogin(p.id(), message, p.sign(t, message), piID, "ok"); !errors.Is(err, ErrDeviceUnknown) {
		t.Fatalf("connexion après retrait : %v", err)
	}
}
