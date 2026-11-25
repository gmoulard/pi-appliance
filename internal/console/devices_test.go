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

package console

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// testPhone : clé d'un téléphone, comme la fait l'application mobile (WebCrypto).
type testPhone struct {
	key *ecdsa.PrivateKey
}

func newTestPhone(t *testing.T) testPhone {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testPhone{key: key}
}

func (p testPhone) coordinates() (string, string) {
	return base64.RawURLEncoding.EncodeToString(p.key.PublicKey.X.FillBytes(make([]byte, 32))),
		base64.RawURLEncoding.EncodeToString(p.key.PublicKey.Y.FillBytes(make([]byte, 32)))
}

func (p testPhone) jwk() string {
	x, y := p.coordinates()
	return fmt.Sprintf(`{"kty":"EC","crv":"P-256","x":%q,"y":%q}`, x, y)
}

func (p testPhone) thumbprint() string {
	x, y := p.coordinates()
	return auth.DeviceKey{X: x, Y: y}.Thumbprint()
}

func (p testPhone) sign(t *testing.T, message string) string {
	t.Helper()
	digest := sha256.Sum256([]byte(message))
	r, s, err := ecdsa.Sign(rand.Reader, p.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...))
}

func (f fixture) postForm(path string, values url.Values, origin string) *httptest.ResponseRecorder {
	return f.do(http.MethodPost, path, "application/x-www-form-urlencoded", values.Encode(), nil, "Origin", origin)
}

// next : adresse vers laquelle la page de passage mène.
func next(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	body := response.Body.String()
	start := strings.Index(body, `content="0;url=`)
	if start < 0 {
		t.Fatalf("pas de page de passage : %s", body)
	}
	rest := body[start+len(`content="0;url=`):]
	return strings.ReplaceAll(rest[:strings.Index(rest, `"`)], "&amp;", "&")
}

func pairForm(t *testing.T, p testPhone, id, code string) url.Values {
	message := "appairer|" + id + "|" + code
	return url.Values{"code": {code}, "cle": {p.jwk()}, "appareil": {"Téléphone Android"}, "message": {message}, "signature": {p.sign(t, message)}}
}

func loginForm(t *testing.T, p testPhone, id string, at time.Time, nonce string) url.Values {
	return signedForm(t, "connexion", p, id, at, nonce)
}

func signedForm(t *testing.T, action string, p testPhone, id string, at time.Time, nonce string) url.Values {
	message := action + "|" + id + "|" + strconv.FormatInt(at.Unix(), 10) + "|" + nonce
	return url.Values{"cle": {p.thumbprint()}, "message": {message}, "signature": {p.sign(t, message)}}
}

func TestDevicePairing(t *testing.T) {
	f, id := mobileFixture(t, "controltower.local")
	cookie := f.admin(t)
	link := decode[MobileLink](t, f.do(http.MethodGet, "/pi-appliance/api/mobile", "", "", cookie))
	if strings.Contains(link.Link, "&c=") || strings.Contains(link.Link, "?c=") {
		t.Fatalf("le lien à partager porte le code d'appairage : %s", link.Link)
	}
	// Le code du QR : le même tant qu'il lui reste plus de 5 minutes.
	code, _ := f.accounts.PairingCode("marie")
	p := newTestPhone(t)

	// Formulaire venu d'un autre site : refusé, sans session.
	refused := f.postForm("/pi-appliance/appareil/appairer", pairForm(t, p, id, code), "https://exemple.org")
	if refused.Code != http.StatusForbidden || len(refused.Result().Cookies()) != 0 || next(t, refused) != "/pi-appliance/?appareil=refus" {
		t.Fatalf("autre site : code %d, %v", refused.Code, refused.Result().Cookies())
	}

	response := f.postForm("/pi-appliance/appareil/appairer", pairForm(t, p, id, code), "https://app.pi-appliance.com")
	if response.Code != http.StatusOK {
		t.Fatalf("appairage : code %d : %s", response.Code, response.Body.String())
	}
	if got := next(t, response); got != "/pi-appliance/?appareil=appaire" {
		t.Fatalf("page de passage vers %q", got)
	}
	if csp := response.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("CSP %q", csp)
	}
	phoneCookie := sessionCookie(t, response)
	if phoneCookie.SameSite != http.SameSiteStrictMode || !phoneCookie.HttpOnly {
		t.Fatalf("cookie %+v", phoneCookie)
	}
	session := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", phoneCookie))
	if session.Account == nil || session.Account.Name != "marie" {
		t.Fatalf("session de l'appareil : %+v", session)
	}
	// Le code ne sert qu'une fois.
	again := f.postForm("/pi-appliance/appareil/appairer", pairForm(t, newTestPhone(t), id, code), "https://app.pi-appliance.com")
	if again.Code != http.StatusForbidden || next(t, again) != "/pi-appliance/?appareil=code" {
		t.Fatalf("code réutilisé : code %d", again.Code)
	}

	// Connexion sans mot de passe, depuis l'adresse provisoire de l'application.
	now := time.Now()
	login := f.postForm("/pi-appliance/appareil/connexion", loginForm(t, p, id, now, "AAAAAAAAAAAAAAAAAAAAAA"), "https://gmoulard.github.io")
	if login.Code != http.StatusOK || next(t, login) != "/pi-appliance/" {
		t.Fatalf("connexion : code %d : %s", login.Code, login.Body.String())
	}
	if session := decode[SessionState](t, f.do(http.MethodGet, "/pi-appliance/api/session", "", "", sessionCookie(t, login))); session.Account == nil || session.Account.Name != "marie" {
		t.Fatalf("session : %+v", session)
	}
	for name, test := range map[string]struct {
		values url.Values
		origin string
		reason string
	}{
		"numéro rejoué":   {loginForm(t, p, id, now, "AAAAAAAAAAAAAAAAAAAAAA"), MobileApp, "refus"},
		"heure":           {loginForm(t, p, id, now.Add(-time.Hour), "BBBBBBBBBBBBBBBBBBBBBB"), MobileApp, "heure"},
		"autre Pi":        {loginForm(t, p, "0000000000000000", now, "CCCCCCCCCCCCCCCCCCCCCC"), MobileApp, "refus"},
		"inconnu":         {loginForm(t, newTestPhone(t), id, now, "DDDDDDDDDDDDDDDDDDDDDD"), MobileApp, "inconnu"},
		"sans origine":    {loginForm(t, p, id, now, "EEEEEEEEEEEEEEEEEEEEEE"), "", "refus"},
		"origine voisine": {loginForm(t, p, id, now, "FFFFFFFFFFFFFFFFFFFFFF"), "https://app.pi-appliance.com.exemple.org", "refus"},
	} {
		response := f.postForm("/pi-appliance/appareil/connexion", test.values, test.origin)
		if len(response.Result().Cookies()) != 0 || next(t, response) != "/pi-appliance/?appareil="+test.reason {
			t.Errorf("%s : code %d, vers %q", name, response.Code, next(t, response))
		}
	}
}

