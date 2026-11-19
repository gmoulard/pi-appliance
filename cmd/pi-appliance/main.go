package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/gmoulard/pi-appliance/internal/console"
)

var version = "dev"

func main() {
	addr := flag.String("addr", ":8080", "adresse d'écoute de la console")
	flag.Parse()
	server := &http.Server{
		Addr:              *addr,
		Handler:           console.Handler(version),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("console pi-appliance %s sur %s%s", version, *addr, console.BasePath)
	log.Fatal(server.ListenAndServe())
}
