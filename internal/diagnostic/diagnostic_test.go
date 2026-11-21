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

package diagnostic

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	out  map[string]string
	err  error
	args []string
}

func (f *fakeRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	f.args = append(f.args, strings.Join(args, " "))
	return []byte(f.out[args[0]]), f.err
}

func TestStatus(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{"status": `{"at":"2026-11-20T10:00:00Z","uptimeSeconds":90,"units":[{"id":"docker","unit":"docker.service","state":"restarting","restarts":4,"since":""}],"traefik":{"container":"restarting","restarts":7,"https":false},"failed":["avahi-daemon.service"],"errors":["psk=[masqué]"],"undervoltage":{"thisBoot":2,"previousBoot":null}}`}}
	status, err := (&Client{Runner: runner}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.UptimeSeconds != 90 || len(status.Units) != 1 || status.Units[0].State != "restarting" || status.Traefik.Restarts != 7 || status.Undervoltage.ThisBoot != 2 || status.Undervoltage.PreviousBoot != nil {
		t.Fatalf("état lu : %+v", status)
	}
	runner.out["status"] = "pas du JSON"
	if _, err := (&Client{Runner: runner}).Status(context.Background()); err == nil {
		t.Fatal("réponse illisible acceptée")
	}
	runner.err = errors.New("code 1")
	if _, err := (&Client{Runner: runner}).Logs(context.Background()); err == nil || !strings.Contains(err.Error(), "diagnostic.sh logs") {
		t.Fatalf("échec des journaux : %v", err)
	}
}

func TestWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	window := &Window{File: filepath.Join(dir, "boot", "diagnostic"), State: filepath.Join(dir, "data", "diagnostic-until"), Now: func() time.Time { return now }}
	if found, err := window.Consume(); found || err != nil {
		t.Fatalf("sans fichier : %v %v", found, err)
	}
	if _, open := window.Until(); open {
		t.Fatal("ouvert sans fichier")
	}
	os.MkdirAll(filepath.Dir(window.File), 0o755)
	os.WriteFile(window.File, nil, 0o644)
	if found, err := window.Consume(); !found || err != nil {
		t.Fatalf("avec fichier : %v %v", found, err)
	}
	if _, err := os.Stat(window.File); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fichier diagnostic gardé sur la carte SD")
	}
	if until, open := window.Until(); !open || !until.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("ouverture : %v %v", until, open)
	}
	now = now.Add(25 * time.Hour)
	if _, open := window.Until(); open {
		t.Fatal("encore ouvert après 24 heures")
	}
	now = now.Add(-2 * time.Hour)
	if err := window.Close(); err != nil {
		t.Fatal(err)
	}
	if _, open := window.Until(); open {
		t.Fatal("encore ouvert après fermeture")
	}
	if err := window.Close(); err != nil {
		t.Fatalf("deuxième fermeture : %v", err)
	}
}

func certificate(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "pi-appliance.local"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, "local.crt"), filepath.Join(dir, "local.key")
	os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	return certFile, keyFile
}

func TestRescueOpensWhenTraefikIsDown(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	probe.Close()
	certFile, keyFile := certificate(t, t.TempDir())
	rescue := &Rescue{
		Addr: "127.0.0.1:0", Probe: address, CertFile: certFile, KeyFile: keyFile, Failures: 2,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "secours") }),
	}
	ctx := context.Background()
	rescue.Check(ctx)
	if rescue.Address() != "" {
		t.Fatal("ouvert dès la première vérification ratée")
	}
	rescue.Check(ctx)
	served := rescue.Address()
	if served == "" {
		t.Fatal("pas ouvert après deux vérifications ratées")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	response, err := client.Get("https://" + served + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "secours" || response.TLS == nil {
		t.Fatalf("réponse : %q", body)
	}
	traefik, err := net.Listen("tcp", address)
	if err != nil {
		t.Skipf("port %s repris entre-temps : %v", address, err)
	}
	defer traefik.Close()
	rescue.Check(ctx)
	if rescue.Address() != "" {
		t.Fatal("encore ouvert alors que Traefik répond")
	}
	if _, err := client.Get("https://" + served + "/"); err == nil {
		t.Fatal("le secours répond encore")
	}
}

func TestRescueWithoutCertificate(t *testing.T) {
	var logs []string
	rescue := &Rescue{Addr: "127.0.0.1:0", Probe: "127.0.0.1:1", CertFile: "/absent.crt", KeyFile: "/absent.key", Failures: 1, Logf: func(format string, args ...any) { logs = append(logs, format) }}
	rescue.Check(context.Background())
	rescue.Check(context.Background())
	if rescue.Address() != "" || len(logs) != 1 {
		t.Fatalf("sans certificat : %q, journal %v", rescue.Address(), logs)
	}
}
