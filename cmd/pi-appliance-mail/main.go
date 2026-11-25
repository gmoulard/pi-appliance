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

// pi-appliance-mail : service d'envoi d'e-mails du projet (spécification 8.5,
// 10.2), sur le Pi du projet, derrière son reverse proxy
// (envoi.pi-appliance.com, journal des accès coupé). Il envoie les liens
// « mot de passe oublié » et d'invitation demandés par les appliances, par le
// relais SMTP du domaine pi-appliance.com (Amazon SES).
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gmoulard/pi-appliance/internal/mail/service"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8093", "adresse d'écoute")
	relay := flag.String("smtp", "email-smtp.eu-west-3.amazonaws.com:587", "relais SMTP (hôte:port, STARTTLS)")
	credentialsFile := flag.String("smtp-credentials", "/etc/pi-appliance-mail/smtp", "identifiants du relais, lignes username= et password=, droits 600 (jamais dans le dépôt)")
	from := flag.String("from", "pi-appliance <ne-pas-repondre@pi-appliance.com>", "expéditeur des messages")
	behindProxy := flag.Bool("behind-proxy", true, "lire l'adresse du client dans X-Real-IP, posé par le reverse proxy")
	flag.Parse()
	sender, err := service.LoadSMTP(*credentialsFile, *relay, *from)
	if err != nil {
		log.Fatal(err)
	}
	mailer := &service.Service{Sender: sender, From: *from, Logf: log.Printf, BehindProxy: *behindProxy}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// NE PAS journaliser les erreurs de net/http : elles donnent l'adresse du client.
	server := &http.Server{Addr: *addr, Handler: mailer.Handler(), ErrorLog: log.New(io.Discard, "", 0), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("service d'envoi d'e-mails sur %s, relais %s", *addr, *relay)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
