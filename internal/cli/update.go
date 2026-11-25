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

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Mise à jour du socle (pia update), par l'API de la console comme le bouton
// de « Mon Pi → Mises à jour » : la console télécharge l'archive de la
// version, la vérifie, puis updates.sh l'installe et remet l'ancienne version
// si la nouvelle ne démarre pas.

// Remplacés dans les tests.
var (
	updatePoll    = 2 * time.Second
	updateTimeout = 20 * time.Minute
	checkWait     = 30 * time.Second
)

type updateStatus struct {
	System *struct {
		Upgrade *struct {
			State      string `json:"state"`
			Version    string `json:"version"`
			Message    string `json:"message"`
			RolledBack bool   `json:"rolled_back"`
			At         string `json:"at"`
		} `json:"upgrade"`
	} `json:"system"`
	Appliance struct {
		Current     string `json:"current"`
		Channel     string `json:"channel"`
		Latest      string `json:"latest"`
		Available   bool   `json:"available"`
		Installable bool   `json:"installable"`
		Error       string `json:"error"`
		Download    *struct {
			State   string `json:"state"`
			Version string `json:"version"`
			Message string `json:"message"`
		} `json:"download"`
	} `json:"appliance"`
	Checking bool `json:"checking"`
}

// upgradeAt : date du dernier état de l'installation (vide : aucune).
func (s updateStatus) upgradeAt() string {
	if s.System == nil || s.System.Upgrade == nil {
		return ""
	}
	return s.System.Upgrade.At
}

type publishedVersion struct {
	Version     string `json:"version"`
	Preview     bool   `json:"preview"`
	Installable bool   `json:"installable"`
	Current     bool   `json:"current"`
	Newer       bool   `json:"newer"`
}

func updateCommand(env Env, args []string) error {
	f, err := parseFlags(args, nil, []string{"oui"})
	if err != nil {
		return err
	}
	if len(f.positional) > 1 {
		return usageError{"Une seule version à la fois : pia update [list | <version>]."}
	}
	if len(f.positional) == 1 {
		switch strings.ToLower(f.positional[0]) {
		case "list", "liste", "versions":
			return updateList(env)
		}
		return updateTo(env, f.positional[0], f.set["oui"])
	}
	return updateLatest(env, f.set["oui"])
}

// freshStatus relance la recherche de la dernière version et attend sa fin.
func freshStatus(ctx context.Context, env Env) (updateStatus, error) {
	var status updateStatus
	if err := call(ctx, env, http.MethodPost, "updates/check", nil, nil); err != nil {
		return status, err
	}
	deadline := time.Now().Add(checkWait)
	for {
		if err := call(ctx, env, http.MethodGet, "updates", nil, &status); err != nil {
			return status, err
		}
		if !status.Checking || time.Now().After(deadline) {
			return status, nil
		}
		time.Sleep(updatePoll / 4)
	}
}

func updateLatest(env Env, yes bool) error {
	ctx := context.Background()
	status, err := freshStatus(ctx, env)
	if err != nil {
		return err
	}
	current := status.Appliance.Current
	if status.Appliance.Error != "" {
		return errors.New(status.Appliance.Error)
	}
	if !status.Appliance.Available {
		fmt.Fprintf(env.Out, "pi-appliance est à jour (%s, canal %s).\n", current, status.Appliance.Channel)
		return nil
	}
	latest := status.Appliance.Latest
	if !status.Appliance.Installable {
		return fmt.Errorf("La version %s est sortie, mais elle ne s'installe pas d'ici pour l'instant (installation déjà en cours, ou image à flasher). Voir : pia update list", latest)
	}
	if !yes && !confirm(env, fmt.Sprintf("Installer pi-appliance %s (le Pi est en %s) ? La console et les services restent en place ; la console redémarre.", latest, current)) {
		fmt.Fprintln(env.Out, "Rien n'a été changé.")
		return nil
	}
	return startUpdate(ctx, env, latest, status.upgradeAt(), nil)
}

