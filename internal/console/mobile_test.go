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

package console

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// mobileFixture : console d'un Pi « controltower » avec sa clé publique ;
// domain : adresse des services (controltower.local ou un domaine personnel).
func mobileFixture(t *testing.T, domain string) (fixture, string) {
	t.Helper()
	catalog, err := marketplace.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	accounts, err := auth.Open(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	public, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "system", "device.pub")
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(public)
	manager := &services.Manager{Dir: dir, Domain: domain, Runner: okRunner{}}
	handler := Handler(Config{
		Version:         "1.2.3",
		Catalog:         catalog,
		Services:        manager,
		Accounts:        accounts,
		DevicePublicKey: keyFile,
		Hostname:        func() (string, error) { return "ControlTower", nil },
	})
	return fixture{handler: handler, accounts: accounts, manager: manager}, hex.EncodeToString(sum[:])[:16]
}

func TestHello(t *testing.T) {
	f, id := mobileFixture(t, "controltower.local")
	cookie := f.admin(t)
	if response := f.install(`{"app": "nextcloud"}`, cookie); response.Code != http.StatusCreated {
		t.Fatalf("installation : code %d : %s", response.Code, response.Body.String())
	}
	// Sans connexion.
	response := f.do(http.MethodGet, "/pi-appliance/api/hello", "", "", nil, "Origin", MobileApp)
	if response.Code != http.StatusOK {
		t.Fatalf("code %d : %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://app.pi-appliance.com" {
		t.Fatalf("Access-Control-Allow-Origin %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Access-Control-Allow-Credentials %q", got)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"id", "name", "services", "version"}) {
		t.Fatalf("champs %v : rien d'autre que name, version, id, services", keys)
	}
	hello := decode[Hello](t, response)
	want := Hello{Name: "controltower", Version: "1.2.3", ID: id, Services: []HelloService{{Name: "cloud", Address: "https://cloud-controltower.local"}}}
	if hello.Name != want.Name || hello.Version != want.Version || hello.ID != want.ID || !slices.Equal(hello.Services, want.Services) {
		t.Fatalf("réponse %+v, attendu %+v", hello, want)
	}
	if len(id) != 16 {
		t.Fatalf("identifiant %q", id)
	}

	// Demande préalable du navigateur (CORS, réseau privé de Chrome).
	preflight := f.do(http.MethodOptions, "/pi-appliance/api/hello", "", "", nil,
		"Origin", MobileApp, "Access-Control-Request-Method", "GET", "Access-Control-Request-Private-Network", "true")
	if preflight.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS : code %d", preflight.Code)
	}
	for header, value := range map[string]string{
		"Access-Control-Allow-Origin":          MobileApp,
		"Access-Control-Allow-Private-Network": "true",
		"Access-Control-Allow-Methods":         "GET",
	} {
		if got := preflight.Header().Get(header); got != value {
			t.Fatalf("OPTIONS : %s %q, attendu %q", header, got, value)
		}
	}
}

// L'adresse provisoire de l'application (GitHub Pages) lit aussi api/hello ;
// une autre origine reçoit l'adresse définitive, que son navigateur refuse.
func TestHelloOrigins(t *testing.T) {
	f, _ := mobileFixture(t, "controltower.local")
	for origin, want := range map[string]string{
		"https://gmoulard.github.io": "https://gmoulard.github.io",
		MobileApp:                    MobileApp,
		"https://exemple.com":        MobileApp,
		"":                           MobileApp,
	} {
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			response := f.do(method, "/pi-appliance/api/hello", "", "", nil, "Origin", origin)
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != want {
				t.Errorf("%s depuis %q : Access-Control-Allow-Origin %q, attendu %q", method, origin, got, want)
			}
			if got := response.Header().Get("Vary"); got != "Origin" {
				t.Errorf("%s depuis %q : Vary %q", method, origin, got)
			}
		}
	}
}

func TestHelloWithoutKeyOrServices(t *testing.T) {
	f := newFixture(t)
	hello := decode[Hello](t, f.do(http.MethodGet, "/pi-appliance/api/hello", "", "", nil))
	if hello.ID != "" || hello.Services == nil || len(hello.Services) != 0 || hello.Name == "" {
		t.Fatalf("réponse %+v", hello)
	}
	if body := f.do(http.MethodGet, "/pi-appliance/api/hello", "", "", nil).Body.String(); !strings.Contains(body, `"services":[]`) {
		t.Fatalf("services : liste vide attendue, pas null : %s", body)
	}
}

func TestDeviceID(t *testing.T) {
	dir := t.TempDir()
	if got := DeviceID(filepath.Join(dir, "absent.pub")); got != "" {
		t.Fatalf("clé absente : %q", got)
	}
	bad := filepath.Join(dir, "bad.pub")
	if err := os.WriteFile(bad, []byte("pas une clé"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DeviceID(bad); got != "" {
		t.Fatalf("clé illisible : %q", got)
	}
	// Clé publique tirée d'une graine connue : empreinte fixe, la même qu'en shell :
	// openssl pkey -pubin -in device.pub -outform DER | tail -c 32 | sha256sum | cut -c1-16
	public := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "device.pub")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(public)
	if got, want := DeviceID(path), hex.EncodeToString(sum[:])[:16]; got != want {
		t.Fatalf("identifiant %q, attendu %q", got, want)
	}
}

func TestMobileLink(t *testing.T) {
	f, id := mobileFixture(t, "controltower.local")
	if response := f.do(http.MethodGet, "/pi-appliance/api/mobile", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans connexion : code %d", response.Code)
	}
	cookie := f.admin(t)
	for _, body := range []string{`{"app": "nextcloud"}`, `{"app": "site-web", "name": "www"}`} {
		if response := f.install(body, cookie); response.Code != http.StatusCreated {
			t.Fatalf("installation %s : code %d : %s", body, response.Code, response.Body.String())
		}
	}
	link := decode[MobileLink](t, f.do(http.MethodGet, "/pi-appliance/api/mobile", "", "", cookie))
	if !strings.HasPrefix(link.QR, "data:image/png;base64,") {
		t.Fatalf("code QR %.40q", link.QR)
	}
	raw, ok := strings.CutPrefix(link.Link, "https://app.pi-appliance.com/#add?")
	if !ok {
		t.Fatalf("lien %q", link.Link)
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{
		"n":  {"controltower"},
		"l":  {"https://controltower.local/pi-appliance"},
		"id": {id},
		"s":  {"cloud,https://cloud-controltower.local", "www,https://www-controltower.local"},
	}
	if values.Encode() != want.Encode() {
		t.Fatalf("paramètres %v, attendu %v", values, want)
	}
	for _, secret := range []string{adminPassword, "password", "token", "key"} {
		if strings.Contains(link.Link, secret) {
			t.Fatalf("le lien contient %q : %s", secret, link.Link)
		}
	}
}

func TestMobileLinkInternet(t *testing.T) {
	f, _ := mobileFixture(t, "maison.example.org")
	cookie := f.admin(t)
	link := decode[MobileLink](t, f.do(http.MethodGet, "/pi-appliance/api/mobile", "", "", cookie))
	values, err := url.ParseQuery(strings.TrimPrefix(link.Link, mobileAdd))
	if err != nil {
		t.Fatal(err)
	}
	if got := values.Get("w"); got != "https://maison.example.org/pi-appliance" {
		t.Fatalf("w %q", got)
	}
	if got := values.Get("l"); got != "https://controltower.local/pi-appliance" {
		t.Fatalf("l %q", got)
	}
}