func TestAccountDevices(t *testing.T) {
	f, id := mobileFixture(t, "controltower.local")
	cookie := f.admin(t)
	if response := f.do(http.MethodGet, "/pi-appliance/api/account/devices", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans connexion : code %d", response.Code)
	}
	type list struct {
		Devices []auth.DevicePublic `json:"devices"`
	}
	if got := decode[list](t, f.do(http.MethodGet, "/pi-appliance/api/account/devices", "", "", cookie)); got.Devices == nil || len(got.Devices) != 0 {
		t.Fatalf("liste vide attendue : %+v", got)
	}
	code, _ := f.accounts.PairingCode("marie")
	p := newTestPhone(t)
	if response := f.postForm("/pi-appliance/appareil/appairer", pairForm(t, p, id, code), MobileApp); response.Code != http.StatusOK {
		t.Fatalf("appairage : code %d", response.Code)
	}
	got := decode[list](t, f.do(http.MethodGet, "/pi-appliance/api/account/devices", "", "", cookie))
	if len(got.Devices) != 1 || got.Devices[0].Name != "Téléphone Android" || got.Devices[0].ID != p.thumbprint() {
		t.Fatalf("appareils %+v", got)
	}
	path := "/pi-appliance/api/account/devices/" + p.thumbprint()
	if response := f.do(http.MethodPut, path, "application/json", `{"name": "Pixel de Marie"}`, cookie); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Pixel de Marie") {
		t.Fatalf("renommer : code %d : %s", response.Code, response.Body.String())
	}
	if response := f.do(http.MethodPut, path, "application/json", `{"name": "  "}`, cookie); response.Code != http.StatusBadRequest {
		t.Fatalf("nom vide : code %d", response.Code)
	}
	if response := f.do(http.MethodDelete, path, "", "", cookie); response.Code != http.StatusNoContent {
		t.Fatalf("retirer : code %d", response.Code)
	}
	if response := f.do(http.MethodDelete, path, "", "", cookie); response.Code != http.StatusNotFound {
		t.Fatalf("retirer deux fois : code %d", response.Code)
	}
	login := f.postForm("/pi-appliance/appareil/connexion", loginForm(t, p, id, time.Now(), "AAAAAAAAAAAAAAAAAAAAAA"), MobileApp)
	if next(t, login) != "/pi-appliance/?appareil=inconnu" {
		t.Fatalf("appareil retiré : vers %q", next(t, login))
	}
}

func TestDeviceRevokeFromPhone(t *testing.T) {
	f, id := mobileFixture(t, "controltower.local")
	f.admin(t)
	code, _ := f.accounts.PairingCode("marie")
	p := newTestPhone(t)
	if response := f.postForm("/pi-appliance/appareil/appairer", pairForm(t, p, id, code), MobileApp); response.Code != http.StatusOK {
		t.Fatalf("appairage : code %d", response.Code)
	}
	if response := f.postForm("/pi-appliance/appareil/retirer", signedForm(t, "retirer", p, id, time.Now(), "AAAAAAAAAAAAAAAAAAAAAA"), "https://exemple.org"); next(t, response) != "/pi-appliance/?appareil=refus" {
		t.Fatalf("autre site : vers %q", next(t, response))
	}
	response := f.postForm("/pi-appliance/appareil/retirer", signedForm(t, "retirer", p, id, time.Now(), "BBBBBBBBBBBBBBBBBBBBBB"), MobileApp)
	if response.Code != http.StatusOK || len(response.Result().Cookies()) != 0 || next(t, response) != "/pi-appliance/?appareil=retire" {
		t.Fatalf("retrait : code %d, %v, vers %q", response.Code, response.Result().Cookies(), next(t, response))
	}
	if got := f.accounts.Devices("marie"); len(got) != 0 {
		t.Fatalf("appareil gardé : %+v", got)
	}
	// Clé inconnue ou déjà retirée : même page.
	again := f.postForm("/pi-appliance/appareil/retirer", signedForm(t, "retirer", p, id, time.Now(), "CCCCCCCCCCCCCCCCCCCCCC"), MobileApp)
	if next(t, again) != "/pi-appliance/?appareil=retire" {
		t.Fatalf("déjà retiré : vers %q", next(t, again))
	}
}
