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

package stats_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/stats"
)

// fakeClient garde les requêtes reçues et répond status.
type fakeClient struct {
	status   int
	err      error
	requests []*http.Request
	bodies   []string
}

func (f *fakeClient) Do(request *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	f.requests = append(f.requests, request)
	f.bodies = append(f.bodies, string(body))
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newReporter(t *testing.T, client *fakeClient, now *clock) *stats.Reporter {
	t.Helper()
	dir := t.TempDir()
	model := filepath.Join(dir, "model")
	if err := os.WriteFile(model, []byte("Raspberry Pi 4 Model B Rev 1.4\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := stats.Open(filepath.Join(dir, "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	r.Version = "1.2.3"
	r.ModelFile = model
	r.Client = client
	r.URL = "https://stats.example/api/report"
	r.Now = now.Now
	r.Installed = func() ([]string, func(string) bool, error) {
		known := func(app string) bool { return app == "nextcloud" || app == "dokuwiki" }
		return []string{"nextcloud", "compose-libre", "dokuwiki", "nextcloud"}, known, nil
	}
	return r
}

func TestDisabledByDefault(t *testing.T) {
	client := &fakeClient{status: http.StatusNoContent}
	r := newReporter(t, client, &clock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)})
	status := r.Status()
	if status.Enabled || status.Decided {
		t.Fatalf("statistiques actives sans accord : %+v", status)
	}
	if r.Send(context.Background()) || len(client.requests) != 0 {
		t.Fatal("envoi sans accord de l'administrateur")
	}
}

func TestPreviewIsExactlyWhatIsSent(t *testing.T) {
	client := &fakeClient{status: http.StatusNoContent}
	r := newReporter(t, client, &clock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)})
	if err := r.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	preview := r.Status().Report
	want := stats.Report{
		Format:         stats.Format,
		Version:        "1.2.3",
		Model:          "Raspberry Pi 4 Model B Rev 1.4",
		Services:       []string{"dokuwiki", "nextcloud", "nextcloud"},
		CustomServices: 1,
		Crashes:        []stats.Crash{},
	}
	if !reflect.DeepEqual(preview, want) {
		t.Fatalf("aperçu %+v, attendu %+v", preview, want)
	}
	if !r.Send(context.Background()) {
		t.Fatal("envoi refusé")
	}
	var sent stats.Report
	if err := json.Unmarshal([]byte(client.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent, preview) {
		t.Fatalf("envoyé %+v, montré %+v", sent, preview)
	}
	// Rien d'autre que le format : pas de champ en plus dans le JSON.
	var fields map[string]any
	json.Unmarshal([]byte(client.bodies[0]), &fields)
	for key := range fields {
		switch key {
		case "format", "version", "model", "services", "custom_services", "crashes":
		default:
			t.Errorf("champ inattendu %q", key)
		}
	}
	request := client.requests[0]
	if request.Method != http.MethodPost || request.URL.String() != "https://stats.example/api/report" {
		t.Errorf("requête %s %s", request.Method, request.URL)
	}
	for name := range request.Header {
		if name != "Content-Type" && name != "User-Agent" {
			t.Errorf("en-tête inattendu %q", name)
		}
	}
	if request.Header.Get("User-Agent") != stats.UserAgent {
		t.Errorf("User-Agent %q", request.Header.Get("User-Agent"))
	}
}

