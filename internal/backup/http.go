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

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gmoulard/pi-appliance/internal/storage"
)

// API d'administration des sauvegardes, sous <prefix>/api/backups. Sauvegarde
// et restauration durent : elles se poursuivent après la réponse (202) et se
// suivent dans GET (champs running et last).

// Summary : une sauvegarde vue par la console, sans l'enveloppe de la clé.
type Summary struct {
	ID       string        `json:"id"`
	Created  time.Time     `json:"created"`
	Kind     Kind          `json:"kind"`
	SizeMB   int64         `json:"size_mb"`
	Services []ServiceCopy `json:"services"`
}

// Status : état des sauvegardes pour la console.
type Status struct {
	Settings Settings `json:"settings"`
	// Ready : la clé est protégée par le mot de passe administrateur.
	Ready   bool     `json:"ready"`
	Running string   `json:"running"`
	Last    *Outcome `json:"last"`
	// Disk : disques USB et disque de sauvegarde ; nil si non réglable ici.
	Disk *DiskStatus `json:"disk"`
	// Destination : vrai si le disque de sauvegarde est branché.
	Destination bool      `json:"destination"`
	Snapshots   []Summary `json:"snapshots"`
}

// RestoreRequest : service à restaurer ; Password seulement pour une
// sauvegarde faite avec une autre clé (autre carte SD).
type RestoreRequest struct {
	Service  string `json:"service"`
	Password string `json:"password"`
}

// DiskUseRequest : utiliser cette partition ext4 pour les sauvegardes.
type DiskUseRequest struct {
	Partition string `json:"partition"`
}

// DiskFormatRequest : effacer ce disque ; Erase confirme que son contenu sera perdu.
type DiskFormatRequest struct {
	Disk  string `json:"disk"`
	Erase bool   `json:"erase"`
}

// Routes ajoute l'API à mux ; admin garde chaque route (session d'administrateur).
func (m *Manager) Routes(mux *http.ServeMux, prefix string, admin func(http.HandlerFunc) http.HandlerFunc) {
	base := prefix + "/api/backups"
	mux.HandleFunc("GET "+base, admin(m.status))
	mux.HandleFunc("POST "+base, admin(m.start))
	mux.HandleFunc("PUT "+base+"/settings", admin(m.saveSettings))
	mux.HandleFunc("POST "+base+"/{id}/restore", admin(m.restore))
	mux.HandleFunc("POST "+base+"/disk/use", admin(m.diskUse))
	mux.HandleFunc("POST "+base+"/disk/format", admin(m.diskFormat))
}

func (m *Manager) status(w http.ResponseWriter, r *http.Request) {
	status := Status{Settings: m.Settings(), Ready: m.keys().hasEnvelope(), Running: m.Running(), Last: m.Last(), Snapshots: []Summary{}}
	if m.Disks != nil {
		if disk, err := m.Disks.Status(r.Context()); err == nil {
			status.Disk = &disk
		} else {
			log.Printf("disque de sauvegarde : %v", err)
		}
	}
	if snapshots, err := m.Snapshots(); err == nil {
		status.Destination = true
		for _, s := range snapshots {
			status.Snapshots = append(status.Snapshots, Summary{ID: s.ID, Created: s.Created, Kind: s.Kind, SizeMB: s.SizeMB, Services: s.Services})
		}
	} else if !errors.Is(err, ErrNoDisk) {
		log.Printf("sauvegardes illisibles : %v", err)
	}
	writeJSON(w, http.StatusOK, status)
}

func (m *Manager) saveSettings(w http.ResponseWriter, r *http.Request) {
	var settings Settings
	if !decode(w, r, &settings) {
		return
	}
	if err := m.SetSettings(settings); err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m.Settings())
}

// background lance une opération longue : refus immédiat si elle ne peut pas
// commencer, sinon 202 et la suite dans GET.
func (m *Manager) background(w http.ResponseWriter, check func() error, run func(context.Context)) {
	if m.Running() != "" {
		writeFailure(w, ErrBusy)
		return
	}
	if err := check(); err != nil {
		writeFailure(w, err)
		return
	}
	go run(context.Background())
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}

func (m *Manager) start(w http.ResponseWriter, r *http.Request) {
	m.background(w, m.ready, func(ctx context.Context) { m.Backup(ctx, Manual) })
}

// ready vérifie ce qui se voit avant de lancer : disque branché, clé protégée.
func (m *Manager) ready() error {
	if _, err := m.Target(); err != nil {
		return err
	}
	if !m.keys().hasEnvelope() {
		return ErrNoKey
	}
	return nil
}

func (m *Manager) restore(w http.ResponseWriter, r *http.Request) {
	var request RestoreRequest
	if !decode(w, r, &request) {
		return
	}
	id := r.PathValue("id")
	check := func() error {
		if !idPattern.MatchString(id) || !validName(request.Service) {
			return ErrInvalid
		}
		root, err := m.Target()
		if err != nil {
			return err
		}
		snapshot, err := readManifest(filepath.Join(root, id))
		if err != nil {
			return ErrNotFound
		}
		if _, ok := snapshot.service(request.Service); !ok {
			return ErrNotFound
		}
		// Le mot de passe se vérifie avant de répondre : la console le redemande.
		_, err = m.snapshotKey(snapshot, request.Password)
		return err
	}
	m.background(w, check, func(ctx context.Context) { m.Restore(ctx, id, request.Service, request.Password) })
}

func (m *Manager) diskUse(w http.ResponseWriter, r *http.Request) {
	var request DiskUseRequest
	if !decode(w, r, &request) {
		return
	}
	if m.Disks == nil {
		writeError(w, http.StatusServiceUnavailable, "Le disque de sauvegarde ne se règle pas depuis cette console.")
		return
	}
	if err := m.Disks.Use(r.Context(), request.Partition); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) diskFormat(w http.ResponseWriter, r *http.Request) {
	var request DiskFormatRequest
	if !decode(w, r, &request) {
		return
	}
	if !request.Erase {
		writeError(w, http.StatusBadRequest, "Confirmez que le contenu du disque sera effacé.")
		return
	}
	if m.Disks == nil {
		writeError(w, http.StatusServiceUnavailable, "Le disque de sauvegarde ne se règle pas depuis cette console.")
		return
	}
	if m.Running() != "" {
		writeFailure(w, ErrBusy)
		return
	}
	if err := m.Disks.Format(r.Context(), request.Disk); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeFailure(w http.ResponseWriter, err error) {
	var failure storage.Failure
	switch {
	case errors.As(err, &failure):
		writeJSON(w, http.StatusConflict, map[string]string{"error": failure.Message, "code": failure.Code})
	case errors.Is(err, ErrPassword):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Cette sauvegarde a été faite sur une autre carte SD : saisissez le mot de passe administrateur en vigueur à sa date.", "code": "password"})
	case errors.Is(err, ErrBusy):
		writeError(w, http.StatusConflict, "Une sauvegarde ou une restauration est déjà en cours.")
	case errors.Is(err, ErrNoDisk):
		writeError(w, http.StatusConflict, "Branchez le disque de sauvegarde.")
	case errors.Is(err, ErrNoKey):
		writeError(w, http.StatusConflict, "Les sauvegardes attendent votre mot de passe : déconnectez-vous puis reconnectez-vous.")
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "Cette sauvegarde est introuvable.")
	case errors.Is(err, ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("sauvegardes : %v", err)
		writeError(w, http.StatusInternalServerError, "Le disque de sauvegarde ne répond pas.")
	}
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, "Demande illisible.")
		return false
	}
	return true
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
