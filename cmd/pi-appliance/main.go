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

package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/console"
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/metrics"
	"github.com/gmoulard/pi-appliance/internal/network"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/system"
	"github.com/gmoulard/pi-appliance/marketplace"
)

var version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "adresse d'écoute de la console")
	data := flag.String("data", "/var/lib/pi-appliance", "dossier des services installés")
	domain := flag.String("domain", "pi-appliance.local", "adresse de l'appliance, suffixe de l'adresse de chaque service")
	resetFile := flag.String("reset-file", "/boot/firmware/reset", "fichier déposé sur la carte SD pour réinitialiser le mot de passe administrateur (vide : désactivé)")
	networkScript := flag.String("network-script", network.DefaultScript, "script du réseau du Pi (vide : réseau non réglable depuis la console)")
	flag.Parse()
	catalog, err := marketplace.Builtin()
	if err != nil {
		log.Fatal(err)
	}
	accounts, err := auth.Open(filepath.Join(*data, "accounts.json"))
	if err != nil {
		log.Fatal(err)
	}
	if found, err := accounts.ConsumeResetFile(*resetFile); err != nil {
		log.Printf("fichier reset : %v", err)
	} else if found {
		log.Printf("fichier reset trouvé : réinitialisation du mot de passe administrateur ouverte")
	}
	manager := &services.Manager{Dir: *data, Domain: *domain, Runner: services.Docker{}, MemoryMB: services.SystemMemoryMB}
	var net *network.Client
	if *networkScript != "" {
		net = &network.Client{Runner: network.Script(*networkScript), JoinDelay: 3 * time.Second, Logf: log.Printf}
	}
	history := &metrics.History{Path: filepath.Join(*data, "history.json")}
	if err := history.Load(); err != nil {
		log.Printf("historique illisible, il repart de zéro : %v", err)
	}
	// Réglages DuckDNS (identifiant et jeton) : fichier en droits 600, jamais journalisés.
	duckdns, err := ddns.Open(filepath.Join(*data, "duckdns.json"))
	if err != nil {
		log.Fatal(err)
	}
	duckdns.Logf = log.Printf
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	measured := make(chan struct{})
	go func() {
		history.Run(ctx, time.Minute, func() metrics.Sample { return metrics.Measure(*data) }, log.Printf)
		close(measured)
	}()
	go duckdns.Run(ctx)
	server := &http.Server{
		Addr: *addr,
		Handler: console.Handler(console.Config{
			Version:    version,
			Catalog:    catalog,
			Services:   manager,
			Accounts:   accounts,
			Network:    net,
			Resources:  func() system.Resources { return system.Read(*data) },
			History:    history,
			DuckDNS:    duckdns,
			WizardFile: filepath.Join(*data, "wizard-done"),
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("console pi-appliance %s sur %s%s", version, *addr, console.BasePath)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	// L'historique est écrit avant de quitter, heure en cours comprise.
	<-measured
}
