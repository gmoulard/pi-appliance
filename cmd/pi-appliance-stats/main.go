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

// pi-appliance-stats : service du projet qui reçoit les statistiques anonymes
// des appliances (spécification 10.2), sur le Pi du projet, derrière son
// reverse proxy (stats.pi-appliance.com, journal des accès coupé).
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

	"github.com/gmoulard/pi-appliance/internal/stats/reception"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "adresse d'écoute de la réception des envois")
	adminAddr := flag.String("admin-addr", "127.0.0.1:8091", "adresse d'écoute du résumé par jour, à garder sur le Pi du projet (vide : pas de résumé)")
	data := flag.String("data", "/var/lib/pi-appliance-stats", "dossier des envois reçus, un fichier par jour")
	flag.Parse()
	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatal(err)
	}
	store := &reception.Store{Dir: *data, Logf: log.Printf}
	// NE PAS journaliser les erreurs de net/http : elles donnent l'adresse du client.
	quiet := log.New(io.Discard, "", 0)
	servers := []*http.Server{{Addr: *addr, Handler: reception.Handler(store, false), ErrorLog: quiet, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}}
	if *adminAddr != "" {
		servers = append(servers, &http.Server{Addr: *adminAddr, Handler: reception.Handler(store, true), ErrorLog: quiet, ReadHeaderTimeout: 10 * time.Second})
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	failed := make(chan error, len(servers))
	for _, server := range servers {
		go func() {
			if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				failed <- err
			}
		}()
	}
	log.Printf("statistiques anonymes sur %s, données dans %s", *addr, *data)
	var failure error
	select {
	case <-ctx.Done():
	case failure = <-failed:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, server := range servers {
		server.Shutdown(shutdown)
	}
	if failure != nil {
		log.Fatal(failure)
	}
}
