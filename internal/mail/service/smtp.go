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
	"bufio"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
)

// SMTP envoie par un relais SMTP authentifié, en STARTTLS (port 587) :
// net/smtp refuse d'envoyer l'identifiant sans chiffrement.
type SMTP struct {
	// Addr : hôte:port du relais, par exemple email-smtp.eu-west-3.amazonaws.com:587.
	Addr     string
	Username string
	Password string
	// Envelope : adresse de l'expéditeur dans l'enveloppe SMTP.
	Envelope string
}

func (s SMTP) Send(to string, message []byte) error {
	host, _, err := net.SplitHostPort(s.Addr)
	if err != nil {
		return err
	}
	return smtp.SendMail(s.Addr, smtp.PlainAuth("", s.Username, s.Password, host), s.Envelope, []string{to}, message)
}

// LoadSMTP lit les identifiants du relais : fichier en droits 600, lignes
// username=… et password=… (jamais dans le dépôt).
func LoadSMTP(path, addr, from string) (SMTP, error) {
	file, err := os.Open(path)
	if err != nil {
		return SMTP{}, err
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Mode().Perm()&0o077 != 0 {
		return SMTP{}, fmt.Errorf("%s : droits trop larges, attendus 600", path)
	}
	relay := SMTP{Addr: addr}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "username":
			relay.Username = strings.TrimSpace(value)
		case "password":
			relay.Password = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return SMTP{}, err
	}
	if relay.Username == "" || relay.Password == "" {
		return SMTP{}, fmt.Errorf("%s : username= et password= attendus", path)
	}
	address, err := mail.ParseAddress(from)
	if err != nil {
		return SMTP{}, fmt.Errorf("expéditeur %q : %w", from, err)
	}
	relay.Envelope = address.Address
	return relay, nil
}
