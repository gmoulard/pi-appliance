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
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/metrics"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// newConfigFixture : console avec les réglages de base, modifiés par change.
func newConfigFixture(t *testing.T, change func(dir string, config *Config)) fixture {
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
	config := Config{
		Version:  "1.2.3",
		Catalog:  catalog,
		Services: &services.Manager{Dir: dir, Domain: "maison.example.org", Runner: okRunner{}},
		Accounts: accounts,
	}
	if change != nil {
		change(dir, &config)
	}
	return fixture{handler: Handler(config), accounts: accounts}
}

type historyResponse struct {
	StepSeconds int `json:"step_seconds"`
	Points      []struct {
		CPUPercent *float64 `json:"cpu_percent"`
	} `json:"points"`
}

func TestHistory(t *testing.T) {
	f := newConfigFixture(t, func(dir string, config *Config) {
		config.History = &metrics.History{Path: filepath.Join(dir, "history.json")}
		cpu := 12.5
		config.History.Record(metrics.Sample{CPUPercent: &cpu})
	})
	if response := f.do(http.MethodGet, "/pi-appliance/api/system/history", "", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("sans session : code %d", response.Code)
	}
	history := decode[historyResponse](t, f.do(http.MethodGet, "/pi-appliance/api/system/history", "", "", f.admin(t)))
	if history.StepSeconds != 3600 || len(history.Points) != 1 || *history.Points[0].CPUPercent != 12.5 {
		t.Fatalf("%+v", history)
	}
}

func TestHistoryAbsent(t *testing.T) {
	f := newConfigFixture(t, nil)
	if history := decode[historyResponse](t, f.do(http.MethodGet, "/pi-appliance/api/system/history", "", "", f.admin(t))); history.Points == nil || len(history.Points) != 0 {
		t.Fatalf("%+v", history)
	}
}
