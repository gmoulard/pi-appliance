package console

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
)

const BasePath = "/pi-appliance"

//go:embed web
var webFiles embed.FS

type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func Handler(version string) http.Handler {
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+BasePath+"/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(Health{Status: "ok", Version: version}); err != nil {
			log.Printf("health: %v", err)
		}
	})
	mux.Handle("GET "+BasePath+"/", http.StripPrefix(BasePath, http.FileServerFS(web)))
	mux.Handle("GET "+BasePath, http.RedirectHandler(BasePath+"/", http.StatusMovedPermanently))
	return mux
}
