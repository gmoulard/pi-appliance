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

package ddns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const token = "a7c4d0ad-114e-40ef-ba1d-d217904a50f2"

// fakeClient remplace internet : l'adresse publique et la réponse de DuckDNS.
type fakeClient struct {
	mu      sync.Mutex
	ip      string
	answer  string
	fail    bool
	updates []string
	lookups int
}

func (f *fakeClient) Do(request *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, fmt.Errorf("Get %q: dial tcp: no route to host", request.URL.String())
	}
	body := ""
	switch request.URL.Host {
	case "ip.test":
		f.lookups++
		body = f.ip + "\n"
	case "duckdns.test":
		f.updates = append(f.updates, request.URL.RawQuery)
		body = f.answer
	default:
		return nil, errors.New("hôte inattendu")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newUpdater(t *testing.T) (*Updater, *fakeClient, *clock, *[]string) {
	t.Helper()
	u, err := Open(filepath.Join(t.TempDir(), "duckdns.json"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeClient{ip: "203.0.113.7", answer: "OK"}
	c := &clock{t: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)}
	var logs []string
	u.Client = fake
	u.UpdateURL = "https://duckdns.test/update"
	u.LookupURL = "https://ip.test/"
	u.Now = c.now
	u.Logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	return u, fake, c, &logs
}

func TestCheck(t *testing.T) {
	s, err := Settings{Domain: " MonPi.duckdns.org ", Token: strings.ToUpper(token)}.Check()
	if err != nil || s.Domain != "monpi" || s.Token != token {
		t.Fatalf("%+v %v", s, err)
	}
	for _, bad := range []Settings{
		{Domain: "", Token: token},
		{Domain: "mon_pi", Token: token},
		{Domain: "-pi", Token: token},
		{Domain: "pi&token=x", Token: token},
		{Domain: "monpi", Token: "abc"},
	} {
		if _, err := bad.Check(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepté", bad)
		}
	}
}

func TestNotConfigured(t *testing.T) {
	u, fake, _, _ := newUpdater(t)
	if delay := u.Update(context.Background()); delay != Interval {
		t.Fatalf("délai %s", delay)
	}
	if fake.lookups != 0 || len(fake.updates) != 0 || u.Status().Configured {
		t.Fatalf("appel sans réglages : %+v", fake)
	}
}

func TestUpdateOnlyWhenNeeded(t *testing.T) {
	u, fake, c, _ := newUpdater(t)
	if err := u.Save(Settings{Domain: "monpi", Token: token}); err != nil {
		t.Fatal(err)
	}
	if delay := u.Update(context.Background()); delay != Interval {
		t.Fatalf("délai %s", delay)
	}
	want := "domains=monpi&ip=203.0.113.7&token=" + token
	if len(fake.updates) != 1 || fake.updates[0] != want {
		t.Fatalf("mises à jour %v", fake.updates)
	}
	status := u.Status()
	if !status.Configured || status.Hostname != "monpi.duckdns.org" || status.IP != "203.0.113.7" || !status.LastSuccess.Equal(c.t) || status.Error != "" {
		t.Fatalf("état %+v", status)
	}
	// Même adresse : pas de mise à jour.
	c.t = c.t.Add(Interval)
	u.Update(context.Background())
	if len(fake.updates) != 1 || fake.lookups != 2 {
		t.Fatalf("mise à jour sans changement : %v", fake.updates)
	}
	// Nouvelle adresse : mise à jour.
	fake.ip = "198.51.100.4"
	u.Update(context.Background())
	if len(fake.updates) != 2 || !strings.Contains(fake.updates[1], "ip=198.51.100.4") {
		t.Fatalf("changement d'adresse ignoré : %v", fake.updates)
	}
	// Un jour sans changement : mise à jour quand même.
	c.t = c.t.Add(Refresh)
	u.Update(context.Background())
	if len(fake.updates) != 3 {
		t.Fatalf("pas de mise à jour quotidienne : %v", fake.updates)
	}
}

func TestBackoff(t *testing.T) {
	u, fake, c, logs := newUpdater(t)
	if err := u.Save(Settings{Domain: "monpi", Token: token}); err != nil {
		t.Fatal(err)
	}
	fake.answer = "KO"
	var delays []time.Duration
	for range 9 {
		delays = append(delays, u.Update(context.Background()))
		c.t = c.t.Add(time.Minute)
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour, time.Hour}
	if fmt.Sprint(delays) != fmt.Sprint(want) {
		t.Fatalf("délais %v", delays)
	}
	if status := u.Status(); !strings.Contains(status.Error, "refusé") || !status.LastSuccess.IsZero() {
		t.Fatalf("état %+v", status)
	}
	// Échec réseau : l'erreur ne contient pas l'URL, donc pas le jeton.
	fake.fail = true
	u.Update(context.Background())
	// Retour à la normale : délai normal, erreur effacée.
	fake.fail = false
	fake.answer = "OK"
	if delay := u.Update(context.Background()); delay != Interval || u.Status().Error != "" {
		t.Fatalf("délai %s, état %+v", delay, u.Status())
	}
	for _, line := range *logs {
		if strings.Contains(line, token) {
			t.Fatalf("jeton dans le journal : %s", line)
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", u.Status()), token) {
		t.Fatal("jeton dans l'état")
	}
}

func TestLookupFailure(t *testing.T) {
	u, fake, _, _ := newUpdater(t)
	if err := u.Save(Settings{Domain: "monpi", Token: token}); err != nil {
		t.Fatal(err)
	}
	fake.ip = "pas une adresse"
	if delay := u.Update(context.Background()); delay != RetryMin {
		t.Fatalf("délai %s", delay)
	}
	if len(fake.updates) != 0 || !strings.Contains(u.Status().Error, "adresse publique") {
		t.Fatalf("mises à jour %v, état %+v", fake.updates, u.Status())
	}
}

func TestFile(t *testing.T) {
	u, _, _, _ := newUpdater(t)
	if err := u.Save(Settings{Domain: "monpi", Token: token}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(u.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("droits %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(u.Path)
	if err != nil {
		t.Fatal(err)
	}
	var saved Settings
	if err := json.Unmarshal(data, &saved); err != nil || saved != (Settings{Domain: "monpi", Token: token}) {
		t.Fatalf("%s %v", data, err)
	}
	reopened, err := Open(u.Path)
	if err != nil || reopened.Status().Domain != "monpi" {
		t.Fatalf("%+v %v", reopened.Status(), err)
	}
	if err := u.Forget(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(u.Path); !os.IsNotExist(err) || u.Status().Configured {
		t.Fatalf("réglages non effacés : %v", err)
	}
}

func TestRunWakesOnSave(t *testing.T) {
	u, fake, _, _ := newUpdater(t)
	u.Logf = nil
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		u.Run(ctx)
		close(done)
	}()
	if err := u.Save(Settings{Domain: "monpi", Token: token}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		fake.mu.Lock()
		n := len(fake.updates)
		fake.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pas de mise à jour après l'enregistrement")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}
