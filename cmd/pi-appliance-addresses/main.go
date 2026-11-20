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

// pi-appliance-addresses : service d'adresses du projet (spécification 7.1,
// 10.2), sur le Pi du projet, derrière son reverse proxy
// (adresses.pi-appliance.com, journal des accès coupé). Il réserve les noms
// <nom>.pi-appliance.com et écrit leurs alias dans AWS Route 53.
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

	"github.com/gmoulard/pi-appliance/internal/addresses/service"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8092", "adresse d'écoute")
	data := flag.String("data", "/var/lib/pi-appliance-addresses", "dossier du registre des adresses")
	zone := flag.String("zone-id", "Z01037363SBTB2LIT0ZB4", "zone hébergée pi-appliance.com dans Route 53")
	credentialsFile := flag.String("aws-credentials", "/etc/pi-appliance-addresses/aws-credentials", "clés AWS au format de la CLI AWS, droits 600 (jamais dans le dépôt)")
	profile := flag.String("aws-profile", "default", "profil du fichier des clés AWS")
	behindProxy := flag.Bool("behind-proxy", true, "lire l'adresse du client dans X-Real-IP, posé par le reverse proxy")
	flag.Parse()
	credentials, err := service.LoadCredentials(*credentialsFile, *profile)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatal(err)
	}
	addresses, err := service.Open(*data+"/registry.json", &service.Route53{ZoneID: *zone, Credentials: credentials})
	if err != nil {
		log.Fatal(err)
	}
	addresses.Logf = log.Printf
	addresses.BehindProxy = *behindProxy
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go addresses.Run(ctx)
	// NE PAS journaliser les erreurs de net/http : elles donnent l'adresse du client.
	server := &http.Server{Addr: *addr, Handler: addresses.Handler(), ErrorLog: log.New(io.Discard, "", 0), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("service d'adresses sur %s, registre dans %s", *addr, *data)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
