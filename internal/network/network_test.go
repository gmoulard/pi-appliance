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

package network

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// Fake remplace network.sh : une réponse et un code par commande.
type Fake struct {
	mu     sync.Mutex
	Out    map[string]string
	Fail   map[string]bool
	Calls  []string
	Stdin  []byte
	Joined chan struct{}
}

func (f *Fake) Run(_ context.Context, stdin []byte, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, strings.Join(args, " "))
	if args[0] == "wifi-join" {
		f.Stdin = stdin
		if f.Joined != nil {
			defer close(f.Joined)
		}
	}
	if f.Fail[args[0]] {
		return []byte(f.Out[args[0]]), errors.New("exit status 1")
	}
	return []byte(f.Out[args[0]]), nil
}

func TestStatus(t *testing.T) {
	fake := &Fake{Out: map[string]string{"status": `{"ok": true, "configured": ""}` + "\n"}}
	out, err := (&Client{Runner: fake}).Status(context.Background())
	if err != nil || string(out) != `{"ok": true, "configured": ""}` {
		t.Fatalf("%s, %v", out, err)
	}
}

func TestFailure(t *testing.T) {
	fake := &Fake{
		Out:  map[string]string{"wired": `{"ok": false, "error": "no-cable", "message": "Aucun câble."}`},
		Fail: map[string]bool{"wired": true},
	}
	_, err := (&Client{Runner: fake}).Wired(context.Background())
	var failure Failure
	if !errors.As(err, &failure) || failure.Code != "no-cable" || failure.Message != "Aucun câble." {
		t.Fatalf("%v", err)
	}
}

func TestUnreadable(t *testing.T) {
	fake := &Fake{Out: map[string]string{"status": "pas du JSON"}}
	if _, err := (&Client{Runner: fake}).Status(context.Background()); err == nil {
		t.Fatal("réponse illisible acceptée")
	}
	fake = &Fake{Out: map[string]string{}, Fail: map[string]bool{"status": true}}
	if _, err := (&Client{Runner: fake}).Status(context.Background()); err == nil {
		t.Fatal("échec sans JSON accepté")
	}
}

func TestValidate(t *testing.T) {
	for _, request := range []JoinRequest{
		{},
		{SSID: strings.Repeat("a", 33)},
		{SSID: "maison", Password: "court"},
		{SSID: "maison", Password: strings.Repeat("a", 65)},
		{SSID: "maison", Password: "avec un\nretour"},
	} {
		if err := request.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepté", request)
		}
	}
	for _, request := range []JoinRequest{
		{SSID: "maison"},
		{SSID: "maison", Password: "12345678", Hidden: true},
		{SSID: strings.Repeat("a", 32), Password: strings.Repeat("a", 64)},
	} {
		if err := request.Validate(); err != nil {
			t.Errorf("%+v refusé : %v", request, err)
		}
	}
}

func TestJoin(t *testing.T) {
	fake := &Fake{Out: map[string]string{"wifi-join": `{"ok": true, "ssid": "maison"}`}, Joined: make(chan struct{})}
	var logs []string
	var mu sync.Mutex
	client := &Client{Runner: fake, Logf: func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, format)
	}}
	if err := client.Join(JoinRequest{SSID: "maison", Password: "mot de passe"}); err != nil {
		t.Fatal(err)
	}
	<-fake.Joined
	fake.mu.Lock()
	var sent JoinRequest
	if err := json.Unmarshal(fake.Stdin, &sent); err != nil || sent.SSID != "maison" || sent.Password != "mot de passe" {
		t.Fatalf("entrée standard %s : %v", fake.Stdin, err)
	}
	fake.mu.Unlock()
	if err := client.Join(JoinRequest{SSID: ""}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("SSID vide : %v", err)
	}
}
