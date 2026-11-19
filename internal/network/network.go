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

// Package network pilote le réseau du Pi par le script network.sh de l'image
// (spécification, section 4) : état, Wi-Fi visibles, connexion, choix du câble.
package network

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultScript est le script installé par l'image.
const DefaultScript = "/usr/local/lib/pi-appliance/network.sh"

var (
	// ErrInvalid : demande refusée avant de toucher au réseau.
	ErrInvalid = errors.New("demande invalide")
)

// Runner lance network.sh avec ses arguments et son entrée standard ; il rend
// la sortie standard (toujours un objet JSON) et une erreur si le code de
// retour n'est pas 0.
type Runner interface {
	Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}

// Script lance le vrai network.sh.
type Script string

func (s Script) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, string(s), args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%w : %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

// Failure est un refus de network.sh : code pour la console, message en langage courant.
type Failure struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (f Failure) Error() string {
	return f.Message
}

// JoinRequest est la demande de connexion au Wi-Fi de la maison.
type JoinRequest struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
	Hidden   bool   `json:"hidden"`
}

// Validate refuse tôt ce que network.sh refuserait : la console répond avant
// de lancer la connexion, elle ne peut plus dire l'erreur après.
func (j JoinRequest) Validate() error {
	if j.SSID == "" || len(j.SSID) > 32 {
		return fmt.Errorf("%w : nom de Wi-Fi manquant ou trop long (32 octets au plus)", ErrInvalid)
	}
	if j.Password == "" {
		return nil
	}
	if strings.ContainsAny(j.Password, "\r\n") {
		return fmt.Errorf("%w : le mot de passe ne peut pas contenir de retour à la ligne", ErrInvalid)
	}
	if !utf8.ValidString(j.Password) || len(j.Password) < 8 || len(j.Password) > 64 {
		return fmt.Errorf("%w : le mot de passe d'un Wi-Fi fait de 8 à 63 caractères", ErrInvalid)
	}
	return nil
}

// Client parle à network.sh.
type Client struct {
	Runner Runner
	// JoinDelay laisse à la réponse HTTP le temps d'arriver au téléphone avant
	// que le point d'accès se ferme.
	JoinDelay time.Duration
	// Logf reçoit le résultat de la connexion, qui n'a plus de réponse HTTP.
	Logf func(format string, args ...any)
}

const (
	shortTimeout = 15 * time.Second
	scanTimeout  = 60 * time.Second
	joinTimeout  = 3 * time.Minute
)

func (c *Client) run(ctx context.Context, timeout time.Duration, stdin []byte, args ...string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := c.Runner.Run(ctx, stdin, args...)
	var failure Failure
	if err != nil {
		if json.Unmarshal(out, &failure) == nil && failure.Code != "" {
			return nil, failure
		}
		return nil, fmt.Errorf("network.sh %s : %w", strings.Join(args, " "), err)
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("network.sh %s : réponse illisible", strings.Join(args, " "))
	}
	return json.RawMessage(bytes.TrimSpace(out)), nil
}

// Status rend l'état du point d'accès, du câble et du Wi-Fi.
func (c *Client) Status(ctx context.Context) (json.RawMessage, error) {
	return c.run(ctx, shortTimeout, nil, "status")
}

// WifiList rend la dernière liste des Wi-Fi visibles.
func (c *Client) WifiList(ctx context.Context) (json.RawMessage, error) {
	return c.run(ctx, shortTimeout, nil, "wifi-list")
}

// WifiScan relance la recherche ; le point d'accès se ferme quelques secondes.
func (c *Client) WifiScan(ctx context.Context) (json.RawMessage, error) {
	return c.run(ctx, scanTimeout, nil, "wifi-scan")
}

// Wired garde le câble comme réseau du Pi.
func (c *Client) Wired(ctx context.Context) (json.RawMessage, error) {
	return c.run(ctx, shortTimeout, nil, "wired")
}

// SetCountry change le pays du Wi-Fi (code ISO 3166 à deux lettres majuscules).
func (c *Client) SetCountry(ctx context.Context, code string) (json.RawMessage, error) {
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return nil, fmt.Errorf("%w : code de pays invalide", ErrInvalid)
	}
	return c.run(ctx, shortTimeout, nil, "country", code)
}

// Join lance la connexion en arrière-plan, après JoinDelay, et rend la main
// tout de suite : le point d'accès va se fermer et le téléphone perdre la
// réponse. Le résultat se lit ensuite dans Status (lastError).
func (c *Client) Join(request JoinRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	go func() {
		time.Sleep(c.JoinDelay)
		_, err := c.run(context.Background(), joinTimeout, body, "wifi-join")
		// NE PAS journaliser la demande : elle contient le mot de passe du Wi-Fi.
		switch {
		case err != nil:
			c.logf("connexion au Wi-Fi %q : %v", request.SSID, err)
		default:
			c.logf("connecté au Wi-Fi %q", request.SSID)
		}
	}()
	return nil
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}
