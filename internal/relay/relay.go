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

// Package relay : relais d'e-mails des services du Pi (spécification 10.2,
// TODO-2.74). Un petit serveur SMTP, sans mot de passe ni chiffrement,
// écoute sur l'adresse du Pi vue des conteneurs (pont Docker, extra_hosts
// « pi-appliance-mail:host-gateway ») ; il reconnaît le service expéditeur
// par l'adresse de son conteneur sur le réseau pi-appliance, refuse ceux dont
// l'administrateur n'a pas ouvert le relais, et renvoie chaque message, en
// texte seul, par le SMTP réglé dans la console (mail.SMTP.SendService).
//
// NE PAS écouter sur toutes les adresses du Pi : le relais serait joignable
// depuis la maison. NE PAS renvoyer les pièces jointes ni le HTML : seul le
// texte part, sous l'adresse de l'administrateur.
package relay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"time"
)

const (
	// MaxMessage : taille d'un message reçu, en octets.
	MaxMessage = 1 << 20
	// MaxRecipients : destinataires par message.
	MaxRecipients = 20
	maxBody       = 20000
	timeout       = 2 * time.Minute
)

// Services : qui envoie (adresse du conteneur → service) et s'il en a le droit.
type Services interface {
	ServiceByIP(ctx context.Context, ip string) (string, error)
	MailAllowed(name string) (string, bool)
}

// Sender : envoi par le SMTP de la console.
type Sender interface {
	SendService(service, to, subject, body string) error
}

// Server : le relais.
type Server struct {
	Services Services
	Sender   Sender
	Logf     func(format string, args ...any)
}

func (s *Server) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Serve accepte les connexions jusqu'à la fermeture de ln.
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go s.handle(conn)
	}
}

