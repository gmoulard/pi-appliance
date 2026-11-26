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

// pi-appliance-addresses : service d'adresses du projet (spécification 7.1,
// 10.2), sur le Pi du projet, derrière son reverse proxy
// (adresses.pi-appliance.com, journal des accès coupé). Il réserve les noms
// <nom>.pi-appliance.com et écrit chez OVH l'adresse IP de chaque appliance.
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
	zone := flag.String("zone", "pi-appliance.com", "zone DNS chez OVH")
	credentialsFile := flag.String("ovh-credentials", "/etc/pi-appliance-addresses/ovh", "clés de l'API OVH (lignes OVH_Application_key=, OVH_Application_secret=, OVH_Consumer_Key=), droits 600, jamais dans le dépôt")
	endpoint := flag.String("ovh-endpoint", service.DefaultOVHEndpoint, "API OVH")
	behindProxy := flag.Bool("behind-proxy", true, "lire l'adresse du client dans X-Real-IP, posé par le reverse proxy")
	flag.Parse()
	credentials, err := service.LoadCredentials(*credentialsFile)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatal(err)
	}
	addresses, err := service.Open(*data+"/registry.json", &service.OVH{Zone: *zone, Credentials: credentials, Endpoint: *endpoint})
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
