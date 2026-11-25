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

package relay

import (
	"context"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"testing"
)

type fakeServices struct{ allowed bool }

func (f fakeServices) ServiceByIP(_ context.Context, ip string) (string, error) {
	if ip == "127.0.0.1" {
		return "nautilink", nil
	}
	return "", nil
}

func (f fakeServices) MailAllowed(name string) (string, bool) { return name, f.allowed }

type sent struct{ service, to, subject, body string }

type fakeSender struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeSender) SendService(service, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{service, to, subject, body})
	return nil
}

func start(t *testing.T, allowed bool) (string, *fakeSender) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	server := &Server{Services: fakeServices{allowed: allowed}, Sender: sender, Logf: t.Logf}
	go server.Serve(ln)
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String(), sender
}

func TestRelay(t *testing.T) {
	addr, sender := start(t, true)
	message := "From: NautiLink <nautilink@moulard.org>\r\nTo: a@exemple.org\r\nSubject: =?utf-8?q?Alerte_SOS_=C3=A9?=\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nBonjour =C3=A9\r\n.point en d=C3=A9but\r\n"
	if err := smtp.SendMail(addr, nil, "nautilink@moulard.org", []string{"a@exemple.org", "b@exemple.org"}, []byte(message)); err != nil {
		t.Fatal(err)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("envoyés : %+v", sender.sent)
	}
	got := sender.sent[0]
	if got.service != "nautilink" || got.to != "a@exemple.org" || got.subject != "Alerte SOS é" || !strings.Contains(got.body, "Bonjour é\n.point en début") {
		t.Fatalf("message : %+v", got)
	}
}

func TestRelayRefused(t *testing.T) {
	addr, sender := start(t, false)
	err := smtp.SendMail(addr, nil, "x@y.z", []string{"a@exemple.org"}, []byte("Subject: s\r\n\r\nt\r\n"))
	if err == nil || !strings.Contains(err.Error(), "554") || len(sender.sent) != 0 {
		t.Fatalf("relais fermé accepté : %v", err)
	}
}

func TestParseMultipart(t *testing.T) {
	data := "Subject: Essai\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=xx\r\n\r\n--xx\r\nContent-Type: text/html\r\n\r\n<p>html</p>\r\n--xx\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\nVGV4dGUg\r\nc2V1bA==\r\n--xx--\r\n"
	subject, body, err := parseMessage([]byte(data))
	if err != nil || subject != "Essai" || body != "Texte seul" {
		t.Fatalf("%q %q %v", subject, body, err)
	}
	if _, _, err := parseMessage([]byte("Content-Type: text/html\r\n\r\n<p>x</p>\r\n")); err == nil {
		t.Fatal("HTML seul accepté")
	}
}
