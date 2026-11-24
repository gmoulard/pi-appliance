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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gmoulard/pi-appliance/internal/auth"
)

// Envoi des e-mails par le serveur SMTP choisi par l'administrateur, depuis le
// Pi lui-même (choix de l'utilisateur du 2026-11-22, spécification 10.2) : la
// boîte e-mail de l'administrateur (Gmail, OVH, Free, Orange…) envoie les
// invitations, les liens « mot de passe oublié », les codes de connexion et de
// vérification. Le service d'envoi du projet (Client) reste possible mais
// n'est plus nécessaire : la console préfère ce réglage quand il est fait.
//
// Réglage gardé dans <données>/smtp.json, droits 600. NE PAS renvoyer le mot
// de passe par l'API ni l'écrire dans les journaux : Status ne dit que s'il
// est enregistré.

// Sécurité de la connexion au serveur : jamais en clair, le mot de passe y passe.
const (
	SecuritySTARTTLS = "starttls"
	SecurityTLS      = "tls"
)

const (
	smtpTimeout = 30 * time.Second
	// smtpPerHour : e-mails envoyés par heure au plus, pour ne pas faire bloquer
	// la boîte de l'administrateur si quelqu'un abuse d'un formulaire.
	smtpPerHour = 60
	passwordMax = 256
)

var (
	// ErrNotConfigured : l'envoi des e-mails n'est pas réglé sur ce Pi.
	ErrNotConfigured = errors.New("l'envoi des e-mails n'est pas réglé sur ce Pi")
	// ErrTooMany : trop d'e-mails envoyés dans l'heure.
	ErrTooMany  = errors.New("trop d'e-mails envoyés dans l'heure, réessayez plus tard")
	hostPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,62}$`)
)

// SMTPSettings : le réglage enregistré, mot de passe compris (jamais renvoyé).
type SMTPSettings struct {
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Security  string    `json:"security"`
	Username  string    `json:"username,omitempty"`
	Password  string    `json:"password,omitempty"`
	From      string    `json:"from"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	UpdatedBy string    `json:"updated_by,omitempty"`
}

