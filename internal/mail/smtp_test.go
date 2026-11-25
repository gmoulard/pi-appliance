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

package mail

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"math/big"
	"mime/quotedprintable"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// fakeSMTP : faux serveur SMTP local, en STARTTLS ou en TLS dès la connexion,
// qui accepte AUTH PLAIN ou LOGIN avec un seul identifiant.
type fakeSMTP struct {
	t         *testing.T
	listener  net.Listener
	config    *tls.Config
	pool      *x509.CertPool
	implicit  bool
	noTLS     bool
	loginOnly bool
	username  string
	password  string

	mu       sync.Mutex
	messages []fakeMessage
	auths    []string
}

type fakeMessage struct {
	from, to string
	data     string
	tls      bool
}

func newFakeSMTP(t *testing.T, implicit bool) *fakeSMTP {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(certificate)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{t: t, listener: listener, pool: pool, implicit: implicit, username: "pi@example.org", password: "mot de passe d'application",
		config: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) port() int { return f.listener.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	secure := false
	if f.implicit {
		conn = tls.Server(conn, f.config)
		secure = true
	}
	reader := bufio.NewReader(conn)
	write := func(line string) { conn.Write([]byte(line + "\r\n")) }
	write("220 faux serveur")
	var from, to string
	authenticated := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			write("250-faux serveur")
			if !secure && !f.noTLS {
				write("250-STARTTLS")
			}
			if secure {
				if f.loginOnly {
					write("250-AUTH LOGIN")
				} else {
					write("250-AUTH PLAIN LOGIN")
				}
			}
			write("250 8BITMIME")
		case "STARTTLS":
			write("220 allez-y")
			tlsConn := tls.Server(conn, f.config)
			if tlsConn.Handshake() != nil {
				return
			}
			conn, reader, secure = tlsConn, bufio.NewReader(tlsConn), true
		case "AUTH":
			parts := strings.Fields(line)
			var user, pass string
			switch strings.ToUpper(parts[1]) {
			case "PLAIN":
				raw, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
				fields := strings.Split(string(raw), "\x00")
				if len(fields) == 3 {
					user, pass = fields[1], fields[2]
				}
			case "LOGIN":
				write("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
				answer, _ := reader.ReadString('\n')
				raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(answer))
				user = string(raw)
				write("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
				answer, _ = reader.ReadString('\n')
				raw, _ = base64.StdEncoding.DecodeString(strings.TrimSpace(answer))
				pass = string(raw)
			}
			f.mu.Lock()
			f.auths = append(f.auths, strings.ToUpper(parts[1]))
			f.mu.Unlock()
			if user == f.username && pass == f.password {
				authenticated = true
				write("235 bienvenue")
			} else {
				write("535 5.7.8 identifiants refusés")
			}
		case "MAIL":
			if !authenticated && f.username != "" {
				write("530 identifiez-vous")
				continue
			}
			from = strings.Trim(strings.TrimPrefix(line, "MAIL FROM:"), "<> ")
			if i := strings.Index(from, ">"); i >= 0 {
				from = from[:i]
			}
			write("250 ok")
		case "RCPT":
			to = strings.Trim(strings.TrimPrefix(line, "RCPT TO:"), "<> ")
			write("250 ok")
		case "DATA":
			write("354 envoyez")
			var data strings.Builder
			for {
				part, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if part == ".\r\n" {
					break
				}
				data.WriteString(part)
			}
			f.mu.Lock()
			f.messages = append(f.messages, fakeMessage{from: from, to: to, data: data.String(), tls: secure})
			f.mu.Unlock()
			write("250 reçu")
		case "QUIT":
			write("221 au revoir")
			return
		default:
			write("250 ok")
		}
	}
}

func (f *fakeSMTP) last(t *testing.T) fakeMessage {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		t.Fatal("aucun message reçu par le faux serveur")
	}
	return f.messages[len(f.messages)-1]
}

