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
	"time"

	"github.com/gmoulard/pi-appliance/internal/mail"
)

// Compose écrit le message complet (en-têtes et corps) d'une demande vérifiée,
// avec les textes fixes du paquet mail (les mêmes que l'envoi par SMTP du Pi).
func Compose(from string, request mail.Request, now time.Time) ([]byte, error) {
	return mail.Compose(from, mail.Message{Kind: request.Kind, To: request.To, Name: request.Name, Link: request.Link, Lang: request.Lang}, now)
}