func TestAtMostOncePerDay(t *testing.T) {
	client := &fakeClient{status: http.StatusNoContent}
	now := &clock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	r := newReporter(t, client, now)
	r.SetEnabled(true)
	if !r.Send(context.Background()) {
		t.Fatal("premier envoi refusé")
	}
	now.now = now.now.Add(23 * time.Hour)
	if r.Send(context.Background()) {
		t.Fatal("deuxième envoi le même jour")
	}
	// Désactiver puis réactiver ne permet pas un envoi de plus.
	r.SetEnabled(false)
	r.SetEnabled(true)
	if r.Send(context.Background()) {
		t.Fatal("envoi de plus après réactivation")
	}
	now.now = now.now.Add(time.Hour)
	if !r.Send(context.Background()) {
		t.Fatal("envoi du lendemain refusé")
	}
	if len(client.requests) != 2 {
		t.Fatalf("%d envois, attendu 2", len(client.requests))
	}
	// Le dernier envoi survit à un redémarrage.
	reopened, err := stats.Open(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Client, reopened.Now, reopened.URL = client, now.Now, r.URL
	if reopened.Send(context.Background()) || !reopened.Status().Enabled {
		t.Fatal("le réglage ou la date du dernier envoi est perdu")
	}
}

func TestFailureIsRetried(t *testing.T) {
	client := &fakeClient{err: errors.New("dial tcp 203.0.113.7:443: refused")}
	now := &clock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	r := newReporter(t, client, now)
	r.SetEnabled(true)
	if r.Send(context.Background()) {
		t.Fatal("échec compté comme réussi")
	}
	status := r.Status()
	if status.Error != "pas de connexion à internet" || !status.LastSent.IsZero() {
		t.Fatalf("état après échec : %+v", status)
	}
	client.err, client.status = nil, http.StatusInternalServerError
	now.now = now.now.Add(stats.Interval)
	if r.Send(context.Background()) || !strings.Contains(r.Status().Error, "500") {
		t.Fatal("réponse 500 comptée comme réussie")
	}
	client.status = http.StatusNoContent
	now.now = now.now.Add(stats.Interval)
	if !r.Send(context.Background()) || r.Status().Error != "" {
		t.Fatal("nouvel essai refusé")
	}
}

func TestCrashes(t *testing.T) {
	client := &fakeClient{status: http.StatusNoContent}
	r := newReporter(t, client, &clock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)})
	r.RunningFile = filepath.Join(t.TempDir(), "running")
	handler := r.Guard(http.HandlerFunc(panicking))

	// Plantage avant l'accord : pas noté.
	serve(t, handler)
	r.SetEnabled(true)
	if crashes := r.Preview().Crashes; len(crashes) != 0 {
		t.Fatalf("plantage noté sans accord : %+v", crashes)
	}

	serve(t, handler)
	serve(t, handler)
	// Arrêt brutal : RunningFile est resté là au démarrage suivant.
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	want := []stats.Crash{
		{Where: stats.CrashUnclean, Count: 1},
		{Where: "internal/stats_test.panicking", Count: 2},
	}
	if got := r.Preview().Crashes; !reflect.DeepEqual(got, want) {
		t.Fatalf("plantages %+v, attendu %+v", got, want)
	}
	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.RunningFile); !os.IsNotExist(err) {
		t.Fatal("RunningFile toujours là après l'arrêt normal")
	}
	r.Send(context.Background())
	if strings.Contains(client.bodies[0], "secret") {
		t.Fatal("le message du plantage est parti")
	}
	if crashes := r.Preview().Crashes; len(crashes) != 0 {
		t.Fatalf("plantages envoyés toujours là : %+v", crashes)
	}
}

func panicking(http.ResponseWriter, *http.Request) {
	panic("nom secret de l'utilisateur")
}

// serve appelle le gestionnaire comme net/http, qui récupère le plantage.
func serve(t *testing.T, handler http.Handler) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("le plantage n'est pas transmis à net/http")
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestCheck(t *testing.T) {
	valid := stats.Report{Format: stats.Format, Version: "dev", Services: []string{}, Crashes: []stats.Crash{}}
	if err := valid.Check(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*stats.Report){
		"format":    func(r *stats.Report) { r.Format = 2 },
		"version":   func(r *stats.Report) { r.Version = "1.0 chez Martine" },
		"modèle":    func(r *stats.Report) { r.Model = "Pi\nde Martine <b>" },
		"service":   func(r *stats.Report) { r.Services = []string{"Mon Nextcloud"} },
		"services":  func(r *stats.Report) { r.Services = nil },
		"hors":      func(r *stats.Report) { r.CustomServices = -1 },
		"plantage":  func(r *stats.Report) { r.Crashes = []stats.Crash{{Where: "x y", Count: 1}} },
		"compte":    func(r *stats.Report) { r.Crashes = []stats.Crash{{Where: "x", Count: 0}} },
		"plantages": func(r *stats.Report) { r.Crashes = nil },
	} {
		report := valid
		change(&report)
		if err := report.Check(); !errors.Is(err, stats.ErrInvalid) {
			t.Errorf("%s : accepté", name)
		}
	}
}
