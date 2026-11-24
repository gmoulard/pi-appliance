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
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/gmoulard/pi-appliance/internal/addresses"
	"github.com/gmoulard/pi-appliance/internal/auth"
	"github.com/gmoulard/pi-appliance/internal/backup"
	"github.com/gmoulard/pi-appliance/internal/catalog"
	"github.com/gmoulard/pi-appliance/internal/cli"
	"github.com/gmoulard/pi-appliance/internal/cockpit"
	"github.com/gmoulard/pi-appliance/internal/console"
	"github.com/gmoulard/pi-appliance/internal/ddns"
	"github.com/gmoulard/pi-appliance/internal/diagnostic"
	"github.com/gmoulard/pi-appliance/internal/mail"
	"github.com/gmoulard/pi-appliance/internal/metrics"
	"github.com/gmoulard/pi-appliance/internal/network"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/internal/stats"
	"github.com/gmoulard/pi-appliance/internal/storage"
	"github.com/gmoulard/pi-appliance/internal/system"
	"github.com/gmoulard/pi-appliance/internal/updates"
	"github.com/gmoulard/pi-appliance/marketplace"
)

// Version, commit complet et date du commit (AAAA-MM-JJ), donnés à la
// construction : go build -ldflags "-X main.version=… -X main.commit=… -X main.date=…"
// (workflows « Image », « Go » et « Système ») ; sans eux, commit et date
// viennent des informations de git gravées par go build (buildCommit).
var (
	version = "dev"
	commit  = ""
	date    = ""
)

// buildCommit : commit et date du binaire, d'après -ldflags, sinon d'après
// vcs.revision et vcs.time ; vides si inconnus (go run, go test).
func buildCommit() (string, string) {
	revision, when := commit, date
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if revision == "" {
					revision = setting.Value
				}
			case "vcs.time":
				if when == "" && len(setting.Value) >= 10 {
					when = setting.Value[:10]
				}
			}
		}
	}
	return revision, when
}

// defaultDomain : <nom du Pi>.local (name.sh donne à chaque Pi son nom).
func defaultDomain() string {
	if host, err := os.Hostname(); err == nil && host != "" && host != "localhost" {
		return strings.ToLower(host) + ".local"
	}
	return "pi-appliance.local"
}