// Run écoute sur addr (réessayé tant que l'adresse n'existe pas, par exemple
// avant le démarrage de Docker) jusqu'à la fin de ctx.
func (s *Server) Run(ctx context.Context, addr func(context.Context) (string, error)) {
	for {
		address, err := addr(ctx)
		var ln net.Listener
		if err == nil {
			ln, err = net.Listen("tcp", address)
		}
		if err == nil {
			s.logf("relais d'e-mails des services : à l'écoute sur %s", address)
			go func() {
				<-ctx.Done()
				ln.Close()
			}()
			if err := s.Serve(ln); err != nil {
				s.logf("relais d'e-mails : %v", err)
			}
			if ctx.Err() != nil {
				return
			}
		} else {
			s.logf("relais d'e-mails : écoute impossible (%v), nouvel essai dans une minute", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

type session struct {
	conn    net.Conn
	r       *bufio.Reader
	service string
	from    string
	to      []string
}

func (s *session) reply(code int, text string) {
	fmt.Fprintf(s.conn, "%d %s\r\n", code, text)
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))
	ses := &session{conn: conn, r: bufio.NewReaderSize(conn, 4096)}
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	name, err := s.Services.ServiceByIP(ctx, host)
	cancel()
	if err != nil {
		s.logf("relais d'e-mails : expéditeur %s : %v", host, err)
		ses.reply(421, "service temporairement indisponible")
		return
	}
	display, ok := s.Services.MailAllowed(name)
	if name == "" || !ok {
		s.logf("relais d'e-mails : refusé à %s (service « %s ») : relais non ouvert dans la console", host, name)
		ses.reply(554, "relais refuse : ouvrez l'envoi d'e-mails de ce service dans la console pi-appliance")
		return
	}
	ses.service = display
	ses.reply(220, "pi-appliance relais d'e-mails")
	for {
		line, err := readLine(ses.r)
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "HELO", "EHLO":
			ses.reply(250, "pi-appliance")
		case "MAIL":
			address, ok := pathArg(arg, "FROM:")
			if !ok {
				ses.reply(501, "MAIL FROM:<adresse> attendu")
				continue
			}
			ses.from, ses.to = address, nil
			ses.reply(250, "ok")
		case "RCPT":
			address, ok := pathArg(arg, "TO:")
			if _, err := mail.ParseAddress(address); !ok || err != nil {
				ses.reply(501, "RCPT TO:<adresse> attendu")
				continue
			}
			if len(ses.to) >= MaxRecipients {
				ses.reply(452, "trop de destinataires")
				continue
			}
			ses.to = append(ses.to, address)
			ses.reply(250, "ok")
		case "DATA":
			if len(ses.to) == 0 {
				ses.reply(503, "RCPT TO d'abord")
				continue
			}
			ses.reply(354, "terminez par une ligne avec un point")
			data, err := readData(ses.r)
			if err != nil {
				if errors.Is(err, errTooBig) {
					ses.reply(552, "message trop gros")
					continue
				}
				return
			}
			code, text := s.deliver(ses, data)
			ses.reply(code, text)
			ses.from, ses.to = "", nil
		case "RSET":
			ses.from, ses.to = "", nil
			ses.reply(250, "ok")
		case "NOOP":
			ses.reply(250, "ok")
		case "QUIT":
			ses.reply(221, "au revoir")
			return
		default:
			ses.reply(502, "commande inconnue")
		}
	}
}

// deliver renvoie le message à chaque destinataire.
func (s *Server) deliver(ses *session, data []byte) (int, string) {
	subject, body, err := parseMessage(data)
	if err != nil {
		return 554, "message illisible : " + err.Error()
	}
	for _, to := range ses.to {
		if err := s.Sender.SendService(ses.service, to, subject, body); err != nil {
			s.logf("relais d'e-mails : %s vers %s : %v", ses.service, to, err)
			return 451, "envoi impossible pour le moment : " + oneLine(err.Error())
		}
	}
	s.logf("relais d'e-mails : %s, %d destinataire(s)", ses.service, len(ses.to))
	return 250, "envoyé"
}

func oneLine(text string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
}

var errTooBig = errors.New("message trop gros")

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > 1000 {
		return "", errors.New("ligne trop longue")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readData lit jusqu'à « . » seul, points doublés retirés (RFC 5321, 4.5.2).
func readData(r *bufio.Reader) ([]byte, error) {
	var b bytes.Buffer
	tooBig := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "." {
			break
		}
		trimmed = strings.TrimPrefix(trimmed, ".")
		if b.Len()+len(trimmed)+2 > MaxMessage {
			tooBig = true
			continue
		}
		b.WriteString(trimmed)
		b.WriteString("\r\n")
	}
	if tooBig {
		return nil, errTooBig
	}
	return b.Bytes(), nil
}

// pathArg : « FROM:<adresse> » (paramètres ESMTP ignorés).
func pathArg(arg, prefix string) (string, bool) {
	arg = strings.TrimSpace(arg)
	if len(arg) < len(prefix) || !strings.EqualFold(arg[:len(prefix)], prefix) {
		return "", false
	}
	rest := strings.TrimSpace(arg[len(prefix):])
	if !strings.HasPrefix(rest, "<") {
		address, _, _ := strings.Cut(rest, " ")
		return address, true
	}
	end := strings.Index(rest, ">")
	if end < 0 {
		return "", false
	}
	return rest[1:end], true
}

// parseMessage : sujet et texte d'un message (text/plain, ou sa partie
// text/plain s'il est en plusieurs parties).
func parseMessage(data []byte) (string, string, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return "", "", err
	}
	decoder := new(mime.WordDecoder)
	subject, err := decoder.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		subject = msg.Header.Get("Subject")
	}
	subject = strings.TrimSpace(oneLine(subject))
	if subject == "" {
		subject = "(sans sujet)"
	}
	if len(subject) > 200 {
		subject = subject[:200]
	}
	body, err := textPart(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body, 0)
	if err != nil {
		return "", "", err
	}
	body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(body) == "" {
		return "", "", errors.New("aucun texte")
	}
	if len(body) > maxBody {
		body = body[:maxBody] + "\n[…]\n"
	}
	return subject, body, nil
}

func textPart(contentType, encoding string, body io.Reader, depth int) (string, error) {
	if contentType == "" {
		contentType = "text/plain"
	}
	media, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(media, "multipart/") && depth < 3 {
		reader := multipart.NewReader(body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				return "", errors.New("aucune partie en texte")
			}
			if text, err := textPart(part.Header.Get("Content-Type"), part.Header.Get("Content-Transfer-Encoding"), part, depth+1); err == nil {
				return text, nil
			}
		}
	}
	if media != "text/plain" {
		return "", errors.New("seul le texte est accepté")
	}
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		body = quotedprintable.NewReader(body)
	case "base64":
		body = base64.NewDecoder(base64.StdEncoding, newlineStripper{body})
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxMessage))
	if err != nil {
		return "", err
	}
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return "", fmt.Errorf("jeu de caractères %s : seul utf-8 est accepté", charset)
	}
	return string(data), nil
}

// newlineStripper retire les retours à la ligne d'un texte en base64.
type newlineStripper struct{ r io.Reader }

func (n newlineStripper) Read(p []byte) (int, error) {
	count, err := n.r.Read(p)
	out := p[:0]
	for _, c := range p[:count] {
		if c != '\r' && c != '\n' {
			out = append(out, c)
		}
	}
	return len(out), err
}
