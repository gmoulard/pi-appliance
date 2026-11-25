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
