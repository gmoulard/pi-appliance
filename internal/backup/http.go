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

package backup

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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
	// Places : destinations qui la gardent (disk, online, peer).
	Places []string `json:"places"`
	// Locked : faite sur une autre carte SD, services illisibles sans le
	// mot de passe en vigueur à sa date.
	Locked bool `json:"locked"`
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
	// Destination : vrai si une destination est utilisable (disque branché
	// ou stockage en ligne réglé).
	Destination bool `json:"destination"`
	// DiskReady : vrai si le disque de sauvegarde est branché.
	DiskReady bool         `json:"disk_ready"`
	Online    OnlinePublic `json:"online"`
	// Peer : sauvegarde chez un proche, et les proches accueillis ici.
	Peer      *PeerStatus `json:"peer,omitempty"`
	Snapshots []Summary   `json:"snapshots"`
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
	mux.HandleFunc("POST "+base+"/{id}/unlock", admin(m.unlock))
	mux.HandleFunc("GET "+base+"/{id}/files", admin(m.files))
	mux.HandleFunc("GET "+base+"/{id}/download", admin(m.download))
	mux.HandleFunc("POST "+base+"/{id}/put-back", admin(m.putBack))
	mux.HandleFunc("PUT "+base+"/online", admin(m.saveOnline))
	mux.HandleFunc("DELETE "+base+"/online", admin(m.removeOnline))
	m.peerRoutes(mux, prefix, admin)
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
	_, diskErr := m.Target()
	status.DiskReady = diskErr == nil
	if online, ok := m.OnlineSettings(); ok {
		status.Online = OnlinePublic{Configured: true, Endpoint: online.Endpoint, Region: online.Region, Bucket: online.Bucket}
	}
	status.Peer = m.peerStatus(r.Context())
	if snapshots, places, err := m.snapshotsByPlace(r.Context()); err == nil {
		status.Destination = true
		for _, s := range snapshots {
			status.Snapshots = append(status.Snapshots, Summary{ID: s.ID, Created: s.Created, Kind: s.Kind, SizeMB: s.SizeMB, Services: s.Services, Places: places[s.ID], Locked: s.Locked})
		}
	} else if !errors.Is(err, ErrNoDisk) {
		status.Destination = true
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

// ready vérifie ce qui se voit avant de lancer : destination, clé protégée.
func (m *Manager) ready() error {
	if _, err := m.stores(); err != nil {
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
		_, snapshot, err := m.find(context.Background(), id)
		if err != nil {
			return err
		}
		// Le mot de passe se vérifie avant de répondre : la console le redemande.
		key, err := m.snapshotKey(snapshot, request.Password)
		if err != nil {
			return err
		}
		if snapshot.Locked {
			if snapshot, err = unseal(snapshot, key); err != nil {
				return err
			}
		}
		if _, ok := snapshot.service(request.Service); !ok {
			return ErrNotFound
		}
		return nil
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

// UnlockRequest : mot de passe en vigueur à la date d'une sauvegarde faite sur une autre carte SD.
type UnlockRequest struct {
	Password string `json:"password"`
}

func (m *Manager) unlock(w http.ResponseWriter, r *http.Request) {
	var request UnlockRequest
	if !decode(w, r, &request) {
		return
	}
	if err := m.Unlock(r.Context(), r.PathValue("id"), request.Password); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// FilesReply : contenu d'un dossier ; Preparing tant que la liste se prépare
// (Count fichiers déjà lus).
type FilesReply struct {
	Preparing bool `json:"preparing"`
	Count     int  `json:"count"`
	Listing
}

func (m *Manager) files(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	listing, count, err := m.Files(r.Context(), r.PathValue("id"), query.Get("service"), query.Get("path"))
	if errors.Is(err, ErrPreparing) {
		writeJSON(w, http.StatusAccepted, FilesReply{Preparing: true, Count: count, Listing: Listing{Entries: []Entry{}}})
		return
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, FilesReply{Count: count, Listing: listing})
}

func (m *Manager) download(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if err := m.Download(r.Context(), w, r.PathValue("id"), query.Get("service"), query.Get("path")); err != nil {
		writeFailure(w, err)
	}
}

// PutBackRequest : remettre un fichier ou un dossier à sa place ; Replace
// confirme le remplacement de celui qui y est encore.
type PutBackRequest struct {
	Service string `json:"service"`
	Path    string `json:"path"`
	Replace bool   `json:"replace"`
}

func (m *Manager) putBack(w http.ResponseWriter, r *http.Request) {
	var request PutBackRequest
	if !decode(w, r, &request) {
		return
	}
	id := r.PathValue("id")
	check := func() error { return m.CheckPutBack(r.Context(), id, request.Service, request.Path, request.Replace) }
	m.background(w, check, func(ctx context.Context) { m.PutBack(ctx, id, request.Service, request.Path) })
}

func (m *Manager) saveOnline(w http.ResponseWriter, r *http.Request) {
	var settings OnlineSettings
	if !decode(w, r, &settings) {
		return
	}
	if m.Running() != "" {
		writeFailure(w, ErrBusy)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := m.SetOnline(ctx, settings); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) removeOnline(w http.ResponseWriter, r *http.Request) {
	if err := m.RemoveOnline(); err != nil {
		writeFailure(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeFailure(w http.ResponseWriter, err error) {
	var failure storage.Failure
	var online *ErrOnline
	var peer *ErrPeer
	switch {
	case errors.As(err, &peer):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": capitalize(peer.Error()) + ".", "code": "peer"})
	case errors.As(err, &online):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Stockage en ligne : " + online.Error() + ".", "code": "online"})
	case errors.Is(err, ErrExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Ce fichier existe encore : confirmez pour le remplacer par celui de la sauvegarde.", "code": "exists"})
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
	case errors.Is(err, ErrNotFound) && err != ErrNotFound:
		writeError(w, http.StatusNotFound, capitalize(strings.TrimPrefix(err.Error(), ErrNotFound.Error()+" : "))+".")
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

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}