// SMTPStatus : le réglage tel que l'API le rend, sans le mot de passe.
type SMTPStatus struct {
	Configured  bool      `json:"configured"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Security    string    `json:"security"`
	Username    string    `json:"username"`
	From        string    `json:"from"`
	PasswordSet bool      `json:"password_set"`
	UpdatedAt   time.Time `json:"updated_at,omitzero"`
	UpdatedBy   string    `json:"updated_by,omitempty"`
}

// SMTPError : un envoi refusé, avec ce qu'il faut vérifier, en langage courant.
// NE PAS y mettre le mot de passe ni le contenu du message.
type SMTPError struct {
	Advice string
	Err    error
}

func (e SMTPError) Error() string { return e.Advice }
func (e SMTPError) Unwrap() error { return e.Err }

// SMTP : le réglage et l'envoi. Sa méthode Invite, PasswordReset et SendCode
// en font l'expéditeur de la console (auth.Mailer et auth.CodeMailer).
type SMTP struct {
	path string
	// RootCAs : autorités acceptées pour le certificat du serveur ; nil : celles
	// du système (remplacé dans les tests par le faux serveur).
	RootCAs *x509.CertPool
	Now     func() time.Time

	mu       sync.Mutex
	settings *SMTPSettings
	sent     []time.Time
}

// OpenSMTP lit le réglage ; absent, l'envoi n'est pas réglé.
func OpenSMTP(path string) (*SMTP, error) {
	s := &SMTP{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var settings SMTPSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("%s : %w", path, err)
	}
	s.settings = &settings
	return s, nil
}

func (s *SMTP) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Configured : l'envoi est réglé.
func (s *SMTP) Configured() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings != nil
}

// Status : le réglage sans le mot de passe.
func (s *SMTP) Status() SMTPStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settings == nil {
		return SMTPStatus{Port: 587, Security: SecuritySTARTTLS}
	}
	v := s.settings
	return SMTPStatus{Configured: true, Host: v.Host, Port: v.Port, Security: v.Security, Username: v.Username, From: v.From, PasswordSet: v.Password != "", UpdatedAt: v.UpdatedAt, UpdatedBy: v.UpdatedBy}
}

// Check vérifie un réglage et le renvoie nettoyé ; current : le réglage
// enregistré, dont le mot de passe est gardé si le nouveau est vide et que le
// serveur et l'identifiant n'ont pas changé.
func (v SMTPSettings) Check(current *SMTPSettings) (SMTPSettings, error) {
	v.Host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(v.Host), "."))
	if (!hostPattern.MatchString(v.Host) && net.ParseIP(v.Host) == nil) || len(v.Host) > 253 {
		return SMTPSettings{}, fmt.Errorf("%w : le nom du serveur d'envoi « %s » ne semble pas valide (par exemple smtp.gmail.com)", ErrInvalid, v.Host)
	}
	switch v.Security {
	case SecuritySTARTTLS, SecurityTLS:
	default:
		return SMTPSettings{}, fmt.Errorf("%w : choisissez la sécurité STARTTLS (port 587 le plus souvent) ou TLS (port 465)", ErrInvalid)
	}
	if v.Port == 0 {
		v.Port = 587
		if v.Security == SecurityTLS {
			v.Port = 465
		}
	}
	if v.Port < 1 || v.Port > 65535 {
		return SMTPSettings{}, fmt.Errorf("%w : le port doit être un nombre entre 1 et 65535 (587 ou 465 le plus souvent)", ErrInvalid)
	}
	v.Username = strings.TrimSpace(v.Username)
	if len(v.Username) > 254 || strings.IndexFunc(v.Username, unicode.IsControl) >= 0 {
		return SMTPSettings{}, fmt.Errorf("%w : identifiant illisible", ErrInvalid)
	}
	if v.Password == "" && current != nil && current.Host == v.Host && current.Username == v.Username {
		v.Password = current.Password
	}
	if len(v.Password) > passwordMax || strings.IndexFunc(v.Password, unicode.IsControl) >= 0 {
		return SMTPSettings{}, fmt.Errorf("%w : mot de passe illisible (%d caractères au plus)", ErrInvalid, passwordMax)
	}
	if v.Username != "" && v.Password == "" {
		return SMTPSettings{}, fmt.Errorf("%w : entrez le mot de passe du compte e-mail (pour Gmail, un mot de passe d'application)", ErrInvalid)
	}
	v.From = strings.TrimSpace(v.From)
	if v.From == "" && strings.Contains(v.Username, "@") {
		v.From = v.Username
	}
	address, err := mail.ParseAddress(v.From)
	if err != nil || address.Address != v.From || address.Name != "" || len(v.From) > 254 {
		return SMTPSettings{}, fmt.Errorf("%w : l'adresse d'expédition « %s » ne semble pas valide", ErrInvalid, v.From)
	}
	return v, nil
}

// Save enregistre un nouveau réglage (droits 600). Un mot de passe vide garde
// l'ancien, si le serveur et l'identifiant ne changent pas.
func (s *SMTP) Save(v SMTPSettings, by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, err := v.Check(s.settings)
	if err != nil {
		return err
	}
	v.UpdatedAt, v.UpdatedBy = s.now().UTC(), by
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary := s.path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, s.path); err != nil {
		return err
	}
	s.settings = &v
	return nil
}

// Forget efface le réglage : le Pi n'envoie plus d'e-mail par ce serveur.
func (s *SMTP) Forget() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.settings = nil
	return nil
}

// Send compose et envoie un message par le serveur réglé.
func (s *SMTP) Send(m Message) error {
	s.mu.Lock()
	if s.settings == nil {
		s.mu.Unlock()
		return ErrNotConfigured
	}
	settings := *s.settings
	now := s.now()
	kept := s.sent[:0]
	for _, at := range s.sent {
		if now.Sub(at) < time.Hour {
			kept = append(kept, at)
		}
	}
	s.sent = kept
	if len(s.sent) >= smtpPerHour {
		s.mu.Unlock()
		return ErrTooMany
	}
	s.sent = append(s.sent, now)
	s.mu.Unlock()
	address, err := mail.ParseAddress(m.To)
	if err != nil || address.Address != m.To {
		return fmt.Errorf("%w : adresse e-mail", ErrInvalid)
	}
	message, err := Compose("pi-appliance <"+settings.From+">", m, now)
	if err != nil {
		return err
	}
	return s.deliver(settings, m.To, message)
}

// Test envoie l'e-mail d'essai à to, avec le réglage enregistré.
func (s *SMTP) Test(to, name, lang string) error {
	return s.Send(Message{Kind: KindTest, To: to, Name: name, Lang: lang})
}

// deliver : une connexion par message, chiffrée avant l'identifiant.
func (s *SMTP) deliver(v SMTPSettings, to string, message []byte) error {
	addr := net.JoinHostPort(v.Host, fmt.Sprint(v.Port))
	config := &tls.Config{ServerName: v.Host, MinVersion: tls.VersionTLS12, RootCAs: s.RootCAs}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if v.Security == SecurityTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, config)
		if err != nil {
			return connectError(err, v)
		}
	} else {
		conn, err = dialer.Dial("tcp", addr)
		if err != nil {
			return connectError(err, v)
		}
	}
	conn.SetDeadline(time.Now().Add(smtpTimeout))
	client, err := smtp.NewClient(conn, v.Host)
	if err != nil {
		conn.Close()
		return SMTPError{Advice: "Le serveur d'envoi ne répond pas comme prévu : vérifiez son nom, son port et la sécurité choisie (STARTTLS ou TLS).", Err: err}
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return SMTPError{Advice: "Le serveur d'envoi refuse la connexion.", Err: err}
	}
	if v.Security == SecuritySTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return SMTPError{Advice: "Ce serveur ne propose pas STARTTLS sur ce port : essayez TLS avec le port 465.", Err: errors.New("STARTTLS absent")}
		}
		if err := client.StartTLS(config); err != nil {
			return SMTPError{Advice: "La connexion chiffrée au serveur d'envoi a échoué : vérifiez son nom et la sécurité choisie.", Err: err}
		}
	}
	if v.Username != "" {
		ok, mechanisms := client.Extension("AUTH")
		if !ok {
			return SMTPError{Advice: "Ce serveur ne demande pas d'identifiant : laissez l'identifiant et le mot de passe vides, ou vérifiez le nom du serveur.", Err: errors.New("AUTH absent")}
		}
		var method smtp.Auth = smtp.PlainAuth("", v.Username, v.Password, v.Host)
		if !strings.Contains(" "+strings.ToUpper(mechanisms)+" ", " PLAIN ") && strings.Contains(" "+strings.ToUpper(mechanisms)+" ", " LOGIN ") {
			method = loginAuth{username: v.Username, password: v.Password}
		}
		if err := client.Auth(method); err != nil {
			return SMTPError{Advice: "Le serveur d'envoi refuse l'identifiant ou le mot de passe. Pour Gmail, il faut un mot de passe d'application (voir l'aide).", Err: errors.New(safeReply(err))}
		}
	}
	if err := client.Mail(v.From); err != nil {
		return SMTPError{Advice: "Le serveur d'envoi refuse l'adresse d'expédition : elle doit souvent être celle du compte e-mail.", Err: err}
	}
	if err := client.Rcpt(to); err != nil {
		return SMTPError{Advice: "Le serveur d'envoi refuse l'adresse du destinataire.", Err: err}
	}
	writer, err := client.Data()
	if err != nil {
		return SMTPError{Advice: "Le serveur d'envoi refuse le message.", Err: err}
	}
	if _, err := writer.Write(message); err != nil {
		writer.Close()
		return SMTPError{Advice: "L'envoi du message a été coupé.", Err: err}
	}
	if err := writer.Close(); err != nil {
		return SMTPError{Advice: "Le serveur d'envoi refuse le message.", Err: err}
	}
	client.Quit()
	return nil
}

func connectError(err error, v SMTPSettings) error {
	var certificate *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	switch {
	case errors.As(err, &certificate):
		return SMTPError{Advice: "Le certificat du serveur d'envoi n'est pas valable pour ce nom : vérifiez le nom du serveur.", Err: err}
	case errors.As(err, &record):
		return SMTPError{Advice: "Ce port n'attend pas une connexion TLS : essayez STARTTLS (port 587).", Err: err}
	}
	return SMTPError{Advice: fmt.Sprintf("Le serveur d'envoi %s ne répond pas sur le port %d : vérifiez son nom, son port, et que le Pi est relié à internet.", v.Host, v.Port), Err: err}
}

// safeReply : la réponse du serveur à l'identifiant, sans rien de ce qui a été envoyé.
func safeReply(err error) string {
	var reply *textproto.Error
	if errors.As(err, &reply) {
		return fmt.Sprintf("%d %s", reply.Code, reply.Msg)
	}
	return "identifiant refusé"
}

// loginAuth : méthode AUTH LOGIN, pour les serveurs qui ne connaissent pas
// PLAIN ; seulement sur une connexion chiffrée.
type loginAuth struct {
	username, password string
}

func (a loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS {
		return "", nil, errors.New("connexion non chiffrée")
	}
	return "LOGIN", nil, nil
}

func (a loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(fromServer))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	}
	return nil, errors.New("question inattendue du serveur")
}

var (
	_ auth.Mailer     = (*SMTP)(nil)
	_ auth.CodeMailer = (*SMTP)(nil)
)

// Invite envoie le lien d'invitation d'un nouveau compte.
func (s *SMTP) Invite(account auth.Public, link, lang string) error {
	return s.sendLink(KindInvite, account, link, lang)
}

// PasswordReset envoie le lien « mot de passe oublié ».
func (s *SMTP) PasswordReset(account auth.Public, link, lang string) error {
	return s.sendLink(KindReset, account, link, lang)
}

func (s *SMTP) sendLink(kind Kind, account auth.Public, link, lang string) error {
	request, err := Request{Kind: kind, To: account.Email, Name: account.Name, Link: link, Lang: lang}.Check()
	if err != nil {
		return err
	}
	return s.Send(Message{Kind: kind, To: request.To, Name: request.Name, Link: request.Link, Lang: request.Lang})
}

// SendCode envoie un code à 6 chiffres : connexion (double authentification)
// ou vérification de l'adresse.
func (s *SMTP) SendCode(account auth.Public, purpose auth.CodePurpose, code, lang string) error {
	kind := KindLoginCode
	if purpose == auth.CodeVerify {
		kind = KindVerifyCode
	}
	return s.Send(Message{Kind: kind, To: account.Email, Name: account.Name, Code: code, Lang: lang})
}

// SendAlert envoie une alerte (spécification 8.6) à un administrateur ;
// subject et body viennent du paquet alerts.
func (s *SMTP) SendAlert(to, name, lang, subject, body string) error {
	return s.Send(Message{Kind: KindAlert, To: to, Name: name, Lang: lang, Subject: subject, Body: body})
}