func (f *fakeSMTP) sender(t *testing.T, security string) *SMTP {
	t.Helper()
	s, err := OpenSMTP(filepath.Join(t.TempDir(), "smtp.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.RootCAs = f.pool
	if err := s.Save(SMTPSettings{Host: "127.0.0.1", Port: f.port(), Security: security, Username: f.username, Password: f.password, From: "pi@example.org"}, "marie"); err != nil {
		t.Fatal(err)
	}
	return s
}

func decoded(t *testing.T, data string) string {
	t.Helper()
	_, body, _ := strings.Cut(data, "\r\n\r\n")
	var b strings.Builder
	reader := quotedprintable.NewReader(strings.NewReader(body))
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return b.String()
}

func TestSMTPSendSTARTTLS(t *testing.T) {
	server := newFakeSMTP(t, false)
	s := server.sender(t, SecuritySTARTTLS)
	account := auth.Public{Name: "marie", Email: "marie@example.org"}
	if err := s.SendCode(account, auth.CodeLogin, "123456", "fr"); err != nil {
		t.Fatal(err)
	}
	message := server.last(t)
	if !message.tls || message.from != "pi@example.org" || message.to != "marie@example.org" {
		t.Fatalf("message : %+v", message)
	}
	body := decoded(t, message.data)
	if !strings.Contains(body, "123456") || !strings.Contains(body, "10 minutes") || !strings.Contains(message.data, "From: pi-appliance <pi@example.org>") {
		t.Fatalf("texte du code : %s", body)
	}
	if err := s.Invite(account, "https://maison.local/pi-appliance/#invite="+strings.Repeat("a", 43), "en"); err != nil {
		t.Fatal(err)
	}
	if body := decoded(t, server.last(t).data); !strings.Contains(body, "#invite=") || !strings.Contains(body, "within 7 days") {
		t.Fatalf("invitation en anglais : %s", body)
	}
	if err := s.Invite(account, "https://ailleurs.example/autre", "fr"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("lien quelconque : %v", err)
	}
}

func TestSMTPSendTLSAndLogin(t *testing.T) {
	server := newFakeSMTP(t, true)
	server.loginOnly = true
	s := server.sender(t, SecurityTLS)
	if err := s.Test("marie@example.org", "marie", "fr"); err != nil {
		t.Fatal(err)
	}
	if body := decoded(t, server.last(t).data); !strings.Contains(body, "e-mail d'essai") {
		t.Fatalf("e-mail d'essai : %s", body)
	}
	if server.auths[0] != "LOGIN" {
		t.Fatalf("méthode d'identification : %v", server.auths)
	}
}

func TestSMTPErrors(t *testing.T) {
	server := newFakeSMTP(t, false)
	s := server.sender(t, SecuritySTARTTLS)
	settings := *s.settings
	settings.Password = "faux"
	if err := s.Save(settings, "marie"); err != nil {
		t.Fatal(err)
	}
	var refused SMTPError
	err := s.Test("marie@example.org", "marie", "fr")
	if !errors.As(err, &refused) || !strings.Contains(refused.Advice, "mot de passe d'application") || strings.Contains(err.Error(), "faux") {
		t.Fatalf("mauvais mot de passe : %v", err)
	}
	// Serveur sans STARTTLS : rien ne part en clair.
	plain := newFakeSMTP(t, false)
	plain.noTLS = true
	s = plain.sender(t, SecuritySTARTTLS)
	if err := s.Test("marie@example.org", "marie", "fr"); !errors.As(err, &refused) || !strings.Contains(refused.Advice, "STARTTLS") {
		t.Fatalf("sans STARTTLS : %v", err)
	}
	if len(plain.auths) != 0 {
		t.Fatal("identifiant envoyé sans chiffrement")
	}
	// Certificat inconnu : refusé.
	s = server.sender(t, SecuritySTARTTLS)
	s.RootCAs = x509.NewCertPool()
	if err := s.Test("marie@example.org", "marie", "fr"); err == nil {
		t.Fatal("certificat inconnu accepté")
	}
	// Personne n'écoute.
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	s.settings.Port = port
	if err := s.Test("marie@example.org", "marie", "fr"); !errors.As(err, &refused) || !strings.Contains(refused.Advice, "ne répond pas") {
		t.Fatalf("serveur absent : %v", err)
	}
}

func TestSMTPSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "smtp.json")
	s, err := OpenSMTP(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Configured() || s.Status().Port != 587 {
		t.Fatalf("réglage vide : %+v", s.Status())
	}
	if err := s.Send(Message{Kind: KindTest, To: "a@example.org"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("sans réglage : %v", err)
	}
	for _, bad := range []SMTPSettings{
		{Host: "smtp gmail", Security: SecuritySTARTTLS, From: "a@example.org"},
		{Host: "smtp.gmail.com", Security: "aucune", From: "a@example.org"},
		{Host: "smtp.gmail.com", Security: SecurityTLS, Port: 70000, From: "a@example.org"},
		{Host: "smtp.gmail.com", Security: SecurityTLS, Username: "a@example.org"},
		{Host: "smtp.gmail.com", Security: SecurityTLS, From: "pas une adresse"},
		{Host: "smtp.gmail.com", Security: SecurityTLS, Username: "a", Password: "x\r\nRCPT", From: "a@example.org"},
	} {
		if err := s.Save(bad, "marie"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepté : %v", bad, err)
		}
	}
	if err := s.Save(SMTPSettings{Host: "SMTP.Gmail.com", Security: SecurityTLS, Username: "pi@gmail.com", Password: "abcd efgh ijkl mnop"}, "marie"); err != nil {
		t.Fatal(err)
	}
	status := s.Status()
	if !status.Configured || status.Host != "smtp.gmail.com" || status.Port != 465 || status.From != "pi@gmail.com" || !status.PasswordSet || status.UpdatedBy != "marie" {
		t.Fatalf("état : %+v", status)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("droits du fichier : %v %v", info.Mode(), err)
	}
	// Mot de passe vide : l'ancien est gardé, tant que le serveur et l'identifiant ne changent pas.
	if err := s.Save(SMTPSettings{Host: "smtp.gmail.com", Security: SecuritySTARTTLS, Username: "pi@gmail.com"}, "marie"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSMTP(path)
	if err != nil || reopened.settings.Password != "abcd efgh ijkl mnop" || reopened.settings.Port != 587 {
		t.Fatalf("relu : %+v %v", reopened.settings, err)
	}
	if err := s.Save(SMTPSettings{Host: "ssl0.ovh.net", Security: SecurityTLS, Username: "pi@gmail.com"}, "marie"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("autre serveur sans mot de passe : %v", err)
	}
	if err := s.Forget(); err != nil || s.Configured() {
		t.Fatalf("effacé : %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fichier gardé")
	}
}

func TestSMTPLimit(t *testing.T) {
	server := newFakeSMTP(t, false)
	s := server.sender(t, SecuritySTARTTLS)
	now := time.Now()
	s.Now = func() time.Time { return now }
	s.sent = make([]time.Time, smtpPerHour)
	for i := range s.sent {
		s.sent[i] = now
	}
	if err := s.Test("marie@example.org", "marie", "fr"); !errors.Is(err, ErrTooMany) {
		t.Fatalf("au-delà de la limite : %v", err)
	}
	now = now.Add(time.Hour)
	if err := s.Test("marie@example.org", "marie", "fr"); err != nil {
		t.Fatal(err)
	}
}

func TestComposeRefusesBadCodes(t *testing.T) {
	for _, code := range []string{"", "12345", "1234567", "12a456"} {
		if _, err := Compose("x <a@example.org>", Message{Kind: KindLoginCode, To: "b@example.org", Name: "marie", Code: code}, time.Now()); !errors.Is(err, ErrInvalid) {
			t.Errorf("code %q accepté", code)
		}
	}
	if _, err := Compose("x <a@example.org>", Message{Kind: KindTest, To: "b@example.org\r\nBcc: c@example.org", Name: "marie"}, time.Now()); !errors.Is(err, ErrInvalid) {
		t.Error("en-tête injecté")
	}
}

// Alertes (spécification 8.6) : sujet et texte du paquet alerts, sur plusieurs
// lignes ; un sujet sur deux lignes est refusé.
func TestSMTPSendAlert(t *testing.T) {
	server := newFakeSMTP(t, false)
	s := server.sender(t, SecuritySTARTTLS)
	if err := s.SendAlert("marie@example.org", "marie", "fr", "pi-appliance : disque presque plein", "Bonjour,\n\n- Carte SD : 93 % utilisés\n"); err != nil {
		t.Fatal(err)
	}
	message := server.last(t)
	if !strings.Contains(message.data, "Subject: pi-appliance : disque presque plein\r\n") {
		t.Fatalf("sujet : %s", message.data)
	}
	if body := decoded(t, message.data); !strings.Contains(body, "- Carte SD : 93 % utilisés\r\n") {
		t.Fatalf("texte : %q", body)
	}
	if err := s.SendAlert("marie@example.org", "marie", "fr", "sujet\nBcc: x@example.org", "texte"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sujet sur deux lignes : %v", err)
	}
	if err := s.SendAlert("marie@example.org", "marie", "fr", "sujet", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("texte vide : %v", err)
	}
}
