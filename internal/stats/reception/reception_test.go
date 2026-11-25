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

package reception

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gmoulard/pi-appliance/internal/stats"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 11, 20, 23, 30, 0, 0, time.FixedZone("Paris", 3600)) }}
}

func post(handler http.Handler, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "198.51.100.23")
	request.RemoteAddr = "203.0.113.7:51234"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

const valid = `{"format":1,"version":"1.2.3","model":"Raspberry Pi 4 Model B Rev 1.4","services":["nextcloud"],"custom_services":1,"crashes":[{"where":"arret-brutal","count":1}]}`

func TestReceiveWithoutAddress(t *testing.T) {
	store := newStore(t)
	handler := Handler(store, false)
	if response := post(handler, valid); response.Code != http.StatusNoContent {
		t.Fatalf("code %d : %s", response.Code, response.Body)
	}
	data, err := os.ReadFile(filepath.Join(store.Dir, "2026-11-20.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	for _, forbidden := range []string{"203.0.113.7", "198.51.100.23", "51234", "23:30", "22:30"} {
		if strings.Contains(line, forbidden) {
			t.Errorf("%q enregistré : %s", forbidden, line)
		}
	}
	if !strings.HasPrefix(line, `{"day":"2026-11-20","format":1,`) {
		t.Errorf("ligne inattendue : %s", line)
	}
	if info, _ := os.Stat(filepath.Join(store.Dir, "2026-11-20.jsonl")); info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("droits %v", info.Mode().Perm())
	}
}

func TestRejected(t *testing.T) {
	store := newStore(t)
	handler := Handler(store, false)
	for name, body := range map[string]string{
		"illisible":     `{`,
		"champ en plus": strings.Replace(valid, `"format":1`, `"format":1,"ip":"203.0.113.7"`, 1),
		"nom":           strings.Replace(valid, `["nextcloud"]`, `["Photos de Martine"]`, 1),
		"deux envois":   valid + valid,
		"trop gros":     strings.Replace(valid, `"1.2.3"`, `"`+strings.Repeat("a", stats.MaxBytes)+`"`, 1),
	} {
		if response := post(handler, body); response.Code != http.StatusBadRequest {
			t.Errorf("%s : code %d", name, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/report", strings.NewReader(valid))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Errorf("sans Content-Type : code %d", recorder.Code)
	}
	if entries, _ := os.ReadDir(store.Dir); len(entries) != 0 {
		t.Errorf("envoi refusé enregistré : %v", entries)
	}
}

func TestSummary(t *testing.T) {
	store := newStore(t)
	public := Handler(store, false)
	post(public, valid)
	post(public, strings.Replace(valid, `"custom_services":1`, `"custom_services":2`, 1))
	request := httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	recorder := httptest.NewRecorder()
	public.ServeHTTP(recorder, request)
	if recorder.Code == http.StatusOK {
		t.Fatal("résumé exposé sur le service public")
	}
	recorder = httptest.NewRecorder()
	Handler(store, true).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/summary?day=2026-11-20", nil))
	var summary Summary
	if err := json.Unmarshal(recorder.Body.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Reports != 2 || summary.Services["nextcloud"] != 2 || summary.Custom != 3 || summary.Crashes["arret-brutal"] != 2 || summary.Versions["1.2.3"] != 2 {
		t.Fatalf("résumé %+v", summary)
	}
	if _, err := store.Summarize("../etc/passwd"); err == nil {
		t.Fatal("jour invalide accepté")
	}
}

// TestFromAppliance : un envoi de l'appliance est accepté tel quel.
func TestFromAppliance(t *testing.T) {
	store := newStore(t)
	server := httptest.NewServer(Handler(store, false))
	defer server.Close()
	reporter, err := stats.Open(filepath.Join(t.TempDir(), "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	reporter.Version, reporter.URL, reporter.Client = "dev", server.URL+"/api/report", server.Client()
	reporter.ModelFile = filepath.Join(t.TempDir(), "absent")
	reporter.SetEnabled(true)
	if !reporter.Send(context.Background()) {
		t.Fatalf("envoi refusé : %s", reporter.Status().Error)
	}
	if summary, _ := store.Summarize(store.day()); summary.Reports != 1 {
		t.Fatalf("résumé %+v", summary)
	}
}
