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

// Commande pi-appliance-relay : relais du projet (spécification 7.3, 10.2),
// sur le Pi du projet, devant son reverse proxy : il tient les ports 80 et
// 443, fait passer les visiteurs des appliances relayées par leur tunnel et
// renvoie les autres noms (adresses, stats…) au reverse proxy.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/gmoulard/pi-appliance/internal/tunnel"
	"github.com/gmoulard/pi-appliance/internal/tunnel/server"
)

func main() {
	https := flag.String("https", ":443", "adresse d'écoute HTTPS (visiteurs et appliances)")
	plain := flag.String("http", ":80", "adresse d'écoute HTTP (visiteurs)")
	zone := flag.String("zone", "pi-appliance.com", "zone des adresses des appliances")
	name := flag.String("name", "", "nom du relais (vide : relais.<zone>)")
	certFile := flag.String("cert", "", "certificat du nom du relais (PEM), relu quand il change")
	keyFile := flag.String("key", "", "clé du certificat (PEM, droits 600)")
	registry := flag.String("registry", "/var/lib/pi-appliance-addresses/registry.json", "registre du service d'adresses, lu seulement")
	data := flag.String("data", "/var/lib/pi-appliance-relay", "dossier des places du relais")
	maxAppliances := flag.Int("max", 20, "nombre maximal d'appliances relayées, liste d'attente au-delà")
	monthly := flag.Int64("monthly-bytes", tunnel.MonthlyBytes, "octets par mois et par appliance, les deux sens")
	rate := flag.Int64("rate", tunnel.RateBits, "débit par appliance et par sens, en bits par seconde")
	pool := flag.Int("pool", 8, "connexions en attente gardées par appliance")
	visits := flag.Int("visits", 64, "visites en cours par appliance")
	fallbackHTTPS := flag.String("fallback-https", "127.0.0.1:8443", "reverse proxy du Pi du projet, HTTPS (vide : refusé)")
	fallbackHTTP := flag.String("fallback-http", "127.0.0.1:8080", "reverse proxy du Pi du projet, HTTP (vide : refusé)")
	flag.Parse()
	if *name == "" {
		*name = "relais." + *zone
	}
	if *certFile == "" || *keyFile == "" {
		log.Fatal("certificat du relais manquant (options -cert et -key)")
	}
	certificate := &reloader{cert: *certFile, key: *keyFile}
	if _, err := certificate.get(nil); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatal(err)
	}
	places, err := server.OpenPlaces(filepath.Join(*data, "places.json"), *maxAppliances)
	if err != nil {
		log.Fatal(err)
	}
	places.MonthlyBytes = *monthly
	relay := &server.Server{
		Zone: *zone, RelayName: *name,
		TLS:    &tls.Config{GetCertificate: certificate.get, MinVersion: tls.VersionTLS12},
		Names:  &server.Registry{Path: *registry},
		Places: places, RateBits: *rate, PoolLimit: *pool, MaxVisits: *visits,
		FallbackHTTPS: *fallbackHTTPS, FallbackHTTP: *fallbackHTTP,
		Logf: log.Printf,
	}
	secure, err := net.Listen("tcp", *https)
	if err != nil {
		log.Fatal(err)
	}
	web, err := net.Listen("tcp", *plain)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		relay.Run(ctx)
		close(done)
	}()
	go func() {
		<-ctx.Done()
		secure.Close()
		web.Close()
	}()
	log.Printf("relais %s sur %s et %s, %d places", *name, *https, *plain, *maxAppliances)
	go relay.ServeHTTP(web)
	relay.ServeHTTPS(secure)
	<-done
}

// reloader : certificat du relais relu quand ses fichiers changent (renouvelé
// à côté), regardé une fois par minute au plus.
type reloader struct {
	cert, key string

	mu      sync.Mutex
	current *tls.Certificate
	stamp   time.Time
	checked time.Time
}

func (r *reloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current != nil && time.Since(r.checked) < time.Minute {
		return r.current, nil
	}
	r.checked = time.Now()
	info, err := os.Stat(r.cert)
	if err == nil && r.current != nil && info.ModTime().Equal(r.stamp) {
		return r.current, nil
	}
	loaded, err := tls.LoadX509KeyPair(r.cert, r.key)
	if err != nil {
		if r.current != nil {
			log.Printf("certificat du relais illisible, l'ancien est gardé : %v", err)
			return r.current, nil
		}
		return nil, err
	}
	r.current = &loaded
	if info != nil {
		r.stamp = info.ModTime()
	}
	return r.current, nil
}
