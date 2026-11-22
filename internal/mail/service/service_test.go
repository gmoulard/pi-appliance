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

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/mail"
)

const token = "4W13WMT6ZgVTDSypSUeKPaubt7nV1Ny7sjzD-NklAUo"

type fakeSender struct {
	mu   sync.Mutex
	sent map[string][]byte
}

func (f *fakeSender) Send(to string, message []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sent == nil {
		f.sent = map[string][]byte{}
	}
	f.sent[to] = message
	return nil
}

// handlerDoer envoie les demandes du client directement au service.
type handlerDoer struct{ handler http.Handler }

func (d handlerDoer) Do(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	r.RemoteAddr = "203.0.113.7:4000"
	d.handler.ServeHTTP(recorder, r)
	return recorder.Result(), nil
}

func setup(t *testing.T) (*fakeSender, *Service, *mail.Client) {
	t.Helper()
	key, err := addresses.LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	service := &Service{Sender: sender, From: "pi-appliance <ne-pas-repondre@pi-appliance.com>"}
	client := &mail.Client{URL: "https://envoi.pi-appliance.com", Key: key, HTTP: handlerDoer{service.Handler()}}
	return sender, service, client
}

func body(t *testing.T, message []byte) string {
	t.Helper()
	_, encoded, ok := bytes.Cut(message, []byte("\r\n\r\n"))
	if !ok {
		t.Fatalf("message sans corps : %q", message)
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	return string(decoded)
}

func TestSendResetLink(t *testing.T) {
	sender, _, client := setup(t)
	link := "https://vdl.moulard.org/pi-appliance/#reset=" + token
	if err := client.PasswordReset(auth.Public{Name: "marie", Email: "marie@example.org"}, link, "fr"); err != nil {
		t.Fatal(err)
	}
	message := sender.sent["marie@example.org"]
	text := body(t, message)
	if !strings.Contains(text, link) || !strings.Contains(text, "« marie »") || !strings.Contains(text, "dans l'heure") {
		t.Fatalf("texte : %s", text)
	}
	if !bytes.Contains(message, []byte("To: <marie@example.org>\r\n")) || !bytes.Contains(message, []byte("From: pi-appliance <ne-pas-repondre@pi-appliance.com>\r\n")) {
		t.Fatalf("en-têtes : %s", message)
	}
	if err := client.Invite(auth.Public{Name: "paul", Email: "paul@example.org"}, "http://192.168.1.30/pi-appliance/#invite="+token, "de"); err != nil {
		t.Fatal(err)
	}
	if text := body(t, sender.sent["paul@example.org"]); !strings.Contains(text, "within 7 days") {
		t.Fatalf("invitation en anglais attendue : %s", text)
	}
}

func TestRefusedRequests(t *testing.T) {
	_, _, client := setup(t)
	for _, request := range []mail.Request{
		{Kind: mail.KindReset, To: "marie@example.org", Name: "marie", Link: "https://pirate.example.org/autre/#reset=" + token},
		{Kind: mail.KindReset, To: "marie@example.org", Name: "marie", Link: "https://maison.example.org/pi-appliance/#invite=" + token},
		{Kind: mail.KindReset, To: "marie@example.org", Name: "marie", Link: "https://maison.example.org/pi-appliance/#reset=court"},
		{Kind: mail.KindReset, To: "Marie <marie@example.org>", Name: "marie", Link: "https://maison.example.org/pi-appliance/#reset=" + token},
		{Kind: mail.KindReset, To: "marie@example.org\r\nBcc: x@example.org", Name: "marie", Link: "https://maison.example.org/pi-appliance/#reset=" + token},
		{Kind: "spam", To: "marie@example.org", Name: "marie", Link: "https://maison.example.org/pi-appliance/#reset=" + token},
		{Kind: mail.KindReset, To: "marie@example.org", Name: "Texte libre", Link: "https://maison.example.org/pi-appliance/#reset=" + token},
	} {
		if err := client.Send(context.Background(), request); err == nil {
			t.Errorf("demande acceptée : %+v", request)
		}
	}
}

func TestSignatureAndReplay(t *testing.T) {
	sender, service, client := setup(t)
	handler := service.Handler()
	request := mail.Request{Kind: mail.KindReset, To: "marie@example.org", Name: "marie", Link: "https://maison.example.org/pi-appliance/#reset=" + token}
	payload, _ := json.Marshal(request)
	// Sans signature : refusé.
	unsigned := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewReader(payload))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, unsigned)
	if recorder.Code != http.StatusUnauthorized || len(sender.sent) != 0 {
		t.Fatalf("sans signature : code %d", recorder.Code)
	}
	// Une demande signée ne sert qu'une fois.
	signed := httptest.NewRequest(http.MethodPost, "/api/send", bytes.NewReader(payload))
	client.Key.Sign(signed, payload, time.Now())
	again := signed.Clone(context.Background())
	again.Body = io.NopCloser(bytes.NewReader(payload))
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, signed)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("signée : code %d : %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, again)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("rejouée : code %d", recorder.Code)
	}
}

func TestLimits(t *testing.T) {
	sender, _, client := setup(t)
	account := auth.Public{Name: "marie", Email: "marie@example.org"}
	link := "https://maison.example.org/pi-appliance/#reset=" + token
	// Une signature par seconde : avancer l'horloge pour ne pas rejouer la même demande.
	now := time.Now()
	client.Now = func() time.Time { now = now.Add(time.Second); return now }
	for i := range PerRecipientPerHour {
		if err := client.PasswordReset(account, link, "fr"); err != nil {
			t.Fatalf("message %d : %v", i+1, err)
		}
	}
	if err := client.PasswordReset(account, link, "fr"); err == nil || !strings.Contains(err.Error(), "trop de messages") {
		t.Fatalf("limite par destinataire : %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("destinataires : %d", len(sender.sent))
	}
}