func main() {
	// pia ou pi-appliance <commande> : ligne de commande du Pi (internal/cli) ; sans
	// commande, ou avec des options seulement, le programme est la console.
	// « pia » (lien posé par install.sh) est toujours la ligne de commande.
	if filepath.Base(os.Args[0]) == cli.ShortName || (len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-")) {
		os.Exit(cli.Run(os.Args[1:], cli.Defaults(version)))
	}
	addr := flag.String("addr", ":8080", "adresse d'écoute de la console")
	data := flag.String("data", "/var/lib/pi-appliance", "dossier des services installés")
	domain := flag.String("domain", defaultDomain(), "adresse de l'appliance, suffixe de l'adresse de chaque service")
	resetFile := flag.String("reset-file", "/boot/firmware/reset", "fichier déposé sur la carte SD pour réinitialiser le mot de passe administrateur (vide : désactivé)")
	networkScript := flag.String("network-script", network.DefaultScript, "script du réseau du Pi (vide : réseau non réglable depuis la console)")
	statsURL := flag.String("stats-url", stats.DefaultURL, "service de réception des statistiques anonymes, si l'administrateur les accepte")
	storageScript := flag.String("storage-script", storage.DefaultScript, "script du disque USB des données (vide : pas de disque USB)")
	backupScript := flag.String("backup-script", backup.DefaultScript, "script du disque USB des sauvegardes (vide : disque non réglable depuis la console)")
	updatesScript := flag.String("updates-script", updates.DefaultScript, "script des correctifs du système (vide : système non mis à jour depuis la console)")
	diagnosticScript := flag.String("diagnostic-script", diagnostic.DefaultScript, "script du diagnostic du Pi (vide : pas de diagnostic depuis la console)")
	diagnosticFile := flag.String("diagnostic-file", "/boot/firmware/diagnostic", "fichier déposé sur la carte SD pour ouvrir le diagnostic sans compte pendant 24 heures (vide : désactivé)")
	rescueAddr := flag.String("rescue-addr", ":8443", "adresse de la console de secours, ouverte seulement quand Traefik ne répond plus (vide : pas de secours)")
	tlsDir := flag.String("tls-dir", "/var/lib/pi-appliance/system/tls", "dossier du certificat local du Pi (local.crt, local.key), pour la console de secours")
	cockpitScript := flag.String("cockpit-script", cockpit.DefaultScript, "script de l'administration du système par Cockpit (vide : non installable depuis la console)")
	releaseRepo := flag.String("release-repo", updates.DefaultRepo, "dépôt GitHub des versions publiées de pi-appliance (vide : pas de comparaison)")
	catalogURL := flag.String("catalog-url", catalog.DefaultURL, "index du catalogue de la marketplace publié en ligne, en HTTPS (vide : catalogue intégré seulement)")
	cliTokenFile := flag.String("cli-token-file", cli.DefaultTokenFile, "jeton de la ligne de commande du Pi, lisible par root seul (vide : pas de ligne de commande)")
	traefikServices := flag.String("traefik-services", "/etc/pi-appliance/traefik/dynamic/services-internet.yml", "configuration dynamique de Traefik des services ouverts sur internet, sous un chemin de l'adresse internet du Pi (vide : aucun)")
	traefikInternet := flag.String("traefik-internet", "/etc/pi-appliance/traefik/dynamic/console-internet.yml", "configuration dynamique de Traefik écrite quand la console est ouverte sur internet (vide : jamais ouverte)")
	addressesURL := flag.String("addresses-url", addresses.DefaultServiceURL, "service d'adresses du projet, pour <nom>.pi-appliance.com (vide : pas de réservation)")
	mailURL := flag.String("mail-url", mail.DefaultServiceURL, "service d'envoi d'e-mails du projet, pour « mot de passe oublié » et les invitations (vide : pas d'e-mail)")
	showVersion := flag.Bool("version", false, "affiche la version et s'arrête")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	builtin, err := marketplace.Builtin()
	if err != nil {
		log.Fatal(err)
	}
	// Catalogue lu en ligne et gardé sur le Pi ; l'intégré tant qu'il n'est pas lu (spécification 6.1).
	market := &catalog.Source{Builtin: builtin, URL: *catalogURL, CacheFile: filepath.Join(*data, "catalog.json"), Version: version, Logf: log.Printf}
	accounts, err := auth.Open(filepath.Join(*data, "accounts.json"))
	if err != nil {
		log.Fatal(err)
	}
	if found, err := accounts.ConsumeResetFile(*resetFile); err != nil {
		log.Printf("fichier reset : %v", err)
	} else if found {
		log.Printf("fichier reset trouvé : réinitialisation du mot de passe administrateur ouverte")
	}
	window := &diagnostic.Window{File: *diagnosticFile, State: filepath.Join(*data, "diagnostic-until")}
	if found, err := window.Consume(); err != nil {
		log.Printf("fichier diagnostic : %v", err)
	} else if found {
		log.Printf("fichier diagnostic trouvé : diagnostic ouvert sans compte pendant 24 heures")
	}
	var diag *diagnostic.Client
	if *diagnosticScript != "" {
		diag = &diagnostic.Client{Runner: network.Script(*diagnosticScript)}
	}
	manager := &services.Manager{Dir: *data, Domain: *domain, Runner: services.Docker{}, Images: services.DockerAPI{}, Measure: services.Measure, FollowName: true}
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "domain" {
			manager.FollowName = false
		}
	})
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
	// Adresse <nom>.pi-appliance.com : clé propre au Pi, tirée au premier démarrage (spécification 7.1).
	// La même clé signe les demandes au service d'envoi d'e-mails (spécification 10.2).
	var key *addresses.Key
	if *addressesURL != "" || *mailURL != "" {
		key, err = addresses.LoadOrCreateKey(filepath.Join(*data, "address-key"))
		if err != nil {
			log.Fatal(err)
		}
	}
	var mailer auth.Mailer
	if *mailURL != "" {
		mailer = &mail.Client{URL: *mailURL, Key: key}
	}
	// Envoi des e-mails par le serveur SMTP réglé dans la console (spécification
	// 10.2) : préféré au service du projet dès qu'il est réglé.
	smtpSender, err := mail.OpenSMTP(filepath.Join(*data, "smtp.json"))
	if err != nil {
		log.Fatal(err)
	}
	var address *addresses.Client
	if *addressesURL != "" {
		address, err = addresses.OpenClient(filepath.Join(*data, "address.json"), key)
		if err != nil {
			log.Fatal(err)
		}
		address.URL = *addressesURL
		address.Logf = log.Printf
		address.Target = func() string { return duckdns.Status().Hostname }
	}
	// Sauvegardes chaque nuit sur le disque USB de sauvegarde (spécification 9.1).
	host := backup.Hostname()
	backups := &backup.Manager{Dir: *data, Services: manager, Target: backup.DiskTarget(backup.DiskMount, host), Hosted: backup.HostedTarget(backup.DiskMount), Host: host, Logf: log.Printf}
	// Sauvegarde croisée : demandes signées par la clé de l'appliance (spécification 9.1).
	if peerKey, err := addresses.LoadOrCreateKey(filepath.Join(*data, "address-key")); err == nil {
		backups.PeerKey = peerKey
	} else {
		log.Printf("sauvegarde chez un proche : %v", err)
	}
	if *backupScript != "" {
		backups.Disks = &backup.Disks{Runner: network.Script(*backupScript)}
	}
	// Mises à jour proposées, jamais installées d'office (spécification 9.2).
	checker := &updates.Checker{Version: version, DownloadDir: filepath.Join(*data, "system", "upgrade"), SettingsFile: filepath.Join(*data, "updates.json"), Logf: log.Printf}
	if err := checker.Load(); err != nil {
		log.Printf("canal de version : %v ; canal stable", err)
	}
	if *updatesScript != "" {
		checker.Runner = network.Script(*updatesScript)
	}
	if *releaseRepo != "" {
		checker.Releases = updates.GitHub{Repo: *releaseRepo}
	}
	// Cockpit, installé à la demande, avec connexion unique (spécification 6.4).
	var admin *cockpit.Client
	var cockpitLogins *cockpit.Logins
	if *cockpitScript != "" {
		admin = &cockpit.Client{Runner: network.Script(*cockpitScript), Logf: log.Printf}
		// Connexion unique : un compte système par administrateur de la console.
		cockpitLogins = &cockpit.Logins{
			Client: admin,
			File:   filepath.Join(*data, "system", "cockpit-accounts.json"),
			Admins: func() []string {
				var names []string
				for _, account := range accounts.List() {
					if account.Role == auth.RoleAdmin && !account.Invited {
						names = append(names, account.Name)
					}
				}
				return names
			},
			Logf: log.Printf,
		}
		go cockpitLogins.Run(context.Background())
	}
	// Statistiques anonymes : désactivées tant que l'administrateur ne les accepte pas.
	reporter, err := stats.Open(filepath.Join(*data, "stats.json"))
	if err != nil {
		log.Fatal(err)
	}
	reporter.RunningFile = filepath.Join(*data, "running")
	reporter.Version = version
	reporter.URL = *statsURL
	reporter.Logf = log.Printf
	reporter.Installed = func() ([]string, func(string) bool, error) {
		instances, err := manager.List()
		apps := make([]string, 0, len(instances))
		for _, instance := range instances {
			apps = append(apps, instance.App)
		}
		known := func(app string) bool { _, found := market.Get(app); return found }
		return apps, known, err
	}
	if err := reporter.Start(); err != nil {
		log.Printf("statistiques : %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go market.Run(ctx)
	measured := make(chan struct{})
	go func() {
		history.Run(ctx, time.Minute, func() metrics.Sample { return metrics.Measure(resources()) }, log.Printf)
		close(measured)
	}()
	go duckdns.Run(ctx)
	if address != nil {
		go address.Run(ctx, 5*time.Minute)
	}
	go backups.Run(ctx)
	go manager.Relocate(ctx, log.Printf)
	go manager.RunNextcloud(ctx, 5*time.Minute, log.Printf)
	go checker.Run(ctx, 5*time.Minute)
	if stats.Available {
		go reporter.Run(ctx)
	}
	buildRevision, buildDate := buildCommit()
	config := console.Config{
		Version:          version,
		Commit:           buildRevision,
		Date:             buildDate,
		Catalog:          market,
		Services:         manager,
		Accounts:         accounts,
		Network:          net,
		Storage:          disks,
		Resources:        resources,
		History:          history,
		DuckDNS:          duckdns,
		Address:          address,
		Backups:          backups,
		Updates:          checker,
		Cockpit:          admin,
		CockpitLogins:    cockpitLogins,
		Stats:            statsConsole(reporter),
		WizardFile:       filepath.Join(*data, "wizard-done"),
		ACMEEmailFile:    filepath.Join(*data, "system", "traefik", "acme-email"),
		Mailer:           mailer,
		SMTP:             smtpSender,
		Diagnostic:       diag,
		DiagnosticWindow: window,
		DevicePublicKey:  filepath.Join(*data, "system", "device.pub"),
	}
	if *cliTokenFile != "" {
		token, err := cli.WriteToken(*cliTokenFile)
		if err != nil {
			log.Printf("ligne de commande : %v", err)
		} else {
			config.CLIToken = token
		}
	}
	if *traefikInternet != "" {
		config.InternetFile = filepath.Join(*data, "internet-console.json")
		config.TraefikInternetFile = *traefikInternet
	}
	config.ServicesInternetFile = *traefikServices
	if config.TraefikInternetFile != "" || config.ServicesInternetFile != "" {
		go console.RunInternet(ctx, config, time.Minute)
	}
	handler := console.Handler(config)
	server := &http.Server{
		Addr:              *addr,
		Handler:           reporter.Guard(handler),
		ReadHeaderTimeout: 10 * time.Second,
	}
	// Traefik tombé : la page de diagnostic reste joignable sur le port 8443 (spécification 8.5).
	if *rescueAddr != "" && diag != nil {
		rescue := &diagnostic.Rescue{
			Addr:     *rescueAddr,
			Probe:    "127.0.0.1:443",
			CertFile: filepath.Join(*tlsDir, "local.crt"),
			KeyFile:  filepath.Join(*tlsDir, "local.key"),
			Handler:  reporter.Guard(console.RescueHandler(handler)),
			Logf:     log.Printf,
		}
		go rescue.Run(ctx)
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
	if err := reporter.Stop(); err != nil {
		log.Printf("statistiques : %v", err)
	}
}

// statsConsole donne les statistiques anonymes à la console seulement quand la
// fonction est proposée (stats.Available) : sinon la console cache l'écran.
func statsConsole(reporter *stats.Reporter) *stats.Reporter {
	if !stats.Available {
		return nil
	}
	return reporter
}
