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
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/gmoulard/pi-appliance/internal/console"
	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

var version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "adresse d'écoute de la console")
	data := flag.String("data", "/var/lib/pi-appliance", "dossier des services installés")
	domain := flag.String("domain", "pi-appliance.local", "adresse de l'appliance, suffixe de l'adresse de chaque service")
	flag.Parse()
	catalog, err := marketplace.Builtin()
	if err != nil {
		log.Fatal(err)
	}
	manager := &services.Manager{Dir: *data, Domain: *domain, Runner: services.Docker{}, MemoryMB: services.SystemMemoryMB}
	server := &http.Server{
		Addr:              *addr,
		Handler:           console.Handler(version, catalog, manager),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("console pi-appliance %s sur %s%s", version, *addr, console.BasePath)
	log.Fatal(server.ListenAndServe())
}