func updateTo(env Env, wanted string, yes bool) error {
	ctx := context.Background()
	tag := wanted
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	list, err := publishedVersions(ctx, env)
	if err != nil {
		return err
	}
	versions, current := list.Versions, list.Current
	var chosen *publishedVersion
	for i := range versions {
		if versions[i].Version == tag {
			chosen = &versions[i]
		}
	}
	switch {
	case chosen == nil:
		return fmt.Errorf("La version %s n'est pas dans la liste des versions publiées. Voir : pia update list", tag)
	case chosen.Current:
		fmt.Fprintf(env.Out, "Le Pi est déjà en %s.\n", tag)
		return nil
	case !chosen.Installable:
		return fmt.Errorf("La version %s ne s'installe pas d'ici : il faut flasher son image sur la carte SD.", tag)
	}
	question := fmt.Sprintf("Installer pi-appliance %s (le Pi est en %s) ? La console redémarre.", tag, current)
	if !chosen.Newer {
		question = fmt.Sprintf("Revenir à pi-appliance %s, plus ancienne que %s ? Les réglages faits depuis peuvent ne pas être compris par cette version ; si elle ne démarre pas, %s est remise.", tag, current, current)
	}
	if !yes && !confirm(env, question) {
		fmt.Fprintln(env.Out, "Rien n'a été changé.")
		return nil
	}
	var status updateStatus
	if err := call(ctx, env, http.MethodGet, "updates", nil, &status); err != nil {
		return err
	}
	return startUpdate(ctx, env, tag, status.upgradeAt(), map[string]string{"version": tag})
}

type versionList struct {
	Current  string             `json:"current"`
	Channel  string             `json:"channel"`
	Versions []publishedVersion `json:"versions"`
}

func publishedVersions(ctx context.Context, env Env) (versionList, error) {
	var reply versionList
	err := call(ctx, env, http.MethodGet, "updates/versions", nil, &reply)
	var failure apiError
	if errors.As(err, &failure) && failure.status == http.StatusNotFound {
		return reply, errors.New("Cette console ne sait pas encore donner la liste des versions : faites d'abord pia update (dernière version).")
	}
	return reply, err
}

func updateList(env Env) error {
	reply, err := publishedVersions(context.Background(), env)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "Versions de pi-appliance publiées (canal %s), la plus récente en premier :\n", reply.Channel)
	if len(reply.Versions) == 0 {
		fmt.Fprintln(env.Out, "  aucune pour l'instant.")
	}
	for i, v := range reply.Versions {
		var notes []string
		if i == 0 {
			notes = append(notes, "dernière")
		}
		if v.Current {
			notes = append(notes, "installée sur ce Pi")
		}
		if v.Preview {
			notes = append(notes, "bêta")
		}
		if !v.Installable && !v.Current {
			notes = append(notes, "image à flasher, pas installable d'ici")
		}
		line := "  " + v.Version
		if len(notes) > 0 {
			line += "  (" + strings.Join(notes, ", ") + ")"
		}
		fmt.Fprintln(env.Out, line)
	}
	fmt.Fprintln(env.Out, "Installer une version : pia update <version> ; la dernière : pia update")
	return nil
}

// startUpdate demande l'installation de version puis la suit jusqu'au bout ;
// before : date du dernier état de l'installation avant la demande, pour ne
// pas prendre la fin d'une ancienne installation pour celle-ci.
func startUpdate(ctx context.Context, env Env, version, before string, body any) error {
	if err := call(ctx, env, http.MethodPost, "updates/appliance", body, nil); err != nil {
		return err
	}
	fmt.Fprintf(env.Out, "Installation de pi-appliance %s :\n", version)
	fmt.Fprintln(env.Out, "  téléchargement et vérification…")
	step, unreachable := "download", false
	deadline := time.Now().Add(updateTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(updatePoll)
		var status updateStatus
		if err := call(ctx, env, http.MethodGet, "updates", nil, &status); err != nil {
			// La console redémarre avec la nouvelle version : elle ne répond
			// pas quelques secondes, puis avec un nouveau jeton (relu à chaque appel).
			if !unreachable {
				fmt.Fprintln(env.Out, "  la console redémarre…")
				unreachable = true
			}
			continue
		}
		unreachable = false
		if d := status.Appliance.Download; d != nil && d.Version == version && d.State == "failed" {
			return errors.New(d.Message)
		}
		if status.System == nil || status.System.Upgrade == nil || status.upgradeAt() == before || status.System.Upgrade.Version != version {
			continue
		}
		upgrade := status.System.Upgrade
		switch upgrade.State {
		case "done":
			fmt.Fprintf(env.Out, "pi-appliance %s est installé.\n", version)
			return nil
		case "failed":
			if upgrade.RolledBack {
				return fmt.Errorf("%s (l'ancienne version a été remise : rien n'a changé)", upgrade.Message)
			}
			return errors.New(upgrade.Message)
		default:
			if step != "install" {
				fmt.Fprintln(env.Out, "  installation (la console redémarre, 2 minutes au plus)…")
				step = "install"
			}
		}
	}
	return errors.New("L'installation dure plus que prévu. Voir : Mon Pi → Mises à jour, ou pia version dans quelques minutes.")
}
