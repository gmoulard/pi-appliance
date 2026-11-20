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
	"strings"
	"syscall"
	"time"

	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/console"
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/metrics"
	"github.com/gmoulard/pi-appliance/internal/network"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/storage"
	"github.com/gmoulard/pi-appliance/internal/system"
	"github.com/gmoulard/pi-appliance/marketplace"
)

var version = "dev"

// defaultDomain : <nom du Pi>.local (name.sh donne à chaque Pi son nom).
func defaultDomain() string {
	if host, err := os.Hostname(); err == nil && host != "" && host != "localhost" {
		return strings.ToLower(host) + ".local"
	}
	return "pi-appliance.local"
}

func main() {
	addr := flag.String("addr", ":8080", "adresse d'écoute de la console")
	data := flag.String("data", "/var/lib/pi-appliance", "dossier des services installés")
	domain := flag.String("domain", defaultDomain(), "adresse de l'appliance, suffixe de l'adresse de chaque service")
	resetFile := flag.String("reset-file", "/boot/firmware/reset", "fichier déposé sur la carte SD pour réinitialiser le mot de passe administrateur (vide : désactivé)")
	networkScript := flag.String("network-script", network.DefaultScript, "script du réseau du Pi (vide : réseau non réglable depuis la console)")
	storageScript := flag.String("storage-script", storage.DefaultScript, "script du disque USB des données (vide : pas de disque USB)")
	backupScript := flag.String("backup-script", backup.DefaultScript, "script du disque USB des sauvegardes (vide : disque non réglable depuis la console)")
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
	manager := &services.Manager{Dir: *data, Domain: *domain, Runner: services.Docker{}, Measure: services.Measure}
	var net *network.Client
	if *networkScript != "" {
		net = &network.Client{Runner: network.Script(*networkScript), JoinDelay: 3 * time.Second, Logf: log.Printf}
	}
	var disks *storage.Client
	if *storageScript != "" {
		disks = &storage.Client{Runner: network.Script(*storageScript)}
	}
	resources := func() system.Resources {
		r := system.Read(*data)
		r.USBDisk = system.MountedDisk(storage.DataMount)
		return r
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
	// Sauvegardes chaque nuit sur le disque USB de sauvegarde (spécification 9.1).
	host := backup.Hostname()
	backups := &backup.Manager{Dir: *data, Services: manager, Target: backup.DiskTarget(backup.DiskMount, host), Host: host, Logf: log.Printf}
	if *backupScript != "" {
		backups.Disks = &backup.Disks{Runner: network.Script(*backupScript)}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	measured := make(chan struct{})
	go func() {
		history.Run(ctx, time.Minute, func() metrics.Sample { return metrics.Measure(resources()) }, log.Printf)
		close(measured)
	}()
	go duckdns.Run(ctx)
	go backups.Run(ctx)
	server := &http.Server{
		Addr: *addr,
		Handler: console.Handler(console.Config{
			Version:       version,
			Catalog:       catalog,
			Services:      manager,
			Accounts:      accounts,
			Network:       net,
			Storage:       disks,
			Resources:     resources,
			History:       history,
			DuckDNS:       duckdns,
			Backups:       backups,
			WizardFile:    filepath.Join(*data, "wizard-done"),
			ACMEEmailFile: filepath.Join(*data, "system", "traefik", "acme-email"),
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
