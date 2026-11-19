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

package console

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"

	"github.com/gmoulard/pi-appliance/internal/services"
	"github.com/gmoulard/pi-appliance/marketplace"
)

const (
	BasePath     = "/pi-appliance"
	maxBodyBytes = 64 << 10
)

//go:embed web
var webFiles embed.FS

type Health struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type InstallRequest struct {
	App        string            `json:"app"`
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters"`
}

type console struct {
	version  string
	catalog  marketplace.Catalog
	services *services.Manager
}

func Handler(version string, catalog marketplace.Catalog, manager *services.Manager) http.Handler {
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	c := &console{version: version, catalog: catalog, services: manager}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+BasePath+"/api/health", c.health)
	mux.HandleFunc("GET "+BasePath+"/api/catalog", c.listCatalog)
	mux.HandleFunc("GET "+BasePath+"/api/services", c.listServices)
	mux.HandleFunc("POST "+BasePath+"/api/services", adminOnly(c.install))
	mux.HandleFunc("DELETE "+BasePath+"/api/services/{name}", adminOnly(c.remove))
	mux.Handle("GET "+BasePath+"/", http.StripPrefix(BasePath, http.FileServerFS(web)))
	mux.Handle("GET "+BasePath, http.RedirectHandler(BasePath+"/", http.StatusMovedPermanently))
	return mux
}

// NE PAS ouvrir les actions d'administration au réseau tant que l'authentification des administrateurs (spécification 8.1) n'existe pas : elles ne répondent qu'aux requêtes venues du Pi lui-même.
func adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			writeError(w, http.StatusForbidden, "Cette action n'est possible que depuis le Pi lui-même pour l'instant.")
			return
		}
		next(w, r)
	}
}

func (c *console) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Health{Status: "ok", Version: c.version})
}

func (c *console) listCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"apps": c.catalog.Apps()})
}

func (c *console) listServices(w http.ResponseWriter, _ *http.Request) {
	instances, err := c.services.List()
	if err != nil {
		log.Printf("liste des services : %v", err)
		writeError(w, http.StatusInternalServerError, "La liste des services est illisible.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": instances})
}

func (c *console) install(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Requête attendue en JSON.")
		return
	}
	var request InstallRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "Requête illisible.")
		return
	}
	app, ok := c.catalog.Get(request.App)
	if !ok {
		writeError(w, http.StatusNotFound, "Cette application n'est pas dans la marketplace.")
		return
	}
	if request.Name == "" {
		request.Name = app.Subdomain
	}
	result, err := c.services.Install(r.Context(), app, request.Name, request.Parameters)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (c *console) remove(w http.ResponseWriter, r *http.Request) {
	var keepData bool
	switch r.URL.Query().Get("data") {
	case "keep":
		keepData = true
	case "delete":
	default:
		writeError(w, http.StatusBadRequest, "Dites s'il faut garder (data=keep) ou supprimer (data=delete) les données du service.")
		return
	}
	if err := c.services.Remove(r.Context(), r.PathValue("name"), keepData); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, services.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		log.Printf("service : %v", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("réponse JSON : %v", err)
	}
}
