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

package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/auth"
)

// ErrUnreachable : le service d'envoi ne répond pas.
var ErrUnreachable = errors.New("le service d'envoi d'e-mails ne répond pas")

// Doer envoie une requête HTTP ; *http.Client en est un, les tests le remplacent.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Client : côté console du service d'envoi. Il signe ses demandes avec la clé
// de l'appliance (la même que celle de l'adresse pi-appliance.com).
// Il satisfait auth.Mailer.
type Client struct {
	URL  string
	Key  *addresses.Key
	HTTP Doer
	Now  func() time.Time
}

var _ auth.Mailer = (*Client)(nil)

// Invite envoie le lien d'invitation d'un nouveau compte.
func (c *Client) Invite(account auth.Public, link, lang string) error {
	return c.Send(context.Background(), Request{Kind: KindInvite, To: account.Email, Name: account.Name, Link: link, Lang: lang})
}

// PasswordReset envoie le lien « mot de passe oublié ».
func (c *Client) PasswordReset(account auth.Public, link, lang string) error {
	return c.Send(context.Background(), Request{Kind: KindReset, To: account.Email, Name: account.Name, Link: link, Lang: lang})
}

// Send demande l'envoi d'un message au service. NE PAS journaliser la demande :
// elle contient le lien, qui donne la main sur le compte pendant une heure.
func (c *Client) Send(ctx context.Context, request Request) error {
	request, err := request.Check()
	if err != nil {
		return err
	}
	if c.URL == "" || c.Key == nil {
		return ErrUnreachable
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+"/api/send", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	c.Key.Sign(r, payload, now())
	doer := c.HTTP
	if doer == nil {
		doer = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := doer.Do(r)
	if err != nil {
		return ErrUnreachable
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(response.Body, 16<<10))
	if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusOK || response.StatusCode == http.StatusNoContent {
		return nil
	}
	var refused ErrorBody
	if json.Unmarshal(data, &refused) == nil && refused.Error != "" {
		return fmt.Errorf("service d'envoi d'e-mails (%d) : %s", response.StatusCode, refused.Error)
	}
	return ErrUnreachable
}
