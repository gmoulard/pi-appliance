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

package services

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// psRunner répond à « compose ps » avec une sortie donnée.
type psRunner struct {
	fakeRunner
	ps    string
	psErr error
}

func (p *psRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	output, err := p.fakeRunner.Run(ctx, dir, args...)
	if strings.Contains(strings.Join(args, " "), " ps ") {
		return []byte(p.ps), p.psErr
	}
	return output, err
}

func installed(t *testing.T, runner Runner) *Manager {
	t.Helper()
	m := &Manager{Dir: t.TempDir(), Domain: "maison.example.org", Runner: runner}
	if _, err := m.Install(context.Background(), nextcloud(t), "cloud", nil); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestControl(t *testing.T) {
	runner := &fakeRunner{}
	m := installed(t, runner)
	cases := map[Action]string{
		Start:   "up --detach --wait",
		Stop:    "--file pi-appliance.override.json stop",
		Restart: "--file pi-appliance.override.json restart",
	}
	for action, call := range cases {
		runner.calls = nil
		if err := m.Control(context.Background(), "cloud", action); err != nil {
			t.Fatalf("%s : %v", action, err)
		}
		if !runner.called("compose --project-name pi-cloud") || !runner.called(call) {
			t.Fatalf("%s : appels %v", action, runner.calls)
		}
	}
}

func TestControlErrors(t *testing.T) {
	runner := &fakeRunner{}
	m := installed(t, runner)
	if err := m.Control(context.Background(), "cloud", "kill"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("action inconnue : %v", err)
	}
	for _, name := range []string{"inconnu", "../etc"} {
		if err := m.Control(context.Background(), name, Stop); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s : %v", name, err)
		}
	}
	runner.fail = map[string]bool{" stop": true}
	if err := m.Control(context.Background(), "cloud", Stop); err == nil || !strings.Contains(err.Error(), "erreur docker") || !strings.Contains(err.Error(), "arrêté") {
		t.Fatalf("échec de docker : %v", err)
	}
}

func TestStatus(t *testing.T) {
	const db = `{"Service":"db","State":"running","Health":""}`
	cases := map[string]struct {
		ps    string
		color Color
	}{
		"tout en marche, une ligne par conteneur": {db + "\n" + `{"Service":"app","State":"running","Health":"healthy"}`, Green},
		"tout en marche, tableau JSON":            {`[` + db + `,{"Service":"app","State":"running","Health":""}]`, Green},
		"démarrage":                               {db + "\n" + `{"Service":"app","State":"running","Health":"starting"}`, Orange},
		"redémarrage en boucle":                   {db + "\n" + `{"Service":"app","State":"restarting","Health":""}`, Orange},
		"en difficulté":                           {db + "\n" + `{"Service":"app","State":"running","Health":"unhealthy"}`, Orange},
		"une partie arrêtée":                      {db + "\n" + `{"Service":"app","State":"exited","Health":""}`, Orange},
		"tout arrêté":                             {`{"Service":"db","State":"exited"}` + "\n" + `{"Service":"app","State":"exited"}`, Red},
		"aucun conteneur":                         {"", Red},
		"réponse illisible":                       {"{pas du json", Orange},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			m := installed(t, &psRunner{ps: c.ps})
			status, err := m.Status(context.Background(), "cloud")
			if err != nil || status.Color != c.color || status.Message == "" {
				t.Fatalf("état %+v, erreur %v", status, err)
			}
		})
	}
}

func TestStatusDockerDown(t *testing.T) {
	m := installed(t, &psRunner{psErr: errors.New("exit status 1")})
	status, err := m.Status(context.Background(), "cloud")
	if err != nil || status.Color != Orange || !strings.Contains(status.Message, "inconnu") {
		t.Fatalf("état %+v, erreur %v", status, err)
	}
	if _, err := m.Status(context.Background(), "inconnu"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("service inconnu : %v", err)
	}
}
